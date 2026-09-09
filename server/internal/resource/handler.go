package resource

import (
	"net/http"
	"time"

	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// maxHistoryPoints 历史接口最多返回的点数，超出则平均下采样
const maxHistoryPoints = 800

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
		var cpu, read, write float64
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
			},
		})
	}
	return out
}
