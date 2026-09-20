package models

import (
	"time"

	"gorm.io/gorm"
)

// 安全事件严重级别
const (
	SeverityHigh   = "high"   // 高危：需管理员关注（暴破锁定、路径穿越、越权绕过等）
	SeverityMedium = "medium" // 中危：可疑但不一定恶意（单次 401/403/登录失败）
	SeverityLow    = "low"    // 低危：常规限流触发等
)

// 安全事件类型（event_type 取值）
const (
	EventAuthFailedLogin  = "auth.failed_login" // 登录/注册失败
	EventAuthLocked       = "auth.locked"       // 登录/注册失败达阈值被锁定（暴破缓解）
	EventAuthUnauthorized = "auth.unauthorized" // 认证失败（401）
	EventAccessForbidden  = "access.forbidden"  // 越权/禁止访问（403）
	EventRateLimited      = "rate.limited"      // 触发频率限制（429）
	EventCodeBruteForce   = "code.bruteforce"   // 无效分享/临时码/口令枚举达阈值被锁定
)

// 命中的防护机制（guard 取值）
const (
	GuardAuth        = "auth"        // 认证/授权
	GuardAccessguard = "accessguard" // 访问防护（下载共享/临时码限流锁定）
	GuardWebDAV      = "webdav"      // WebDAV
	GuardFTP         = "ftp"         // FTP
)

// SecurityEvent 安全事件记录
// 记录所有被识别为攻击/越权/异常的请求，支撑管理员或运维人员安全分析。
// 字段按 OWASP 日志指南设计，保证足够细、可关联、可追责。
type SecurityEvent struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	EventType  string    `gorm:"size:64;not null" json:"eventType"`              // 事件类型，见 Event* 常量
	Severity   string    `gorm:"size:16;not null;default:low" json:"severity"`   // 严重级别 high/medium/low
	ClientIP   string    `gorm:"size:45;index" json:"clientIP"`                  // 源 IP
	Identity   string    `gorm:"size:64" json:"identity"`                        // 操作者身份（用户名 或 anonymous）
	Operation  string    `gorm:"size:64" json:"operation"`                       // 操作名称（如 auth.login、file.download）
	Target     string    `gorm:"size:512" json:"target"`                         // 操作对象（路径/分享码/用户名）
	HTTPMethod string    `gorm:"size:16" json:"httpMethod"`                      // HTTP 方法
	Path       string    `gorm:"size:1024" json:"path"`                          // 请求路径+查询串
	UserAgent  string    `gorm:"size:512" json:"userAgent"`                      // 客户端 UA
	StatusCode int       `gorm:"default:0" json:"statusCode"`                    // HTTP 状态码
	Result     string    `gorm:"size:16;not null;default:failure" json:"result"` // success/failure
	Reason     string    `gorm:"size:512" json:"reason"`                         // 事件详情/错误描述
	RequestID  string    `gorm:"size:64;index" json:"requestID"`                 // 关联同一波攻击的请求 ID
	Guard      string    `gorm:"size:64" json:"guard"`                           // 命中的防护机制
	Read       bool      `gorm:"not null;default:false;index" json:"read"`       // 管理员是否已读（评估提醒未读）
	CreatedAt  time.Time `gorm:"index" json:"createdAt"`                         // 事件产生时间（UTC）
}

// TableName 指定表名
func (SecurityEvent) TableName() string {
	return "security_events"
}

// EnsureSecurityEventIndexes 创建安全事件表索引（命名格式：idx__{table}__{col1}_{col2}_...）
// 幂等：SQLite 不支持 IF NOT EXISTS，仅在索引缺失时创建。
func EnsureSecurityEventIndexes(db *gorm.DB) {
	// 未读高危事件查询：WHERE read=false AND severity=... ORDER BY created_at DESC
	name := "idx__security_events__read_severity_created"
	if db.Migrator().HasIndex(&SecurityEvent{}, name) {
		return
	}
	if err := db.Exec("CREATE INDEX " + name + " ON " + SecurityEvent{}.TableName() + " (read, severity, created_at)").Error; err != nil {
		// 索引创建失败不阻断启动（仅提示）
		return
	}
}
