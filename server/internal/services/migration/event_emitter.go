package migration

import (
    "encoding/json"
    "fmt"
    "io"
    "strings"
    "sync"
)

// MigrationEvent 迁移事件
type MigrationEvent struct {
    Stage              string `json:"stage"`               // 当前阶段
    Progress           int    `json:"progress"`            // 进度 0-100
    Message            string `json:"message"`             // 消息
    Current            string `json:"current,omitempty"`   // 当前处理项
    Error              string `json:"error,omitempty"`     // 错误信息
    RollbackAvailable  bool   `json:"rollbackAvailable"`   // 是否可回滚
    RecordsMigrated    int64  `json:"recordsMigrated"`     // 已迁移记录数
    TablesCompleted    int    `json:"tablesCompleted"`     // 已完成表数
    TablesTotal        int    `json:"tablesTotal"`         // 总表数
}

// EventEmitter 事件发送器接口
type EventEmitter interface {
    // Emit 发送事件
    Emit(event *MigrationEvent)
    // Flush 刷新缓冲区
    Flush() error
    // Close 关闭
    Close() error
}

// SSEEventEmitter SSE 格式事件发送器
type SSEEventEmitter struct {
    writer io.Writer
    mu     sync.Mutex
}

// NewSSEEventEmitter 创建 SSE 事件发送器
func NewSSEEventEmitter(w io.Writer) *SSEEventEmitter {
    return &SSEEventEmitter{
        writer: w,
    }
}

// Emit 发送 SSE 事件
func (e *SSEEventEmitter) Emit(event *MigrationEvent) {
    e.mu.Lock()
    defer e.mu.Unlock()

    data, err := json.Marshal(event)
    if err != nil {
        fmt.Fprintf(e.writer, "data: %s\n\n", fmt.Sprintf(`{"stage":"error","message":"序列化事件失败: %v"}`, err))
        return
    }

    // SSE 格式: data: {...}\n\n
    fmt.Fprintf(e.writer, "data: %s\n\n", string(data))
}

// Flush 刷新缓冲区
func (e *SSEEventEmitter) Flush() error {
    if f, ok := e.writer.(flusher); ok {
        return f.Flush()
    }
    return nil
}

// Close 关闭
func (e *SSEEventEmitter) Close() error {
    // 发送结束事件
    e.Emit(&MigrationEvent{
        Stage:    "complete",
        Progress: 100,
        Message:  "迁移完成",
    })
    return e.Flush()
}

type flusher interface {
    Flush() error
}

// ChannelEventEmitter 基于 Channel 的事件发送器
type ChannelEventEmitter struct {
    ch     chan *MigrationEvent
    closed bool
    mu     sync.Mutex
}

// NewChannelEventEmitter 创建基于 Channel 的事件发送器
func NewChannelEventEmitter(bufferSize int) *ChannelEventEmitter {
    if bufferSize <= 0 {
        bufferSize = 100
    }
    return &ChannelEventEmitter{
        ch: make(chan *MigrationEvent, bufferSize),
    }
}

// Emit 发送事件到 Channel
func (e *ChannelEventEmitter) Emit(event *MigrationEvent) {
    e.mu.Lock()
    defer e.mu.Unlock()

    if e.closed {
        return
    }

    select {
    case e.ch <- event:
    default:
        // Channel 已满，丢弃事件
    }
}

// Channel 返回事件 Channel
func (e *ChannelEventEmitter) Channel() <-chan *MigrationEvent {
    return e.ch
}

// Close 关闭 Channel
func (e *ChannelEventEmitter) Close() error {
    e.mu.Lock()
    defer e.mu.Unlock()

    if !e.closed {
        e.closed = true
        close(e.ch)
    }
    return nil
}

// Flush 空实现
func (e *ChannelEventEmitter) Flush() error {
    return nil
}

// BufferedEventEmitter 缓冲事件发送器 (同时支持 SSE 和 Channel)
type BufferedEventEmitter struct {
    *SSEEventEmitter
    *ChannelEventEmitter
}

// NewBufferedEventEmitter 创建缓冲事件发送器
func NewBufferedEventEmitter(w io.Writer, bufferSize int) *BufferedEventEmitter {
    return &BufferedEventEmitter{
        SSEEventEmitter:   NewSSEEventEmitter(w),
        ChannelEventEmitter: NewChannelEventEmitter(bufferSize),
    }
}

// Emit 同时发送到 SSE 和 Channel
func (e *BufferedEventEmitter) Emit(event *MigrationEvent) {
    e.SSEEventEmitter.Emit(event)
    e.ChannelEventEmitter.Emit(event)
}

// Close 关闭所有资源
func (e *BufferedEventEmitter) Close() error {
    if err := e.SSEEventEmitter.Close(); err != nil {
        return err
    }
    return e.ChannelEventEmitter.Close()
}

// FormatSSEMessage 格式化 SSE 消息
func FormatSSEMessage(eventType, data string) string {
    var builder strings.Builder
    if eventType != "" {
        builder.WriteString(fmt.Sprintf("event: %s\n", eventType))
    }
    builder.WriteString(fmt.Sprintf("data: %s\n\n", data))
    return builder.String()
}