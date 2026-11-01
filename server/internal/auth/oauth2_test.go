package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

// ==================== JWKS 反序列化 ====================

func TestJWKSKey_UnmarshalPreservesRawFields(t *testing.T) {
	// 关键回归：RawJSON 字段带 json:"-"，若没有自定义 UnmarshalJSON，
	// 标准库反序列化会忽略它，导致验签永远拿不到 n/e 等公钥参数。
	raw := `{"kty":"RSA","kid":"key-1","use":"sig","alg":"RS256","n":"AQAB","e":"AQAB"}`
	var k jwksKey
	if err := json.Unmarshal([]byte(raw), &k); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if k.Kty != "RSA" || k.Kid != "key-1" || k.Use != "sig" || k.Alg != "RS256" {
		t.Fatalf("常规字段未填充: %+v", k)
	}
	for _, f := range []string{"n", "e"} {
		if _, ok := k.RawJSON[f]; !ok {
			t.Fatalf("RawJSON 缺少字段 %q，验签将失败", f)
		}
		if v, ok := jwkString(&k, f); !ok || v == "" {
			t.Fatalf("jwkString(%q) 读取失败, ok=%v", f, ok)
		}
	}
}

// ==================== RS256 验签 ====================

// makeRSAJWKS 生成 RSA 密钥对并将其 n/e 序列化为 JWK JSON。
func makeRSAJWK(t *testing.T, kid string) (*rsa.PrivateKey, string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成 RSA 密钥失败: %v", err)
	}
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.PublicKey.E)).Bytes())
	n := base64.RawURLEncoding.EncodeToString(priv.PublicKey.N.Bytes())
	jwk := `{"kty":"RSA","kid":"` + kid + `","use":"sig","alg":"RS256","n":"` + n + `","e":"` + e + `"}`
	return priv, jwk
}

// signRSAJWT 用 RSA 私钥对 header.payload 签名，返回完整 JWT 与签名段。
func signRSAJWT(t *testing.T, priv *rsa.PrivateKey, alg string, kid string, claims map[string]interface{}) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": alg, "kid": kid})
	payload, _ := json.Marshal(claims)
	h := base64.RawURLEncoding.EncodeToString(header)
	p := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := h + "." + p

	var digest []byte
	switch alg {
	case "RS256":
		d := sha256.Sum256([]byte(signingInput))
		digest = d[:]
	case "RS384":
		d := sha512.Sum384([]byte(signingInput))
		digest = d[:]
	case "RS512":
		d := sha512.Sum512([]byte(signingInput))
		digest = d[:]
	default:
		t.Fatalf("不支持 alg %s", alg)
	}
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest)
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}
	return h + "." + p + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// verifyIDTokenWithJWK 用给定 JWK JSON 构造服务，对 idToken 执行完整校验。
func verifyIDTokenWithJWK(t *testing.T, idToken, jwk, kid string, cfg *OAuth2Config) error {
	t.Helper()
	var key jwksKey
	if err := json.Unmarshal([]byte(jwk), &key); err != nil {
		t.Fatalf("JWK 解析失败: %v", err)
	}
	s := &OAuth2Service{
		verify: jwtVerifyState{jwks: &jwksSet{Keys: []*jwksKey{&key}, FetchedAt: time.Now()}},
	}
	_, err := s.verifyIDToken(cfg, idToken)
	return err
}

func validTestOAuth2Config() *OAuth2Config {
	return &OAuth2Config{
		ClientID: "test-client",
		Issuer:   "https://issuer.example.com",
	}
}

func TestVerifyIDToken_RS256Valid(t *testing.T) {
	cfg := validTestOAuth2Config()
	priv, jwk := makeRSAJWK(t, "key-1")
	claims := map[string]interface{}{
		"iss": "https://issuer.example.com",
		"aud": "test-client",
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Add(-time.Minute).Unix(),
	}
	token := signRSAJWT(t, priv, "RS256", "key-1", claims)
	if err := verifyIDTokenWithJWK(t, token, jwk, "key-1", cfg); err != nil {
		t.Fatalf("合法 RS256 token 应验签通过: %v", err)
	}
}

