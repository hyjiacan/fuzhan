// Package webhook 提供应用内关键事件的异步 HTTP 推送能力。
//
// 职责：
//   - 定义统一的事件结构（Event），携带事件类型、来源、时间戳与业务负载；
//   - 通过进程内有界队列异步派发事件，派发绝不阻塞业务请求（满载丢弃并告警）；
//   - 对配置中 endpoints（或旧 urls）逐个发送 HTTP POST JSON，带 HMAC-SHA256 签名与指数退避重试。
package webhook

import "time"

// 事件类型（event 取值）。触发点要求：
//   - task.*、security.event 来自各自模块；
//   - 其余事件在 middleware.LogOperation 中按 operation 映射统一派发（见 audit.go）。
const (
	EventTaskStarted  = "task.started"   // 系统作业开始执行
	EventTaskFinished = "task.finished"  // 系统作业完成
	EventTaskFailed   = "task.failed"    // 系统作业失败
	EventTaskCanceled = "task.canceled"  // 系统作业被取消
	EventSecurity     = "security.event" // 安全事件
	EventStorageAlert = "storage.alert"  // 存储使用率达到告警阈值
)

// EventCatalogItem 事件目录条目（供管理接口/前端动态渲染）。
type EventCatalogItem struct {
	Event    string `json:"event"`
	Desc     string `json:"desc"`
	Category string `json:"category"`
}

// EventCatalog 返回完整事件目录。前端据此动态渲染事件选项，避免前后端列表漂移。
// 事件过滤语法：空列表表示不接收任何事件（默认拒绝）；支持精确（file.uploaded）、
// 前缀通配（file.*）与 `*`（全部）。
func EventCatalog() []EventCatalogItem {
	return []EventCatalogItem{
		// 作业与安全
		{EventTaskStarted, "系统作业开始执行（扫描/哈希/清理/URL 下载等）", "任务"},
		{EventTaskFinished, "系统作业完成", "任务"},
		{EventTaskFailed, "系统作业失败（含进程重启中断的对账记录）", "任务"},
		{EventTaskCanceled, "系统作业被取消", "任务"},
		{EventSecurity, "安全事件（登录暴破/越权拒绝/频率限制/DDoS 告警）", "安全"},
		{EventStorageAlert, "存储使用率达到配置的告警阈值", "系统"},
		// 文件
		{"file.upload_started", "创建上传会话，开始上传文件", "文件"},
		{"file.upload_canceled", "取消上传会话", "文件"},
		{"file.uploaded", "上传会话完成，文件写入最终路径", "文件"},
		{"file.renamed", "文件/目录重命名", "文件"},
		{"file.moved", "文件/目录移动", "文件"},
		{"file.deleted", "文件/目录删除", "文件"},
		// 私有文件、临时文件、URL 下载任务
		{"private.downloaded", "私有文件被下载", "私有文件"},
		{"private.deleted", "私有文件被删除", "私有文件"},
		{"temp.downloaded", "临时文件被下载", "临时文件"},
		{"temp.deleted", "临时文件被删除", "临时文件"},
		{"temp.upload_canceled", "临时文件上传会话取消", "临时文件"},
		{"url_task.canceled", "URL 下载任务取消", "URL 任务"},
		{"url_task.retried", "URL 下载任务重试", "URL 任务"},
		{"url_task.deleted", "URL 下载任务删除", "URL 任务"},
		{"url_task.checksummed", "URL 下载任务校验和更新", "URL 任务"},
		// 用户与认证
		{"user.registered", "新用户注册", "用户"},
		{"user.created", "管理员创建用户", "用户"},
		{"user.deleted", "管理员删除用户", "用户"},
		{"user.status_changed", "启用/禁用用户", "用户"},
		{"user.password_changed", "密码修改/重置", "用户"},
		{"auth.login_success", "本地/OAuth2 登录成功", "认证"},
		{"auth.login_failed", "本地/OAuth2 登录失败（含限流锁定）", "认证"},
	}
}

// Event 一条待推送的 Webhook 事件。
// ID 在同一进程内单调递增的唯一标识，供接收方幂等去重。
type Event struct {
	ID        uint64      `json:"id"`
	Event     string      `json:"event"`     // 事件类型，见 Event* 常量与 EventCatalog
	Source    string      `json:"source"`    // 事件来源（模块名，如 task/security/config/audit）
	Timestamp time.Time   `json:"timestamp"` // 事件产生时间
	Payload   interface{} `json:"payload"`   // 业务负载
}
