package downloadticket

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// testResourceKey 构造一个稳定的文件身份，供各用例复用
func testResourceKey() ResourceKey {
	return ResourceKey{Kind: KindPublic, Root: "public", Rel: "docs/report.pdf", Size: 1024, Mtime: 1700000000}
}

// TestSignValidateRoundTrip 签发票据后应能验签并还原全部字段
func TestSignValidateRoundTrip(t *testing.T) {
	key := testResourceKey()
	ticket, err := Sign(key, ModeDownload, Signer{Username: "alice", Role: "user", UUID: "uuid-1"})
	if err != nil {
		t.Fatalf("签发票据失败: %v", err)
	}

	p, err := Validate(ticket)
	if err != nil {
		t.Fatalf("验签失败: %v", err)
	}
	if p.Kind != KindPublic || p.Root != "public" || p.Rel != "docs/report.pdf" {
		t.Errorf("资源身份不匹配: %+v", p)
	}
	if p.Size != key.Size || p.Mtime != key.Mtime {
		t.Errorf("文件大小/修改时间不匹配: %d/%d", p.Size, p.Mtime)
	}
	if p.M != ModeDownload || p.U != "alice" || p.R != "user" || p.D != "uuid-1" {
		t.Errorf("用户或用途字段不匹配: %+v", p)
	}
	if p.E <= time.Now().Unix() {
		t.Errorf("过期时间应大于当前时间: %d", p.E)
	}
}

// TestValidateReusableWithinTTL 票据按时限生效，TTL 内可重复校验（不做一次性消费）
func TestValidateReusableWithinTTL(t *testing.T) {
	key := testResourceKey()
	ticket, err := Sign(key, ModePreview, Signer{Username: "alice", Role: "user", UUID: "uuid-1"})
	if err != nil {
		t.Fatalf("签发票据失败: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := Validate(ticket); err != nil {
			t.Fatalf("第 %d 次验签失败: %v", i+1, err)
		}
	}
}

// TestValidateRejectsTampered 篡改载荷后签名不匹配，必须拒绝
func TestValidateRejectsTampered(t *testing.T) {
	key := testResourceKey()
	ticket, err := Sign(key, ModeDownload, Signer{Username: "alice", Role: "user", UUID: "uuid-1"})
	if err != nil {
		t.Fatalf("签发票据失败: %v", err)
	}

	parts := strings.SplitN(ticket, ".", 2)
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("解码载荷失败: %v", err)
	}
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("反序列化载荷失败: %v", err)
	}
	p.Rel = "docs/other.pdf"
	raw, _ = json.Marshal(p)
	tampered := base64.RawURLEncoding.EncodeToString(raw) + "." + parts[1]

	if _, err := Validate(tampered); !errors.Is(err, ErrInvalid) {
		t.Fatalf("期望 ErrInvalid，实际: %v", err)
	}
}

// TestValidateRejectsMalformed 空串、缺分隔符、签名错误均拒绝
func TestValidateRejectsMalformed(t *testing.T) {
	for _, ticket := range []string{"", "abc", "abc.def", "a.b.c"} {
		if _, err := Validate(ticket); !errors.Is(err, ErrInvalid) {
			t.Errorf("票据 %q 期望 ErrInvalid，实际: %v", ticket, err)
		}
	}
}

// TestValidateRejectsLegacyPayload 缺少文件绑定字段的旧票据一律拒绝
func TestValidateRejectsLegacyPayload(t *testing.T) {
	payload := Payload{V: 1, M: ModeDownload, E: time.Now().Add(time.Minute).Unix(), U: "alice", R: "user", D: "uuid-1"}
	raw, _ := json.Marshal(payload)
	b64 := base64.RawURLEncoding.EncodeToString(raw)
	ticket := b64 + "." + hmacSHA256(b64)

	if _, err := Validate(ticket); !errors.Is(err, ErrInvalid) {
		t.Fatalf("期望 ErrInvalid，实际: %v", err)
	}
}

// TestValidateExpired 超过有效期的票据返回 ErrExpired
func TestValidateExpired(t *testing.T) {
	key := testResourceKey()
	payload := Payload{
		V: 1, Kind: key.Kind, Root: key.Root, Rel: key.Rel, Size: key.Size, Mtime: key.Mtime,
		M: ModeDownload, E: time.Now().Add(-time.Minute).Unix(), U: "alice", R: "user", D: "uuid-1",
	}
	raw, _ := json.Marshal(payload)
	b64 := base64.RawURLEncoding.EncodeToString(raw)
	ticket := b64 + "." + hmacSHA256(b64)

	if _, err := Validate(ticket); !errors.Is(err, ErrExpired) {
		t.Fatalf("期望 ErrExpired，实际: %v", err)
	}
}

