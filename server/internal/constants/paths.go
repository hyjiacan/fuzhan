package constants

// 请求路径前缀常量：作为按渠道归类日志与请求的唯一来源，
// 避免路径字符串散落在路由与中间件中导致判定不一致。

const (
	// WebDAVPathPrefix WebDAV 统一端点的挂载前缀。
	WebDAVPathPrefix = "/api/v1/webdav"

	// APIPathPrefix HTTP API 前缀。
	APIPathPrefix = "/api/"
)
