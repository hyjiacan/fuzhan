package webhook

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"fuzhan/internal/appconfig"
)

// TestPush 同步向当前配置的全部回调地址发送一条测试事件（管理员「测试连接」用）。
// 不使用全局队列，而是直接新建 sender 发送，便于管理接口即时获得结果。
// 返回被发送的 URL 与结果；注意：本函数忽略全局启停，仅依赖当前配置。
func TestPush(cfg appconfig.WebhookConfig, overrideURLs []string) (results []string) {
	urls := cfg.URLs
	if len(overrideURLs) > 0 {
		urls = overrideURLs
	}
	if len(urls) == 0 {
		return []string{"未配置任何回调地址（urls 为空）"}
	}
	timeout := cfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 10
	}
	maxRetries := cfg.MaxRetries
	s := &sender{
		client:     &http.Client{Timeout: time.Duration(timeout) * time.Second},
		secret:     cfg.Secret,
		maxRetries: maxRetries,
	}
	ev := Event{
		ID:        1,
		Event:     "test.ping",
		Source:    "admin",
		Timestamp: time.Now(),
		Payload:   map[string]interface{}{"message": "这是一条来自 Fuzhan 的 Webhook 测试事件"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), (time.Duration(timeout)*time.Second)*(time.Duration(maxRetries+2)))
	defer cancel()
	for _, u := range urls {
		if u == "" {
			continue
		}
		if err := s.send(ctx, u, ev); err != nil {
			results = append(results, fmt.Sprintf("%s -> 失败: %v", u, err))
		} else {
			results = append(results, fmt.Sprintf("%s -> 成功", u))
		}
	}
	if len(results) == 0 {
		results = append(results, "回调地址列表为空")
	}
	return results
}
