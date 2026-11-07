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

	// ContextKeyAuthFailureReason 401 认证失败原因上下文键（取值见下方 AuthFailure* 常量）
	ContextKeyAuthFailureReason ContextKey = "authFailureReason"
)

// 401 认证失败原因：用于区分「正常未登录」与「可疑凭证」，
// 前者日志降级且不单次入库，后者按安全事件处理。
const (
	// AuthFailureMissing 未携带任何登录凭证：正常的未登录访问
	AuthFailureMissing = "missing"
	// AuthFailureExpired 凭证已过期（签名本身有效）：登录态自然失效
	AuthFailureExpired = "expired"
	// AuthFailureInvalid 凭证无效/签名校验失败：可疑（伪造或篡改）
	AuthFailureInvalid = "invalid"
)
