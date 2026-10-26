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
	"time"

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

// send 向 url 发送一条事件，失败时按指数退避重试，返回最终错误（含 URL）。
func (s *sender) send(ctx context.Context, url string, ev Event) error {
	body, err := newPayload(ev)
	if err != nil {
		return fmt.Errorf("webhook 事件编码失败: %w", err)
	}
	sig := sign(s.secret, body)

	attempt := 0
	for {
		err := s.sendOnce(ctx, url, body, sig)
		if err == nil {
			return nil
		}
		// 不可重试错误（4xx 等）直接返回，避免无意义的重试与退避等待
		var perm *permanentError
		if errors.As(err, &perm) {
			return perm.err
		}
		if attempt >= s.maxRetries {
			return err
		}
		attempt++
		backoff := delay(attempt)
		utils.Warn("Webhook 发送失败，将重试",
			utils.String("url", url),
			utils.String("event", ev.Event),
			utils.Int("attempt", attempt), utils.Int("max_retries", s.maxRetries),
			utils.Duration("backoff", backoff),
			utils.Err(err))
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %v", ctx.Err(), err)
		case <-time.After(backoff):
		}
	}
}

// sendOnce 执行一次 HTTP POST。2xx/3xx 视为成功；其余状态码按可重试错误处理。
func (s *sender) sendOnce(ctx context.Context, url string, body []byte, sig string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "fuzhan-webhook/1.0")
	if sig != "" {
		req.Header.Set(SignatureHeader, sig)
	}

	start := time.Now()
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
	done := time.Since(start)
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		utils.Info("Webhook 推送成功",
			utils.String("url", url),
			utils.Duration("elapsed", done),
			utils.String("status", resp.Status))
		return nil
	}
	err = fmt.Errorf("Webhook 返回非成功状态码: %s", resp.Status)
	// 4xx 为客户端错误（地址失效/签名不符），重试无意义；
	// 408（超时）与 429（限流）属于临时性错误，仍按可重试处理。
	if resp.StatusCode >= 400 && resp.StatusCode < 500 &&
		resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests {
		return &permanentError{err: err}
	}
	return err
}
