// Package admin 处理管理员后台业务
package admin

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/pkg/response"

	"github.com/gin-gonic/gin"
)

// logFileNames 允许查看的日志文件（与 utils.logger 中创建的写入器一一对应）
var logFileNames = []string{
	"app.log",
	"error.log",
	"access.log",
	"api.log",
	"webdav.log",
	"ftp.log",
	"openapi.log",
	"login.log",
	"register.log",
}

// logRotatedPattern 匹配 lumberjack 轮转产物：<name>-2026-10-25T14-30-05.123.log
var logRotatedPattern = regexp.MustCompile(
	`^(app|error|access|api|webdav|ftp|openapi|login|register)-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{3}\.log$`,
)

// logANSIRe 匹配终端颜色转义序列（旧版本 app.log/error.log 曾使用
// CapitalColorLevelEncoder 写入 ANSI 颜色码；现统一为无色编码，此处仅兜底清理历史残留）
var logANSIRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// 日志级别序，用于 level 阈值过滤
var logLevelRank = map[string]int{
	"TRACE":    0,
	"DEBUG":    1,
	"INFO":     2,
	"WARN":     3,
	"ERROR":    4,
	"CRITICAL": 5,
}

const (
	// logTailDefaultLines 默认返回行数
	logTailDefaultLines = 200
	// logTailMaxLines 单次返回行数上限
	logTailMaxLines = 2000
	// logReadBufferSize 读取缓冲区大小
	logReadBufferSize = 64 * 1024
)

// LogLine 单条日志（pos 为行首字节偏移，作为游标与前端列表 key）
type LogLine struct {
	Pos     int64  `json:"pos"`
	Time    string `json:"time"`
	Level   string `json:"level"`
	Caller  string `json:"caller"`
	Message string `json:"message"`
	Extra   string `json:"extra,omitempty"`
	Raw     string `json:"raw"`
}

// LogFileInfo 日志文件元信息
type LogFileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
}

// LogHandler 管理员日志查看处理器
type LogHandler struct{}

// NewLogHandler 创建日志查看处理器
func NewLogHandler() *LogHandler {
	return &LogHandler{}
}

// resolveLogDir 解析日志目录，口径与 resource.collector / appconfig.InitLogging 保持一致
func resolveLogDir() string {
	dir := appconfig.ExpandWorkDirPath(appconfig.GlobalConfig.Log.Directory)
	if dir == "" {
		dir = filepath.Join(appconfig.WorkDir, "logs")
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Clean(dir)
}

// isAllowedLogFile 校验文件名是否在白名单内（含轮转产物），用于阻止路径穿越
func isAllowedLogFile(name string) bool {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return false
	}
	for _, n := range logFileNames {
		if name == n {
			return true
		}
	}
	return logRotatedPattern.MatchString(name)
}

// Files 列出可查看的日志文件（仅返回实际存在的文件）
func (h *LogHandler) Files(c *gin.Context) {
	dir := resolveLogDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		// 日志目录不存在时返回空列表而非报错：允许前端在首次运行前打开页面
		response.HandleSuccess(c, 200, "ok", gin.H{"files": []LogFileInfo{}, "directory": dir})
		return
	}
	items := make([]LogFileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isAllowedLogFile(entry.Name()) {
			continue
		}
		info, ierr := entry.Info()
		if ierr != nil {
			continue
		}
		items = append(items, LogFileInfo{
			Name:    entry.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		oi, oj := logFileOrder(items[i].Name), logFileOrder(items[j].Name)
		if oi != oj {
			return oi < oj
		}
		return items[i].Name > items[j].Name
	})
	response.HandleSuccess(c, 200, "ok", gin.H{"files": items, "directory": dir})
}

// logFileOrder 固定文件排序权重；轮转产物（无固定名）排在固定文件之后
func logFileOrder(name string) int {
	for i, n := range logFileNames {
		if name == n {
			return i
		}
	}
	return len(logFileNames)
}

