// Package security 提供安全事件（攻击/越权/异常请求）的记录、持久化与分析能力。
//
// 职责：
//   - 从 Gin 请求上下文抽取维度（源IP/身份/方法/路径/UA/请求ID/状态码），归一化为 SecurityEvent；
//   - 通过全局有界异步写入器落库，避免攻击场景下同步写库拖慢被拒请求的响应；
//   - 不直接绑定业务，各中间件/Handler 在判定风险时调用 FromGin + Record 即可。
package security

import (
	"errors"
	"strings"

	"fuzhan/internal/constants"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
)

// SecurityEventStore 别名：复用 repositories 中的存储接口
type SecurityEventStore = repositories.SecurityEventStore

var errNotAuthenticated = errors.New("用户未认证")

// FromGin 从 Gin 上下文补齐请求维度字段（IP/身份/方法/路径/UA/状态码/请求ID）。
// 不覆盖调用方已显式设置的字段；未提供时才回填请求维度默认值。
func FromGin(c *gin.Context, ev *models.SecurityEvent) *models.SecurityEvent {
	if c == nil {
		return ev
	}
	if ev == nil {
		ev = &models.SecurityEvent{}
	}
	if ev.ClientIP == "" {
		ev.ClientIP = utils.GetClientIP(c)
	}
	if ev.Identity == "" {
		if username, err := getUsername(c); err == nil && username != "" {
			ev.Identity = username
		} else {
			ev.Identity = "anonymous"
		}
	}
	if ev.HTTPMethod == "" {
		ev.HTTPMethod = c.Request.Method
	}
	if ev.Path == "" {
		ev.Path = c.Request.URL.RequestURI()
	}
	if ev.UserAgent == "" {
		ev.UserAgent = truncate(c.Request.UserAgent(), 512)
	}
	if ev.StatusCode == 0 {
		ev.StatusCode = c.Writer.Status()
	}
	if ev.RequestID == "" {
		ev.RequestID = getRequestID(c)
	}
	return ev
}

// RecordFromGin 一次性完成 Gin 上下文维度抽取 + 异步提交，返回是否已交给写入器。
func RecordFromGin(c *gin.Context, ev *models.SecurityEvent) bool {
	if ev == nil {
		ev = &models.SecurityEvent{}
	}
	FromGin(c, ev)
	return Record(ev)
}

// getUsername 从 Gin 上下文读取已认证用户名（未认证时返回错误）
func getUsername(c *gin.Context) (string, error) {
	v, exists := c.Get(string(constants.ContextKeyUsername))
	if !exists {
		return "", errNotAuthenticated
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", errNotAuthenticated
	}
	return s, nil
}

// getRequestID 取请求 ID（先查上下文，回退响应头）
func getRequestID(c *gin.Context) string {
	if v, exists := c.Get(string(constants.ContextKeyRequestID)); exists {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return truncate(c.GetHeader("X-Request-ID"), 64)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}
