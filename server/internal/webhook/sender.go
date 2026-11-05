package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"
)

// SignatureHeader HTTP 报头：HMAC-SHA256 签名，格式为 X-Fuzhan-Signature: sha256=<hex>。
// 接收方用配置的 secret 对请求体做同样计算即可校验推送来源与内容完整性。
const SignatureHeader = "X-Fuzhan-Signature"

// sender 负责向单个回调地址发送一次事件（含失败重试）。
type sender struct {
	client *http.Client
	secret string
	// maxRetries 失败重试次数（不含首次发送）；0 表示不重试。
	maxRetries int
	// endpoint 端点级配置（headers/query/独立 secret），便于发送方身份与接收方鉴权。
	endpoint *appconfig.WebhookEndpoint
}

// redactSensitiveKey 判断键名是否属于需要脱敏的敏感参数（URL query / headers）。
func redactSensitiveKey(k string) bool {
	lk := strings.ToLower(k)
	switch lk {
	case "token", "key", "secret", "access_token", "apikey", "api_key", "signature",
		"x-api-key", "authorization", "x-fuzhan-signature", "password", "accesskey":
		return true
	}
	return strings.Contains(lk, "token") || strings.Contains(lk, "secret") ||
		strings.Contains(lk, "key") || strings.Contains(lk, "signature") ||
		strings.Contains(lk, "password")
}

// redactURL 对 URL 中敏感 query 参数值脱敏为 ***，供日志/错误输出使用。
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	changed := false
	for k := range q {
		if redactSensitiveKey(k) {
			q.Set(k, "***")
			changed = true
		}
	}
	if changed {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// redactHeaders 对敏感请求头值脱敏为 ***，供日志/错误输出使用。
func redactHeaders(h map[string]string) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		if redactSensitiveKey(k) {
			out[k] = "***"
		} else {
			out[k] = v
		}
	}
	return out
}

// newPayload 将事件编码为 UTF-8 JSON 字节，供 body 与签名计算共用同一份内容。
func newPayload(ev Event) ([]byte, error) {
	return json.Marshal(ev)
}

// sign 用 secret 计算 body 的 HMAC-SHA256 十六进制摘要。
// secret 为空时不生成签名（返回空串），接收方应依赖 HTTPS 保护。
func sign(secret string, body []byte) string {
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// delay 第 attempt 次重试前的退避时长（0 起）：0.2s, 0.4s, 0.8s, 1.6s...
func delay(attempt int) time.Duration {
	base := 200 * time.Millisecond
	for i := 0; i < attempt; i++ {
		base *= 2
		if base >= 30*time.Second {
			return 30 * time.Second
		}
	}
	return base
}

// permanentError 标记不应重试的发送错误（如 4xx 客户端错误：地址失效、签名不符等）。
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// send 向 url 发送一条事件，失败时按指数退避重试。
// 返回最终 HTTP 状态码（成功时即 2xx/3xx）与最终错误（含脱敏后的 URL）。
func (s *sender) send(ctx context.Context, url string, ev Event) (int, error) {
	body, err := newPayload(ev)
	if err != nil {
		return 0, fmt.Errorf("webhook 事件编码失败: %w", err)
	}
	secret := s.secret
	if s.endpoint != nil && s.endpoint.Secret != "" {
		secret = s.endpoint.Secret
	}
	sig := sign(secret, body)
	logURL := redactURL(url)

	attempt := 0
	for {
		status, err := s.sendOnce(ctx, url, body, sig)
		if err == nil {
			return status, nil
		}
		// 不可重试错误（4xx 等）直接返回，避免无意义的重试与退避等待
		var perm *permanentError
		if errors.As(err, &perm) {
			return status, perm.err
		}
		if attempt >= s.maxRetries {
			return status, err
		}
		attempt++
		backoff := delay(attempt)
		utils.Warn("Webhook 发送失败，将重试",
			utils.String("url", logURL),
			utils.String("event", ev.Event),
			utils.Int("attempt", attempt), utils.Int("max_retries", s.maxRetries),
			utils.Duration("backoff", backoff),
			utils.Err(err))
		select {
		case <-ctx.Done():
			return status, fmt.Errorf("%w: %v", ctx.Err(), err)
		case <-time.After(backoff):
		}
	}
}

// sendOnce 执行一次 HTTP POST。2xx/3xx 视为成功并返回状态码；
// 其余状态码按可重试错误处理（4xx 客户端错误除外，见下）。
func (s *sender) sendOnce(ctx context.Context, rawURL string, body []byte, sig string) (int, error) {
	finalURL := rawURL
	// 合并端点级 query 参数（仅追加 URL 中没有的键，保留 URL 自带参数）
	if s.endpoint != nil && len(s.endpoint.Query) > 0 {
		u, err := url.Parse(finalURL)
		if err == nil {
			q := u.Query()
			for k, v := range s.endpoint.Query {
				if v != "" {
					if _, exists := q[k]; !exists {
						q.Set(k, v)
					}
				}
			}
			u.RawQuery = q.Encode()
			finalURL = u.String()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, finalURL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "fuzhan-webhook/1.0")
	if sig != "" {
		req.Header.Set(SignatureHeader, sig)
	}
	if s.endpoint != nil {
		for k, v := range s.endpoint.Headers {
			if k != "" {
				req.Header.Set(k, v)
			}
		}
	}

	start := time.Now()
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
	done := time.Since(start)
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		utils.Info("Webhook 推送成功",
			utils.String("url", redactURL(finalURL)),
			utils.Duration("elapsed", done),
			utils.String("status", resp.Status))
		return resp.StatusCode, nil
	}
	err = fmt.Errorf("Webhook 返回非成功状态码: %s", resp.Status)
	// 4xx 为客户端错误（地址失效/鉴权失败/签名不符），重试无意义；
	// 408（超时）与 429（限流）属于临时性错误，仍按可重试处理。
	if resp.StatusCode >= 400 && resp.StatusCode < 500 &&
		resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests {
		return resp.StatusCode, &permanentError{err: err}
	}
	return resp.StatusCode, err
}
