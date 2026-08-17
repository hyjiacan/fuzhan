package migration

import (
    "bytes"
    "encoding/json"
    "strings"
    "testing"
    "time"
)

// TestChannelEventEmitter_Basic tests basic event emission through channel
func TestChannelEventEmitter_Basic(t *testing.T) {
    emitter := NewChannelEventEmitter(10)

    event := &MigrationEvent{
        Stage:    string(StagePreparing),
        Progress: 5,
        Message:  "Starting...",
    }

    emitter.Emit(event)

    // Receive from channel
    select {
    case received := <-emitter.Channel():
        if received.Stage != event.Stage {
            t.Errorf("预期 stage 为 %s，实际为 %s", event.Stage, received.Stage)
        }
        if received.Progress != event.Progress {
            t.Errorf("预期 progress 为 %d，实际为 %d", event.Progress, received.Progress)
        }
    case <-time.After(100 * time.Millisecond):
        t.Error("未收到事件")
    }

    emitter.Close()
}

func timeAfter(dur string) chan struct{} {
    // Simple stub - in real code this would be time.After
    return nil
}

// TestChannelEventEmitter_BufferFull tests behavior when buffer is full
func TestChannelEventEmitter_BufferFull(t *testing.T) {
    emitter := NewChannelEventEmitter(2) // Small buffer

    // Fill the buffer
    emitter.Emit(&MigrationEvent{Stage: string(StagePreparing), Progress: 0})
    emitter.Emit(&MigrationEvent{Stage: string(StageBackup), Progress: 10})

    // This should not block, event will be dropped
    emitter.Emit(&MigrationEvent{Stage: string(StageDataExport), Progress: 20})

    // Should still receive events
    count := 0
    for count < 2 {
        select {
        case <-emitter.Channel():
            count++
        case <-time.After(100 * time.Millisecond):
            break
        }
    }

    if count != 2 {
        t.Errorf("预期接收 2 个事件，实际接收 %d 个", count)
    }

    emitter.Close()
}

// TestChannelEventEmitter_Close tests channel closure
func TestChannelEventEmitter_Close(t *testing.T) {
    emitter := NewChannelEventEmitter(10)

    emitter.Emit(&MigrationEvent{Stage: string(StageComplete), Progress: 100})
    emitter.Close()

    // Drain any remaining events first
    ch := emitter.Channel()
    for {
        _, ok := <-ch
        if !ok {
            break
        }
    }

    // Now try to receive again - should get zero value from closed channel
    _, ok := <-ch
    if ok {
        t.Error("Channel 应已关闭")
    }

    // Emit after close should be no-op
    emitter.Emit(&MigrationEvent{Stage: string(StageError), Progress: 0})
}

// TestSSEEventEmitter_Basic tests SSE event emission
func TestSSEEventEmitter_Basic(t *testing.T) {
    var buf bytes.Buffer
    emitter := NewSSEEventEmitter(&buf)

    event := &MigrationEvent{
        Stage:           string(StageDataImport),
        Progress:        60,
        Message:         "Importing data...",
        TablesCompleted: 3,
        TablesTotal:     5,
    }

    emitter.Emit(event)

    output := buf.String()

    if !strings.HasPrefix(output, "data: ") {
        t.Error("SSE 输出应以 'data: ' 开头")
    }

    if !strings.HasSuffix(output, "\n\n") {
        t.Error("SSE 输出应以 '\\n\\n' 结尾")
    }

    // Verify JSON content
    jsonPart := strings.TrimPrefix(output, "data: ")
    jsonPart = strings.TrimSuffix(jsonPart, "\n\n")

    var parsed MigrationEvent
    if err := json.Unmarshal([]byte(jsonPart), &parsed); err != nil {
        t.Fatalf("无法解析 SSE 数据: %v", err)
    }

    if parsed.Stage != event.Stage {
        t.Errorf("stage 不匹配: %s != %s", parsed.Stage, event.Stage)
    }
}