// Tail 读取日志尾部 / 增量 / 向前翻页。
//
// 参数：
//   - file     日志文件名（白名单内）
//   - lines    返回行数（默认 200，上限 2000）
//   - offset   增量游标（上次响应的 next_offset），只返回该偏移之后的新行
//   - before   向前翻页锚点（最旧行 pos），只返回该偏移之前的行
//   - level    最低级别阈值（trace/debug/info/warn/error/critical，空或 all 表示不过滤）
//   - keyword  关键字（对原始行做不区分大小写的包含匹配）
func (h *LogHandler) Tail(c *gin.Context) {
	file := c.DefaultQuery("file", "app.log")
	if !isAllowedLogFile(file) {
		response.HandleBadRequest(c, "不支持的日志文件", nil)
		return
	}
	path := filepath.Join(resolveLogDir(), file)
	f, err := os.Open(path)
	if err != nil {
		response.HandleNotFound(c, "日志文件不存在或不可读")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		response.HandleInternalServerError(c, "读取日志文件信息失败")
		return
	}

	maxLines := parseLogLinesParam(c.Query("lines"))
	match := buildLogMatcher(c.Query("level"), c.Query("keyword"))

	var lines []LogLine
	var nextOffset int64
	var hasMore bool

	switch {
	case c.Query("before") != "":
		before, perr := strconv.ParseInt(c.Query("before"), 10, 64)
		if perr != nil || before < 0 {
			response.HandleBadRequest(c, "before 参数非法", nil)
			return
		}
		if before > stat.Size() {
			before = stat.Size()
		}
		lines, _, hasMore, err = scanLogKeepLast(f, before, maxLines, match)
		nextOffset = stat.Size()
	case c.Query("offset") != "":
		offset, perr := strconv.ParseInt(c.Query("offset"), 10, 64)
		if perr != nil || offset < 0 {
			response.HandleBadRequest(c, "offset 参数非法", nil)
			return
		}
		// 文件被轮转或截断后偏移越界：从头重新读取，游标随之归零
		if offset > stat.Size() {
			offset = 0
		}
		lines, nextOffset, hasMore, err = scanLogFrom(f, offset, stat.Size(), maxLines, match)
	default:
		lines, nextOffset, hasMore, err = scanLogKeepLast(f, stat.Size(), maxLines, match)
	}
	if err != nil {
		response.HandleInternalServerError(c, "读取日志内容失败")
		return
	}
	if lines == nil {
		lines = []LogLine{}
	}
	response.HandleSuccess(c, 200, "ok", gin.H{
		"lines":       lines,
		"next_offset": nextOffset,
		"has_more":    hasMore,
	})
}

const (
	// logStreamCheckInterval 增量扫描间隔
	logStreamCheckInterval = 500 * time.Millisecond
	// logStreamHeartbeatInterval SSE 心跳间隔，避免代理按空闲超时断开
	logStreamHeartbeatInterval = 15 * time.Second
)

// logStreamFrame SSE 数据帧
type logStreamFrame struct {
	Lines      []LogLine `json:"lines"`
	NextOffset int64     `json:"next_offset"`
}