// TestMatchFileBinding 票据只能用于签发时的同一文件与同一用途
func TestMatchFileBinding(t *testing.T) {
	key := testResourceKey()
	ticket, err := Sign(key, ModeDownload, Signer{Username: "alice", Role: "user", UUID: "uuid-1"})
	if err != nil {
		t.Fatalf("签发票据失败: %v", err)
	}
	p, err := Validate(ticket)
	if err != nil {
		t.Fatalf("验签失败: %v", err)
	}
	if !p.Match(key, false) {
		t.Fatal("同一文件与同一用途应匹配")
	}

	cases := []struct {
		name    string
		key     ResourceKey
		preview bool
		want    bool
	}{
		{"用途不一致（按预览取用）", key, true, false},
		{"相对路径不同", ResourceKey{Kind: KindPublic, Root: "public", Rel: "docs/other.pdf", Size: key.Size, Mtime: key.Mtime}, false, false},
		{"大小不同", ResourceKey{Kind: KindPublic, Root: "public", Rel: key.Rel, Size: key.Size + 1, Mtime: key.Mtime}, false, false},
		{"修改时间不同", ResourceKey{Kind: KindPublic, Root: "public", Rel: key.Rel, Size: key.Size, Mtime: key.Mtime + 1}, false, false},
		{"端点种类不同", ResourceKey{Kind: KindAdmin, Root: "public", Rel: key.Rel, Size: key.Size, Mtime: key.Mtime}, false, false},
		{"根目录不同", ResourceKey{Kind: KindPublic, Root: "private", Rel: key.Rel, Size: key.Size, Mtime: key.Mtime}, false, false},
		{"相对路径写法等价（分隔符/首尾斜杠）", ResourceKey{Kind: KindPublic, Root: "public", Rel: "/docs\\report.pdf", Size: key.Size, Mtime: key.Mtime}, false, true},
	}
	for _, tc := range cases {
		if got := p.Match(tc.key, tc.preview); got != tc.want {
			t.Errorf("%s: 期望 %v，实际 %v", tc.name, tc.want, got)
		}
	}
}

// TestMatchNilPayload 空载荷视为不匹配
func TestMatchNilPayload(t *testing.T) {
	var p *Payload
	if p.Match(testResourceKey(), false) {
		t.Fatal("nil 载荷不应匹配")
	}
}

// TestSplitRequestPath 端点种类识别（管理下载前缀优先）与未知路径
func TestSplitRequestPath(t *testing.T) {
	cases := []struct {
		path string
		kind string
		rest string
		ok   bool
	}{
		{"/api/v1/web/admin/download/public/a.txt", KindAdmin, "public/a.txt", true},
		{"/api/v1/web/download/public/a.txt", KindPublic, "public/a.txt", true},
		{"/download/public/a.txt", KindPublic, "public/a.txt", true},
		{"/api/v1/web/other", "", "", false},
		{"/download/", KindPublic, "", true},
	}
	for _, tc := range cases {
		kind, rest, ok := SplitRequestPath(tc.path)
		if ok != tc.ok || kind != tc.kind || rest != tc.rest {
			t.Errorf("路径 %q: 期望 (%q,%q,%v)，实际 (%q,%q,%v)",
				tc.path, tc.kind, tc.rest, tc.ok, kind, rest, ok)
		}
	}
}

// TestNormalizeRel 相对路径归一化规则
func TestNormalizeRel(t *testing.T) {
	cases := map[string]string{
		"/docs/report.pdf": "docs/report.pdf",
		"docs\\report.pdf": "docs/report.pdf",
		"//docs//a//b.txt": "docs/a/b.txt",
		"./docs/../a.txt":  "a.txt",
		"":                 "",
		".":                "",
	}
	for in, want := range cases {
		if got := NormalizeRel(in); got != want {
			t.Errorf("NormalizeRel(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestPayloadContext 票据载荷可经 request context 传递与读取
func TestPayloadContext(t *testing.T) {
	ctx := context.Background()
	if FromContext(ctx) != nil {
		t.Fatal("空 context 不应有载荷")
	}
	p := &Payload{Kind: KindPublic, Root: "public", Rel: "a.txt", Size: 1, Mtime: 1}
	ctx2 := WithPayload(ctx, p)
	if got := FromContext(ctx2); got != p {
		t.Fatalf("载荷不匹配: %+v", got)
	}
}