// TestSSEEventEmitter_Flush tests flush functionality
func TestSSEEventEmitter_Flush(t *testing.T) {
    // Test with buffer that doesn't implement flusher
    var buf bytes.Buffer
    emitter := NewSSEEventEmitter(&buf)

    err := emitter.Flush()
    if err != nil {
        t.Errorf("Flush 不应返回错误: %v", err)
    }
}

// TestSSEEventEmitter_Close tests SSE close
func TestSSEEventEmitter_Close(t *testing.T) {
    var buf bytes.Buffer
    emitter := NewSSEEventEmitter(&buf)

    err := emitter.Close()
    if err != nil {
        t.Errorf("Close 不应返回错误: %v", err)
    }

    output := buf.String()
    if !strings.Contains(output, `"stage":"complete"`) {
        t.Error("关闭时应发送 complete 事件")
    }
}

// TestBufferedEventEmitter_BothChannels tests buffered emitter sends to both channels
func TestBufferedEventEmitter_BothChannels(t *testing.T) {
    var buf bytes.Buffer
    emitter := NewBufferedEventEmitter(&buf, 10)

    event := &MigrationEvent{
        Stage:    string(StageVerify),
        Progress: 95,
        Message:  "Verifying...",
    }

    emitter.Emit(event)

    // Check SSE output
    if buf.Len() == 0 {
        t.Error("SSE 输出不应为空")
    }

    // Check channel output
    select {
    case received := <-emitter.Channel():
        if received.Progress != event.Progress {
            t.Errorf("channel progress 不匹配: %d != %d", received.Progress, event.Progress)
        }
    case <-time.After(100 * time.Millisecond):
        t.Error("未从 Channel 接收到事件")
    }
}

// TestBufferedEventEmitter_Close tests closing buffered emitter
func TestBufferedEventEmitter_Close(t *testing.T) {
    var buf bytes.Buffer
    emitter := NewBufferedEventEmitter(&buf, 10)

    emitter.Close()

    // SSE should have complete event
    if !strings.Contains(buf.String(), `"stage":"complete"`) {
        t.Error("SSE 输出应包含 complete 事件")
    }

    // Channel should be closed
    _, ok := <-emitter.Channel()
    if ok {
        t.Error("Channel 应已关闭")
    }
}

// TestFormatSSEMessage tests SSE message formatting
func TestFormatSSEMessage(t *testing.T) {
    tests := []struct {
        eventType string
        data      string
        expected  string
    }{
        {"", `{"stage":"complete"}`, "data: {\"stage\":\"complete\"}\n\n"},
        {"migration", `{"progress":100}`, "event: migration\ndata: {\"progress\":100}\n\n"},
        {"error", "error message", "event: error\ndata: error message\n\n"},
    }

    for _, tt := range tests {
        result := FormatSSEMessage(tt.eventType, tt.data)
        if result != tt.expected {
            t.Errorf("FormatSSEMessage(%q, %q) = %q, want %q", tt.eventType, tt.data, result, tt.expected)
        }
    }
}

// TestMigrationEvent_JSON tests JSON serialization of MigrationEvent
func TestMigrationEvent_JSON(t *testing.T) {
    event := &MigrationEvent{
        Stage:              string(StageDataImport),
        Progress:           75,
        Message:            "Testing JSON",
        Current:            "users",
        RecordsMigrated:    1000,
        TablesCompleted:    5,
        TablesTotal:        10,
        RollbackAvailable:  true,
    }

    data, err := json.Marshal(event)
    if err != nil {
        t.Fatalf("JSON 序列化失败: %v", err)
    }

    var parsed MigrationEvent
    if err := json.Unmarshal(data, &parsed); err != nil {
        t.Fatalf("JSON 反序列化失败: %v", err)
    }

    if parsed.Stage != event.Stage {
        t.Errorf("Stage 不匹配: %s != %s", parsed.Stage, event.Stage)
    }

    if parsed.Progress != event.Progress {
        t.Errorf("Progress 不匹配: %d != %d", parsed.Progress, event.Progress)
    }

    if parsed.RollbackAvailable != event.RollbackAvailable {
        t.Errorf("RollbackAvailable 不匹配: %v != %v", parsed.RollbackAvailable, event.RollbackAvailable)
    }
}

