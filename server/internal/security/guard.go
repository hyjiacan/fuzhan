package security

import "fuzhan/internal/models"

// GuardForScope 将 accessguard 的 scope 映射为安全事件的 guard 标识。
// 使用字符串字面量匹配，避免 security 包反向导入 accessguard 造成依赖环。
func GuardForScope(scope string) string {
	switch scope {
	case "webdav":
		return models.GuardWebDAV
	case "ftp":
		return models.GuardFTP
	default:
		return models.GuardAccessguard
	}
}

// FailLockReason 返回指定 scope 失败锁定事件的中文原因描述。
func FailLockReason(scope string) string {
	switch scope {
	case "webdav":
		return "WebDAV 认证失败达阈值，已对该 IP 锁定"
	default:
		return "无效码/认证失败达阈值，已对该 IP 锁定"
	}
}
