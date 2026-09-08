package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"fuzhan/internal/online"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// isNonActivityRequest 判断是否为自动轮询/状态类请求。
// 这类请求由前端定时轮询发起、并非用户主动操作，不应影响客户端 IP 的
// “在线”判定与请求计数（否则后台轮询会让 IP 永远在线）。
func isNonActivityRequest(method, path string) bool {
	if method != http.MethodGet {
		return false
	}
	switch {
	case strings.HasSuffix(path, "/scan/status"),
		strings.HasSuffix(path, "/scan/progress"),
		strings.HasSuffix(path, "/online-ips"),
		strings.HasSuffix(path, "/tasks"),
		strings.HasSuffix(path, "/tasks/history"),
		strings.HasSuffix(path, "/monitor/recent"):
		return true
	}
	return false
}

// MonitoringMiddleware 监控中间件
func MonitoringMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		clientIP := utils.GetClientIP(c)

		// 记录客户端访问（与登录无关），用于管理端"在线IP"统计
		// 状态/轮询类请求不计入，避免后台轮询将客户端 IP 误判为持续在线
		if !isNonActivityRequest(c.Request.Method, c.FullPath()) {
			online.Default.Record(clientIP, c.Request.UserAgent(), time.Now())
		}

		utils.AppMetrics.HTTPInFlightRequests.Inc()
		defer utils.AppMetrics.HTTPInFlightRequests.Dec()

		c.Next()

		duration := time.Since(start)
		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method
		endpoint := c.FullPath()

		if endpoint == "" {
			endpoint = c.Request.URL.Path
		}

		utils.AppMetrics.HTTPRequestTotal.WithLabelValues(method, endpoint, status).Inc()
		utils.AppMetrics.HTTPDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
		utils.AppMetrics.HTTPResponseSize.WithLabelValues(method, endpoint).Observe(float64(c.Writer.Size()))

		// 记录访问日志
		utils.Access("HTTP请求",
			utils.String("method", method),
			utils.String("path", endpoint),
			utils.Int("status", c.Writer.Status()),
			utils.Duration("duration", duration),
			utils.String("client_ip", clientIP),
			utils.String("user_agent", c.Request.UserAgent()),
		)
	}
}

// PrometheusHandler Prometheus指标处理器
func PrometheusHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}
