package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"fuzhan/internal/constants"
	"fuzhan/internal/online"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
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
		strings.HasSuffix(path, "/monitor/recent"),
		strings.HasSuffix(path, "/resource/snapshot"),
		strings.HasSuffix(path, "/resource/history"),
		strings.HasSuffix(path, "/resource/stream"):
		return true
	}
	return false
}

// MonitoringMiddleware 监控中间件
func MonitoringMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := utils.Now()
		clientIP := utils.GetClientIP(c)
		path := c.Request.URL.Path
		// WebDAV 挂载于 /api/v1/webdav，需先于 API 前缀排除，归入独立渠道
		isAPI := utils.IsAPIPath(path) && !utils.IsWebDAVPath(path)

		// 记录客户端访问（与登录无关），用于管理端"在线IP"统计
		// 状态/轮询类请求不计入，避免后台轮询将客户端 IP 误判为持续在线
		if !isNonActivityRequest(c.Request.Method, c.FullPath()) {
			online.Default.Record(clientIP, c.Request.UserAgent(), utils.Now())
		}

		utils.AppMetrics.HTTPInFlightRequests.Inc()
		defer utils.AppMetrics.HTTPInFlightRequests.Dec()

		// API 渠道：在 c.Next() 前捕获请求数据并包装响应写入器，
		// 以便请求结束后记录"做了什么/数据/结果/异常"，而非只有一条访问记录。
		var activity *apiActivity
		var capture *bodyCaptureWriter
		if isAPI {
			activity, capture = startAPIActivity(c)
		}

		c.Next()

		duration := time.Since(start)
		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method
		endpoint := c.FullPath()

		if endpoint == "" {
			endpoint = path
		}

		utils.AppMetrics.HTTPRequestTotal.WithLabelValues(method, endpoint, status).Inc()
		utils.AppMetrics.HTTPDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
		utils.AppMetrics.HTTPResponseSize.WithLabelValues(method, endpoint).Observe(float64(c.Writer.Size()))

		// 记录访问日志：按调用渠道分流到独立文件，便于分别跟踪与分析。
		fields := []zap.Field{
			utils.String("method", method),
			utils.String("path", path),
			utils.String("route", endpoint),
			utils.Int("status", c.Writer.Status()),
			utils.Duration("duration", duration),
			utils.String("client_ip", clientIP),
			utils.String("user_agent", c.Request.UserAgent()),
		}
		fields = append(fields, userIdentityFields(c)...)

		switch {
		case utils.IsWebDAVPath(path):
			utils.WebDAVAccess("WebDAV请求", fields...)
		case isAPI:
			// 补充操作详情：做了什么（action）、请求数据、结果、异常消息
			activity.finish(capture)
			fields = append(fields,
				utils.String("action", method+" "+endpoint),
				utils.String("request", activity.request),
				utils.String("result", activity.result),
			)
			if activity.errMsg != "" {
				fields = append(fields, utils.String("error", activity.errMsg))
			}
			utils.APIAccess("API操作", fields...)
		default:
			utils.Access("HTTP请求", fields...)
		}
	}
}

// userIdentityFields 提取已认证用户身份，便于按用户定位 API 调用。
func userIdentityFields(c *gin.Context) []zap.Field {
	fields := make([]zap.Field, 0, 2)
	if uid := utils.UserIDForLog(c); uid != "" {
		fields = append(fields, utils.String("user_id", uid))
	}
	if uname, ok := c.Get(string(constants.ContextKeyUsername)); ok {
		if s, ok := uname.(string); ok && s != "" {
			fields = append(fields, utils.String("username", s))
		}
	}
	return fields
}

// PrometheusHandler Prometheus指标处理器
func PrometheusHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}
