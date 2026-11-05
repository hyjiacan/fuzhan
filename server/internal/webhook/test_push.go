package webhook

import (
	"context"
	"net/http"
	"time"

	"fuzhan/internal/appconfig"
)

// TestResult 单个端点的测试结果（供管理接口展示，含真实 HTTP 状态码）。
type TestResult struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Success    bool   `json:"success"`
	StatusCode int    `json:"statusCode,omitempty"`
	ElapsedMs  int64  `json:"elapsedMs,omitempty"`
	Error      string `json:"error,omitempty"`
}

// TestPush 同步向配置的端点逐条发送测试事件（管理员「测试连接」用）。
// 不使用全局队列，而是直接新建 sender 发送，便于管理接口即时获得结果。
// 测试忽略端点启用状态与事件过滤（保证未启用的行也能测试），但完整应用端点级
// secret/headers/query 鉴权；未配置端点时返回一条失败提示。
func TestPush(cfg appconfig.WebhookConfig) []TestResult {
	endpoints := cfg.EffectiveEndpoints()
	if len(endpoints) == 0 {
		return []TestResult{{Error: "未配置任何回调地址（endpoints 或旧 urls 为空）"}}
	}
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 10
	}
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	ev := Event{
		ID:        1,
		Event:     "test.ping",
		Source:    "admin",
		Timestamp: time.Now(),
		Payload:   map[string]interface{}{"message": "这是一条来自 Fuzhan 的 Webhook 测试事件"},
	}

	results := make([]TestResult, 0, len(endpoints))
	for _, ep := range endpoints {
		if ep.URL == "" {
			continue
		}
		epTimeout := ep.TimeoutSeconds
		if epTimeout <= 0 {
			epTimeout = timeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), (time.Duration(epTimeout)*time.Second)*(time.Duration(maxRetries+2)))
		s := &sender{
			client:     &http.Client{Timeout: time.Duration(epTimeout) * time.Second},
			secret:     cfg.Secret,
			maxRetries: maxRetries,
			endpoint:   &ep,
		}
		start := time.Now()
		status, err := s.send(ctx, ep.URL, ev)
		elapsed := time.Since(start).Milliseconds()
		cancel()

		r := TestResult{
			Name:       ep.Name,
			URL:        redactURL(ep.URL),
			StatusCode: status,
			ElapsedMs:  elapsed,
		}
		if err != nil {
			r.Success = false
			r.Error = err.Error()
		} else {
			r.Success = true
		}
		results = append(results, r)
	}
	return results
}
