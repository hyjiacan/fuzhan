// Package webhook 提供应用内关键事件的异步 HTTP 推送能力。
//
// 职责：
//   - 定义统一的事件结构（Event），携带事件类型、来源、时间戳与业务负载；
//   - 通过进程内有界队列异步派发事件，派发绝不阻塞业务请求（满载丢弃并告警）；
//   - 对配置中 urls 列表逐个发送 HTTP POST JSON，带 HMAC-SHA256 签名与指数退避重试。
//
// 事件类型常量（event 取值）与现有功能域的语义一致，便于接收方路由与过滤：
//   - task.*：系统作业生命周期（创建/完成/失败/取消），负载为任务记录字段；
//   - security.*：安全事件（暴破/越权/限流/DDoS 等），负载为 SecurityEvent 字段；
//   - config.saved：配置保存成功。
package webhook

import "time"

// 事件类型（event 取值）
const (
	EventTaskStarted  = "task.started"  // 系统作业开始执行
	EventTaskFinished = "task.finished" // 系统作业完成
	EventTaskFailed   = "task.failed"   // 系统作业失败
	EventTaskCanceled = "task.canceled" // 系统作业被取消
	EventSecurity     = "security.event" // 安全事件
	EventConfigSaved  = "config.saved"   // 配置保存成功
)

// Event 一条待推送的 Webhook 事件。
// ID 在同一进程内单调递增的唯一标识，供接收方幂等去重。
type Event struct {
	ID        uint64      `json:"id"`
	Event     string      `json:"event"`     // 事件类型，见 Event* 常量
	Source    string      `json:"source"`    // 事件来源（模块名，如 task/security/config）
	Timestamp time.Time   `json:"timestamp"` // 事件产生时间
	Payload   interface{} `json:"payload"`   // 业务负载
}