func TestVerifyIDToken_WrongKeyRejected(t *testing.T) {
	// 用错误的公钥验签应失败
	cfg := validTestOAuth2Config()
	priv, _ := makeRSAJWK(t, "key-1")
	_, jwkOther := makeRSAJWK(t, "key-1") // 另一把公钥，同 kid
	claims := map[string]interface{}{
		"iss": "https://issuer.example.com",
		"aud": "test-client",
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	token := signRSAJWT(t, priv, "RS256", "key-1", claims)
	if err := verifyIDTokenWithJWK(t, token, jwkOther, "key-1", cfg); err == nil {
		t.Fatalf("错误公钥验签应失败")
	}
}

func TestVerifyIDToken_UnsafeAlgRejected(t *testing.T) {
	// none / HS256 / PS256 一律拒绝，防 alg 混淆
	cfg := validTestOAuth2Config()
	claims := map[string]interface{}{
		"iss": "https://issuer.example.com",
		"aud": "test-client",
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	payload, _ := json.Marshal(claims)
	hb := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	pb := base64.RawURLEncoding.EncodeToString(payload)
	noneToken := hb + "." + pb + "."
	if err := verifyIDTokenWithJWK(t, noneToken, `{"kty":"RSA","kid":"k","n":"AQAB","e":"AQAB"}`, "k", cfg); err == nil {
		t.Fatalf("alg=none 应被拒绝")
	}

	hsHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","kid":"k"}`))
	hsToken := hsHeader + "." + pb + ".xxx"
	if err := verifyIDTokenWithJWK(t, hsToken, `{"kty":"RSA","kid":"k","n":"AQAB","e":"AQAB"}`, "k", cfg); err == nil {
		t.Fatalf("alg=HS256 应被拒绝")
	}

	priv, jwk := makeRSAJWK(t, "k")
	psClaims := claims
	psToken := signRSAJWT(t, priv, "RS256", "k", psClaims) // header 仍 RS256，仅验证 alg 白名单对非 RS/ES 拒收
	if err := verifyIDTokenWithJWK(t, psToken, jwk, "k", cfg); err != nil {
		// 允许通过（RS256 合法）；这里不测 PS 签名路径
		_ = err
	}
	// 直接验证 verifySignature 对 PS256 的拒绝
	key := &jwksKey{Kty: "RSA", Kid: "k"}
	if err := verifySignature(key, "PS256", "a.b", []byte("sig")); err == nil {
		t.Fatalf("verifySignature 应拒绝 PS256")
	}
}

func TestVerifyIDToken_ClaimChecks(t *testing.T) {
	cfg := validTestOAuth2Config()
	priv, jwk := makeRSAJWK(t, "key-1")

	base := map[string]interface{}{
		"iss": "https://issuer.example.com",
		"aud": "test-client",
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Add(-time.Minute).Unix(),
	}

	cases := []struct {
		name   string
		mutate func(m map[string]interface{})
	}{
		{"iss 不匹配", func(m map[string]interface{}) { m["iss"] = "https://evil.example.com" }},
		{"aud 不包含 client_id", func(m map[string]interface{}) { m["aud"] = "other-client" }},
		{"aud 是数组且不含 client_id", func(m map[string]interface{}) { m["aud"] = []interface{}{"a", "b"} }},
		{"过期", func(m map[string]interface{}) { m["exp"] = time.Now().Add(-time.Hour).Unix() }},
		{"缺 exp", func(m map[string]interface{}) { delete(m, "exp") }},
		{"nbf 在未来", func(m map[string]interface{}) { m["nbf"] = time.Now().Add(time.Hour).Unix() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			claims := make(map[string]interface{}, len(base))
			for k, v := range base {
				claims[k] = v
			}
			c.mutate(claims)
			token := signRSAJWT(t, priv, "RS256", "key-1", claims)
			if err := verifyIDTokenWithJWK(t, token, jwk, "key-1", cfg); err == nil {
				t.Fatalf("case %q 应验签失败", c.name)
			}
		})
	}
}

func TestVerifyIDToken_AudienceArrayContainsClientID(t *testing.T) {
	cfg := validTestOAuth2Config()
	priv, jwk := makeRSAJWK(t, "key-1")
	claims := map[string]interface{}{
		"iss": "https://issuer.example.com",
		"aud": []interface{}{"other", "test-client"},
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	token := signRSAJWT(t, priv, "RS256", "key-1", claims)
	if err := verifyIDTokenWithJWK(t, token, jwk, "key-1", cfg); err != nil {
		t.Fatalf("aud 数组包含 client_id 应通过: %v", err)
	}
}

// ==================== ECDSA 验签 ====================

func makeECJWK(t *testing.T, kid string) (*ecdsa.PrivateKey, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成 EC 密钥失败: %v", err)
	}
	byteLen := (priv.PublicKey.Curve.Params().BitSize + 7) / 8
	x := priv.PublicKey.X.Bytes()
	y := priv.PublicKey.Y.Bytes()
	xb := make([]byte, byteLen)
	yb := make([]byte, byteLen)
	copy(xb[byteLen-len(x):], x)
	copy(yb[byteLen-len(y):], y)
	jwk := `{"kty":"EC","kid":"` + kid + `","use":"sig","alg":"ES256","crv":"P-256","x":"` +
		base64.RawURLEncoding.EncodeToString(xb) + `","y":"` + base64.RawURLEncoding.EncodeToString(yb) + `"}`
	return priv, jwk
}

func signECJWT(t *testing.T, priv *ecdsa.PrivateKey, alg string, kid string, claims map[string]interface{}) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": alg, "kid": kid})
	payload, _ := json.Marshal(claims)
	h := base64.RawURLEncoding.EncodeToString(header)
	p := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := h + "." + p
	d := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, priv, d[:])
	if err != nil {
		t.Fatalf("EC 签名失败: %v", err)
	}
	byteLen := (priv.PublicKey.Curve.Params().BitSize + 7) / 8
	sig := make([]byte, 2*byteLen)
	r.FillBytes(sig[:byteLen])
	s.FillBytes(sig[byteLen:])
	return h + "." + p + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestVerifyIDToken_ES256Valid(t *testing.T) {
	cfg := validTestOAuth2Config()
	priv, jwk := makeECJWK(t, "ec-1")
	claims := map[string]interface{}{
		"iss": "https://issuer.example.com",
		"aud": "test-client",
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	token := signECJWT(t, priv, "ES256", "ec-1", claims)
	if err := verifyIDTokenWithJWK(t, token, jwk, "ec-1", cfg); err != nil {
		t.Fatalf("合法 ES256 token 应验签通过: %v", err)
	}
}

func TestVerifyIDToken_MalformedRejected(t *testing.T) {
	cfg := validTestOAuth2Config()
	// 非三段式
	if _, err := simpleJWTVerify(cfg, "only-two-parts"); err == nil {
		t.Fatalf("非三段式 JWT 应被拒绝")
	}
}

// simpleJWTVerify 构造不依赖缓存的直接调用（空 JWKS 触发 fail-closed）。
func simpleJWTVerify(cfg *OAuth2Config, token string) (map[string]interface{}, error) {
	s := &OAuth2Service{}
	return s.verifyIDToken(cfg, token)
}

// ==================== 多候选 / 密钥选择 ====================

func TestLookupVerifyKey_MultipleCandidatesRejected(t *testing.T) {
	// 两个同 kid 候选且 use/alg 均不明确匹配时，不应静默选错
	k1 := &jwksKey{Kid: "same", Kty: "RSA", Use: "enc", Alg: "RS256"}
	k2 := &jwksKey{Kid: "same", Kty: "RSA", Use: "enc", Alg: "RS256"}
	s := &OAuth2Service{verify: jwtVerifyState{jwks: &jwksSet{Keys: []*jwksKey{k1, k2}, FetchedAt: time.Now()}}}
	if _, err := s.lookupVerifyKey(validTestOAuth2Config(), "same", "RS256"); err == nil {
		t.Fatalf("多个候选且 use/alg 均不符时应拒绝，当前静默选错有密钥选择风险")
	}
}

func TestSignState_VerifyStateRoundtrip(t *testing.T) {
	s := &OAuth2Service{}
	state, err := s.signState()
	if err != nil {
		t.Fatalf("signState 失败: %v", err)
	}
	if err := s.verifyState(state); err != nil {
		t.Fatalf("自身生成的 state 应校验通过: %v", err)
	}
	// 篡改一位应失败
	if strings.HasSuffix(state, "a") {
		state = state[:len(state)-1] + "b"
	} else {
		state = state + "x"
	}
	if err := s.verifyState(state); err == nil {
		t.Fatalf("被篡改的 state 应校验失败")
	}
	// 格式非法拒绝
	if err := s.verifyState("not-valid-state"); err == nil {
		t.Fatalf("格式非法 state 应校验失败")
	}
}