// Stream 以 SSE 持续推送日志增量（Tail 的实时版本）。
//
// 参数：
//   - file     日志文件名（白名单内）
//   - offset   起始游标（缺省从当前文件末尾开始，只推送之后新增的行）
//   - lines    单帧最多返回行数（默认 200，上限 2000）
//   - level    最低级别阈值
//   - keyword  关键字
func (h *LogHandler) Stream(c *gin.Context) {
	file := c.DefaultQuery("file", "app.log")
	if !isAllowedLogFile(file) {
		response.HandleBadRequest(c, "不支持的日志文件", nil)
		return
	}
	path := filepath.Join(resolveLogDir(), file)
	f, err := os.Open(path)
	if err != nil {
		response.HandleNotFound(c, "日志文件不存在或不可读")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		response.HandleInternalServerError(c, "读取日志文件信息失败")
		return
	}
	offset := stat.Size()
	if raw := c.Query("offset"); raw != "" {
		parsed, perr := strconv.ParseInt(raw, 10, 64)
		if perr != nil || parsed < 0 {
			response.HandleBadRequest(c, "offset 参数非法", nil)
			return
		}
		offset = parsed
	}
	maxLines := parseLogLinesParam(c.Query("lines"))
	match := buildLogMatcher(c.Query("level"), c.Query("keyword"))

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return
	}

	// 建连即回一帧当前游标，便于前端确认连接已生效
	if !writeLogStreamFrame(c, flusher, nil, offset) {
		return
	}

	checkTicker := time.NewTicker(logStreamCheckInterval)
	defer checkTicker.Stop()
	heartbeatTicker := time.NewTicker(logStreamHeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-heartbeatTicker.C:
			if _, err := fmt.Fprint(c.Writer, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-checkTicker.C:
			lines, next, err := readLogIncrement(f, offset, maxLines, match)
			if err != nil {
				// 读取失败不中断连接，下个周期重试
				continue
			}
			offset = next
			if len(lines) == 0 {
				continue
			}
			if !writeLogStreamFrame(c, flusher, lines, offset) {
				return
			}
		}
	}
}

// readLogIncrement 从 offset 读取新增的完整行；文件被截断或轮转后偏移越界时归零重读。
func readLogIncrement(f *os.File, offset int64, maxLines int, match logMatcher) ([]LogLine, int64, error) {
	stat, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	size := stat.Size()
	if offset > size {
		offset = 0
	}
	lines, next, _, err := scanLogFrom(f, offset, size, maxLines, match)
	if err != nil {
		return nil, offset, err
	}
	return lines, next, nil
}

// writeLogStreamFrame 写入一帧日志增量，返回 false 表示连接已断开
func writeLogStreamFrame(c *gin.Context, flusher http.Flusher, lines []LogLine, nextOffset int64) bool {
	if lines == nil {
		lines = []LogLine{}
	}
	payload, err := json.Marshal(logStreamFrame{Lines: lines, NextOffset: nextOffset})
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

// parseLogLinesParam 解析并收敛 lines 参数
func parseLogLinesParam(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return logTailDefaultLines
	}
	if n > logTailMaxLines {
		return logTailMaxLines
	}
	return n
}

// logMatcher 行过滤器
type logMatcher func(LogLine) bool

