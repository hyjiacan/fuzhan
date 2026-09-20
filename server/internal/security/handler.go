// Package security 提供安全事件（攻击/越权/异常请求）的记录、持久化与分析能力。
//
// 职责：
//   - 从 Gin 请求上下文抽取维度（源IP/身份/方法/路径/UA/请求ID/状态码），归一化为 SecurityEvent；
//   - 通过全局有界异步写入器落库，避免攻击场景下同步写库拖慢被拒请求的响应；
//   - 提供供管理端查询/分析安全事件的 Handler（分页过滤、未读统计、批量标记已读、趋势/TOP 统计）。
package security

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// Handler 安全事件管理处理器（仅管理员路由使用）
type Handler struct {
	repo SecurityEventStore
}

// NewHandler 创建安全事件处理器
func NewHandler(repo SecurityEventStore) *Handler {
	return &Handler{repo: repo}
}

// parseFilter 解析通用分页/过滤参数
func parseFilter(c *gin.Context) (repositories.SecurityEventFilter, int, int, error) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	f := repositories.SecurityEventFilter{
		Severity:   c.Query("severity"),
		EventType:  c.Query("eventType"),
		ClientIP:   c.Query("clientIP"),
		Identity:   c.Query("identity"),
		Result:     c.Query("result"),
		Guard:      c.Query("guard"),
		Q:          c.Query("q"),
		UnreadOnly: c.Query("unread") == "1",
	}
	if from := c.Query("from"); from != "" {
		t, err := parseDate(from)
		if err != nil {
			return f, page, pageSize, fmt.Errorf("时间格式错误")
		}
		f.From = t
	}
	if to := c.Query("to"); to != "" {
		t, err := parseDate(to)
		if err != nil {
			return f, page, pageSize, fmt.Errorf("时间格式错误")
		}
		f.To = t
	}
	return f, page, pageSize, nil
}

func parseDate(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", "2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析时间")
}

// ListEvents 分页查询安全事件（过滤 + 分页）
func (h *Handler) ListEvents(c *gin.Context) {
	f, page, pageSize, err := parseFilter(c)
	if err != nil {
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}
	list, total, err := h.repo.List(f, page, pageSize)
	if err != nil {
		utils.Error("查询安全事件失败", utils.Err(err))
		response.HandleInternalServerError(c, "查询安全事件失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"list":     list,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	})
}

// UnreadSummary 未读安全事件汇总（供管理端入口红点与登录强提醒）
func (h *Handler) UnreadSummary(c *gin.Context) {
	total, high, recent, err := h.repo.UnreadSummary(20)
	if err != nil {
		utils.Error("查询未读安全事件失败", utils.Err(err))
		response.HandleInternalServerError(c, "查询未读安全事件失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"total":  total,
		"high":   high,
		"recent": recent,
	})
}

// MarkReadRequest 标记已读请求体
type MarkReadRequest struct {
	IDs []uint `json:"ids"`
	All bool   `json:"all"`
}

// MarkRead 标记安全事件为已读（按 ID 或全部）
func (h *Handler) MarkRead(c *gin.Context) {
	var req MarkReadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求参数错误", nil)
		return
	}
	var n int64
	var err error
	if req.All || len(req.IDs) == 0 {
		n, err = h.repo.MarkAllRead()
	} else {
		n, err = h.repo.MarkRead(req.IDs)
	}
	if err != nil {
		utils.Error("标记安全事件已读失败", utils.Err(err))
		response.HandleInternalServerError(c, "标记已读失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{"affected": n})
}

// trendPoint 单日各严重级别事件数
type trendPoint struct {
	Date   string `json:"date"`
	High   int64  `json:"high"`
	Medium int64  `json:"medium"`
	Low    int64  `json:"low"`
}

// Stats 基础统计：严重级别分布、近7日趋势、TOP 来源IP、TOP 事件类型、近期锁定事件
func (h *Handler) Stats(c *gin.Context) {
	total, high, medium, low, err := h.repo.CountBySeverity()
	if err != nil {
		utils.Error("查询安全事件严重级别统计失败", utils.Err(err))
		response.HandleInternalServerError(c, "统计失败")
		return
	}

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -6).Truncate(24 * time.Hour)
	to := now.Add(time.Hour)

	trend := h.trendByDay(from, to)
	topIPs := h.topCounts("client_ip", 10, from, to, nil)
	topTypes := h.topCounts("event_type", 10, from, to, nil)
	locks := h.recentLocks(10)

	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"severity": gin.H{"total": total, "high": high, "medium": medium, "low": low},
		"trend":    trend,
		"topIPs":   topIPs,
		"topTypes": topTypes,
		"locks":    locks,
	})
}

// trendByDay 近7日按天聚合各严重级别事件数（在 Go 侧用标准库截断到 UTC 当天零点，跨数据库兼容）
func (h *Handler) trendByDay(from, to time.Time) []trendPoint {
	points := make(map[string]*trendPoint)
	add := func(d string, sev string) {
		p, ok := points[d]
		if !ok {
			p = &trendPoint{Date: d}
			points[d] = p
		}
		switch sev {
		case models.SeverityHigh:
			p.High++
		case models.SeverityMedium:
			p.Medium++
		default:
			p.Low++
		}
	}

	evs, err := h.repo.EventsInRange(from, to, nil)
	if err == nil {
		for i := range evs {
			add(evs[i].CreatedAt.UTC().Format("2006-01-02"), evs[i].Severity)
		}
	}

	// 补齐全 7 天占位（无事件的天为 0），保证折线连续
	var days []string
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		days = append(days, d.Format("2006-01-02"))
	}
	result := make([]trendPoint, 0, len(days))
	for _, day := range days {
		p, ok := points[day]
		if !ok {
			p = &trendPoint{Date: day}
		}
		result = append(result, *p)
	}
	return result
}

// columnCount TOP 聚合结果
type columnCount struct {
	Label string `json:"label"`
	N     int64  `json:"n"`
}

// topCounts 按指定列聚合 TOP（列名取自白名单，避免拼接注入）
func (h *Handler) topCounts(column string, limit int, from, to time.Time, severities []string) []columnCount {
	var counts []columnCount
	list, err := h.repo.EventsInRange(from, to, severities)
	if err != nil {
		return counts
	}
	agg := make(map[string]int64)
	for i := range list {
		var key string
		if column == "client_ip" {
			key = list[i].ClientIP
		} else {
			key = list[i].EventType
		}
		if key == "" {
			key = "未知"
		}
		agg[key]++
	}
	for label, n := range agg {
		counts = append(counts, columnCount{Label: label, N: n})
	}
	sort.Slice(counts, func(i, j int) bool { return counts[i].N > counts[j].N })
	if len(counts) > limit {
		counts = counts[:limit]
	}
	return counts
}

// recentLocks 最近触发的锁定类高危事件（登录锁定 + 共享码/口令暴破锁定）
func (h *Handler) recentLocks(limit int) []models.SecurityEvent {
	if limit <= 0 {
		limit = 10
	}
	var list []models.SecurityEvent
	evs, err := h.repo.EventsInRange(time.Time{}, time.Time{}, []string{models.SeverityHigh})
	if err != nil {
		return list
	}
	for i := range evs {
		if evs[i].EventType == models.EventAuthLocked || evs[i].EventType == models.EventCodeBruteForce {
			list = append(list, evs[i])
			if len(list) >= limit {
				break
			}
		}
	}
	return list
}
