package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
)

// apiActivity 记录一次 API 请求的“做了什么 / 数据 / 结果 / 异常”。
// 请求体仅捕获 JSON；响应体仅捕获 JSON。为便于排查不设长度上限，
// 但仅缓存 JSON 响应，避免文件下载等大响应被整体驻留内存。
type apiActivity struct {
	request string // 请求数据（query + JSON body，已脱敏）
	result  string // 响应结果（成功时的 data，已脱敏）
	errMsg  string // 异常消息（失败时的响应 message）
}

// bodyCaptureWriter 透传响应写入，同时旁路缓存 JSON 响应体用于日志。
// 仅覆盖 Write/WriteString，其余方法由内嵌的 gin.ResponseWriter 提供。
type bodyCaptureWriter struct {
	gin.ResponseWriter
	buf bytes.Buffer
}

func (w *bodyCaptureWriter) Write(b []byte) (int, error) {
	w.capture(b)
	return w.ResponseWriter.Write(b)
}

func (w *bodyCaptureWriter) WriteString(s string) (int, error) {
	w.capture([]byte(s))
	return w.ResponseWriter.WriteString(s)
}

// capture 仅缓存 JSON 响应体；非 JSON（文件下载、SSE 流等）不缓存，
// 既保证日志完整可读，又避免大响应被整体驻留内存。
func (w *bodyCaptureWriter) capture(b []byte) {
	if !isJSONContentType(w.Header().Get("Content-Type")) {
		return
	}
	w.buf.Write(b)
}

func isJSONContentType(ct string) bool {
	return strings.Contains(strings.ToLower(ct), "application/json")
}

// startAPIActivity 在 c.Next() 前调用：捕获请求数据并包装响应写入器。
func startAPIActivity(c *gin.Context) (*apiActivity, *bodyCaptureWriter) {
	act := &apiActivity{request: captureRequestData(c)}
	w := &bodyCaptureWriter{ResponseWriter: c.Writer}
	c.Writer = w
	return act, w
}

// finish 在 c.Next() 后调用：解析响应体，提取结果与异常消息。
func (a *apiActivity) finish(w *bodyCaptureWriter) {
	// 仅解析 JSON 响应；文件下载等二进制响应只保留状态码，不做解析
	if !isJSONContentType(w.Header().Get("Content-Type")) {
		return
	}
	raw := bytes.TrimSpace(w.buf.Bytes())
	if len(raw) == 0 {
		return
	}
	var resp struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return
	}
	a.result = sanitizeJSON(resp.Data)
	if !resp.Success {
		a.errMsg = resp.Message
	}
}

// captureRequestData 汇总请求数据：查询串 + JSON 请求体（均已脱敏）。
func captureRequestData(c *gin.Context) string {
	parts := make([]string, 0, 2)
	if q := utils.SanitizeQuery(c.Request.URL.RawQuery); q != "" {
		parts = append(parts, "?"+q)
	}
	if b := captureJSONBody(c); b != "" {
		parts = append(parts, b)
	}
	return strings.Join(parts, " ")
}

// captureJSONBody 完整读取并还原 JSON 请求体，保证后续绑定仍能读到全部内容。
// 非 JSON 请求体（如分片上传的 multipart）不读取，避免无谓的内存占用。
func captureJSONBody(c *gin.Context) string {
	if c.Request.Body == nil {
		return ""
	}
	if !isJSONContentType(c.Request.Header.Get("Content-Type")) {
		return ""
	}
	orig := c.Request.Body
	buf, err := io.ReadAll(orig)
	// 还原请求体，保证后续绑定仍能读到完整内容
	c.Request.Body = io.NopCloser(bytes.NewReader(buf))
	if err != nil {
		return ""
	}
	return sanitizeJSON(buf)
}

// sanitizeJSON 解析 JSON 后脱敏并重新序列化；不设长度上限。
// 解析/序列化失败时按文本兜底脱敏，绝不原样输出，避免密钥落盘。
func sanitizeJSON(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err == nil {
		redactSensitive(v)
		if out, merr := json.Marshal(v); merr == nil {
			return string(out)
		}
	}
	return scrubJSONText(string(raw))
}

// redactSensitive 递归替换敏感字段的值。
func redactSensitive(v interface{}) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			if utils.IsSensitiveKey(k) {
				t[k] = "***"
				continue
			}
			redactSensitive(val)
		}
	case []interface{}:
		for _, item := range t {
			redactSensitive(item)
		}
	}
}

// jsonStringFieldRe 匹配 JSON 中的 "key": "value" 字符串字段。
var jsonStringFieldRe = regexp.MustCompile(`"([^"\\]*(?:\\.[^"\\]*)*)"\s*:\s*"((?:[^"\\]|\\.)*)"`)

// scrubJSONText 在 JSON 无法解析时，按字段名对 "key":"value" 做兜底脱敏。
func scrubJSONText(s string) string {
	return jsonStringFieldRe.ReplaceAllStringFunc(s, func(m string) string {
		colon := strings.Index(m, ":")
		if colon < 0 {
			return m
		}
		key := strings.Trim(strings.TrimSpace(m[:colon]), `"`)
		if !utils.IsSensitiveKey(key) {
			return m
		}
		return `"` + key + `": "***"`
	})
}