// buildLogMatcher 构建级别阈值 + 关键字过滤器；minRank 为 -1 表示不按级别过滤
func buildLogMatcher(level, keyword string) logMatcher {
	minRank := -1
	lv := strings.ToUpper(strings.TrimSpace(level))
	if lv != "" && lv != "ALL" {
		if rank, ok := logLevelRank[normalizeLogLevel(lv)]; ok {
			minRank = rank
		}
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	return func(line LogLine) bool {
		if minRank >= 0 && logLevelRank[line.Level] < minRank {
			return false
		}
		if kw != "" && !strings.Contains(strings.ToLower(line.Raw), kw) {
			return false
		}
		return true
	}
}

// normalizeLogLevel 归一化级别名称（zap 的 WARNING/FATAL/PANIC 等别名收敛到 6 级）
func normalizeLogLevel(raw string) string {
	lv := strings.ToUpper(strings.TrimSpace(raw))
	switch lv {
	case "WARNING":
		return "WARN"
	case "FATAL", "PANIC":
		return "CRITICAL"
	case "DPANIC":
		return "ERROR"
	case "TRACE", "DEBUG", "INFO", "WARN", "ERROR", "CRITICAL":
		return lv
	case "":
		return "INFO"
	}
	return lv
}

// jsonLogEntry error.log 使用 JSON 编码器写入的结构
type jsonLogEntry struct {
	Timestamp  string `json:"timestamp"`
	Level      string `json:"level"`
	Caller     string `json:"caller"`
	Msg        string `json:"msg"`
	Stacktrace string `json:"stacktrace"`
}

// parseLogLine 解析单行日志：优先按 JSON（error.log）解析，失败则按 Console 文本解析
func parseLogLine(raw string) LogLine {
	cleaned := strings.TrimRight(logANSIRe.ReplaceAllString(raw, ""), "\r")
	line := LogLine{Raw: cleaned}
	if strings.HasPrefix(strings.TrimSpace(cleaned), "{") {
		var entry jsonLogEntry
		if err := json.Unmarshal([]byte(cleaned), &entry); err == nil && entry.Msg != "" {
			line.Time = entry.Timestamp
			line.Level = normalizeLogLevel(entry.Level)
			line.Caller = entry.Caller
			line.Message = entry.Msg
			line.Extra = entry.Stacktrace
			return line
		}
	}
	// Console 编码器格式：timestamp \t level \t caller \t msg [\t {json fields}]
	parts := strings.SplitN(cleaned, "\t", 5)
	if len(parts) >= 4 {
		line.Time = strings.TrimSpace(parts[0])
		line.Level = normalizeLogLevel(parts[1])
		line.Caller = strings.TrimSpace(parts[2])
		line.Message = parts[3]
		if len(parts) == 5 {
			line.Extra = parts[4]
		}
		return line
	}
	line.Level = "INFO"
	line.Message = cleaned
	return line
}

// scanLogFrom 从 start 偏移向后读取，最多返回 maxLines 条匹配行。
// 只消费以换行结尾的完整行，尾部写了一半的行留待下次轮询。
func scanLogFrom(f *os.File, start, end int64, maxLines int, match logMatcher) ([]LogLine, int64, bool, error) {
	if start < 0 {
		start = 0
	}
	if start > end {
		start = end
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, start, false, err
	}
	reader := bufio.NewReaderSize(f, logReadBufferSize)
	lines := make([]LogLine, 0, maxLines)
	pos := start
	for pos < end {
		buf, err := reader.ReadBytes('\n')
		if len(buf) == 0 || buf[len(buf)-1] != '\n' {
			// 不完整行（文件仍在写入）或已到文件末尾
			break
		}
		line := parseLogLine(string(buf))
		line.Pos = pos
		pos += int64(len(buf))
		if match(line) {
			lines = append(lines, line)
			if len(lines) >= maxLines {
				return lines, pos, pos < end, nil
			}
		}
		if err != nil {
			break
		}
	}
	return lines, pos, false, nil
}

// scanLogKeepLast 扫描 [0, end) 前缀，保留最后 maxLines 条匹配行。
// 返回实际消费到的字节偏移与是否丢弃过更早的匹配行（has_more）。
func scanLogKeepLast(f *os.File, end int64, maxLines int, match logMatcher) ([]LogLine, int64, bool, error) {
	if end < 0 {
		end = 0
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, 0, false, err
	}
	reader := bufio.NewReaderSize(f, logReadBufferSize)
	ring := make([]LogLine, maxLines)
	count, head := 0, 0
	dropped := false
	pos := int64(0)
	for pos < end {
		buf, err := reader.ReadBytes('\n')
		if len(buf) == 0 || buf[len(buf)-1] != '\n' {
			break
		}
		line := parseLogLine(string(buf))
		line.Pos = pos
		pos += int64(len(buf))
		if match(line) {
			if count == maxLines {
				dropped = true
			} else {
				count++
			}
			ring[head] = line
			head = (head + 1) % maxLines
		}
		if err != nil {
			break
		}
	}
	out := make([]LogLine, 0, count)
	startIdx := 0
	if count == maxLines {
		startIdx = head
	}
	for i := 0; i < count; i++ {
		out = append(out, ring[(startIdx+i)%maxLines])
	}
	return out, pos, dropped, nil
}
