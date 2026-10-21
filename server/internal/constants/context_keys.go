package constants

// ContextKey 上下文键名常量
type ContextKey string

const (
	// ContextKeyUsername 用户名上下文键
	ContextKeyUsername ContextKey = "username"

	// ContextKeyRole 用户角色上下文键
	ContextKeyRole ContextKey = "role"

	// ContextKeyRequestID 请求ID上下文键
	ContextKeyRequestID ContextKey = "requestID"

	// ContextKeyUserID 用户ID上下文键
	ContextKeyUserID ContextKey = "userID"

	// ContextKeyUserUUID 用户UUID上下文键
	ContextKeyUserUUID ContextKey = "userUUID"

	// ContextKeySecurityGuard 安全事件防护渠道标记键（如 WebDAV 中间件 abort 前标记 guard=webdav）
	ContextKeySecurityGuard ContextKey = "securityGuard"
)
