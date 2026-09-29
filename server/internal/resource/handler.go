package resource

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// maxHistoryPoints 历史接口最多返回的点数，超出则平均下采样
const maxHistoryPoints = 800

// streamHeartbeatInterval SSE 心跳间隔。长时间无采样（如功能未启用）时也要
// 定期发送注释帧，避免中间代理按空闲超时断开连接。
const streamHeartbeatInterval = 15 * time.Second

// Handler 服务器资源监控处理器
type Handler struct {
	collector *Collector
}

// NewHandler 创建资源监控处理器
func NewHandler(collector *Collector) *Handler {
	return &Handler{collector: collector}
}

// Snapshot 实时快照 + 最近内存采样曲线
func (h *Handler) Snapshot(c *gin.Context) {
	response.HandleSuccess(c, http.StatusOK, "", h.collector.Snapshot())
}

// Stream 以 SSE 推送实时快照：连接建立时先发一帧当前快照，此后每次采样广播一帧。
// 相比前端定时轮询，实时性更好、请求开销更低；历史曲线仍走 History 接口。
func (h *Handler) Stream(c *gin.Context) {
	ch, unsubscribe := h.collector.Subscribe()
	defer unsubscribe()

	// SSE 必需响应头；X-Accel-Buffering 关闭 Nginx 等反向代理的响应缓冲，
	// 否则事件会被代理攒批，前端看到的刷新会明显滞后。
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		utils.Warn("当前环境不支持 SSE（响应写入器不支持 Flush）")
		response.HandleInternalServerError(c, "当前环境不支持实时推送")
		return
	}

	// 建连即推送一帧，前端无需额外请求即可拿到首屏数据
	if !writeSSEFrame(c, flusher, h.collector.Snapshot()) {
		return
	}

	ticker := time.NewTicker(streamHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case rt, open := <-ch:
			if !open {
				return
			}
			if !writeSSEFrame(c, flusher, rt) {
				return
			}
		case <-ticker.C:
			// 注释帧仅用于保活，前端会忽略
			if _, err := fmt.Fprint(c.Writer, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEFrame 写入一帧数据，返回 false 表示连接已断开
func writeSSEFrame(c *gin.Context, flusher http.Flusher, rt Realtime) bool {
	payload, err := json.Marshal(rt)
	if err != nil {
		utils.Warn("序列化资源监控实时数据失败", utils.Err(err))
		return true
	}
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// History 历史查询
// 参数：scope=server|program，range=1h|6h|24h|7d（默认 24h）
func (h *Handler) History(c *gin.Context) {
	scope := c.DefaultQuery("scope", ScopeServer)
	if scope != ScopeServer && scope != ScopeProgram {
		response.HandleBadRequest(c, "无效的 scope，应为 server 或 program", nil)
		return
	}
	r := c.DefaultQuery("range", "24h")
	dur := parseRange(r)
	if dur <= 0 {
		response.HandleBadRequest(c, "无效的 range，可选 1h/6h/24h/7d", nil)
		return
	}
	now := utils.Now()
	from := now.Add(-dur)
	pts, err := h.collector.History(scope, from, now)
	if err != nil {
		utils.Warn("查询资源监控历史失败", utils.String("scope", scope), utils.Err(err))
		response.HandleInternalServerError(c, "查询资源监控历史失败")
		return
	}
	pts = downsample(pts)
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"scope":  scope,
		"range":  r,
		"points": pts,
	})
}

// parseRange 解析历史时间范围
func parseRange(r string) time.Duration {
	switch r {
	case "1h":
		return time.Hour
	case "6h":
		return 6 * time.Hour
	case "24h", "1d":
		return 24 * time.Hour
	case "7d", "7 * 24h":
		return 7 * 24 * time.Hour
	default:
		return 0
	}
}

// downsample 若点数超出上限，按相等数量分桶取平均，降低前端渲染量
func downsample(pts []ScopePoint) []ScopePoint {
	if len(pts) <= maxHistoryPoints {
		return pts
	}
	bucket := (len(pts) + maxHistoryPoints - 1) / maxHistoryPoints
	out := make([]ScopePoint, 0, maxHistoryPoints)
	for i := 0; i < len(pts); i += bucket {
		end := i + bucket
		if end > len(pts) {
			end = len(pts)
		}
		n := end - i
		var cpu, read, write, netRecv, netSent float64
		var mem, memT, used, total uint64
		for j := i; j < end; j++ {
			p := pts[j]
			cpu += p.CPU
			mem += p.Memory
			memT += p.MemoryTotal
			used += p.DiskUsed
			total += p.DiskTotal
			read += p.DiskIORead
			write += p.DiskIOWrite
			netRecv += p.NetRecv
			netSent += p.NetSent
		}
		out = append(out, ScopePoint{
			Timestamp: pts[i].Timestamp,
			Scope:     pts[i].Scope,
			Point: Point{
				CPU:         cpu / float64(n),
				Memory:      mem / uint64(n),
				MemoryTotal: memT / uint64(n),
				DiskUsed:    used / uint64(n),
				DiskTotal:   total / uint64(n),
				DiskIORead:  read / float64(n),
				DiskIOWrite: write / float64(n),
				NetRecv:     netRecv / float64(n),
				NetSent:     netSent / float64(n),
			},
		})
	}
	return out
}
