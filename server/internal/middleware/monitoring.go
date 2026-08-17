package middleware

import (
    "strconv"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "fuzhan/internal/utils"
)

// MonitoringMiddleware 监控中间件
func MonitoringMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        clientIP := utils.GetClientIP(c)

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