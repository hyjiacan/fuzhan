package utils

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics 监控指标
type Metrics struct {
    // HTTP请求相关指标
    HTTPRequestTotal    *prometheus.CounterVec
    HTTPDuration        *prometheus.HistogramVec
    HTTPResponseSize    *prometheus.HistogramVec
    HTTPInFlightRequests prometheus.Gauge

    // 文件操作相关指标
    FileUploadTotal     *prometheus.CounterVec
    FileDownloadTotal   *prometheus.CounterVec
    FileUploadSize      *prometheus.HistogramVec
    FileDownloadSize    *prometheus.HistogramVec

    // 临时文件相关指标
    TempFileUploadTotal *prometheus.CounterVec
    TempFileDownloadTotal *prometheus.CounterVec
    TempFileDeleteTotal *prometheus.CounterVec

    // 错误相关指标
    ErrorTotal *prometheus.CounterVec
}

// AppMetrics 应用监控指标实例
var AppMetrics *Metrics

// InitMetrics 初始化监控指标
func InitMetrics() {
    AppMetrics = &Metrics{
        // HTTP请求相关指标
        HTTPRequestTotal: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Name: "http_requests_total",
                Help: "Total number of HTTP requests",
            },
            []string{"method", "endpoint", "status"},
        ),
        HTTPDuration: promauto.NewHistogramVec(
            prometheus.HistogramOpts{
                Name:    "http_request_duration_seconds",
                Help:    "HTTP request duration in seconds",
                Buckets: prometheus.DefBuckets,
            },
            []string{"method", "endpoint"},
        ),
        HTTPResponseSize: promauto.NewHistogramVec(
            prometheus.HistogramOpts{
                Name:    "http_response_size_bytes",
                Help:    "HTTP response size in bytes",
                Buckets: prometheus.ExponentialBuckets(100, 10, 8),
            },
            []string{"method", "endpoint"},
        ),
        HTTPInFlightRequests: promauto.NewGauge(
            prometheus.GaugeOpts{
                Name: "http_in_flight_requests",
                Help: "Current number of HTTP requests being processed",
            },
        ),

        // 文件操作相关指标
        FileUploadTotal: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Name: "file_uploads_total",
                Help: "Total number of file uploads",
            },
            []string{"status"},
        ),
        FileDownloadTotal: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Name: "file_downloads_total",
                Help: "Total number of file downloads",
            },
            []string{"status"},
        ),
        FileUploadSize: promauto.NewHistogramVec(
            prometheus.HistogramOpts{
                Name:    "file_upload_size_bytes",
                Help:    "File upload size in bytes",
                Buckets: prometheus.ExponentialBuckets(1024, 2, 20),
            },
            []string{},
        ),
        FileDownloadSize: promauto.NewHistogramVec(
            prometheus.HistogramOpts{
                Name:    "file_download_size_bytes",
                Help:    "File download size in bytes",
                Buckets: prometheus.ExponentialBuckets(1024, 2, 20),
            },
            []string{},
        ),

        // 临时文件相关指标
        TempFileUploadTotal: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Name: "temp_file_uploads_total",
                Help: "Total number of temporary file uploads",
            },
            []string{"status"},
        ),
        TempFileDownloadTotal: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Name: "temp_file_downloads_total",
                Help: "Total number of temporary file downloads",
            },
            []string{"status"},
        ),
        TempFileDeleteTotal: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Name: "temp_file_deletes_total",
                Help: "Total number of temporary file deletions",
            },
            []string{"status"},
        ),

        // 错误相关指标
        ErrorTotal: promauto.NewCounterVec(
            prometheus.CounterOpts{
                Name: "errors_total",
                Help: "Total number of errors",
            },
            []string{"type", "endpoint"},
        ),
    }
}