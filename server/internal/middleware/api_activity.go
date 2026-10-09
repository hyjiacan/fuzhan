package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// maxAPICaptureBytes 单条 API 日志中请求/响应体的最大字节数。
// 目的是让日志能说清“做了什么、结果如何”，又不被大响应（文件列表、下载流）刷爆。
const maxAPICaptureBytes = 2048

// sensitiveKeys 需要脱敏的字段名（小写匹配）。日志会长期留存，
// 密码/令牌/密钥这类值绝不能落盘。
var sensitiveKeys = map[string]struct{}{
	"password":      {},
	"old_password":  {},
	"new_password":  {},
	"password_hash": {},
	"token":         {},
	"access_token":  {},
	"refresh_token": {},
	"jwt":           {},
	"secret":        {},
	"client_secret": {},
	"api_key":       {},
	"apikey":        {},
	"authorization": {},
	"private_key":   {},
	"key":           {},
}

// apiActivity 记录一次 API 请求的“做了什么 / 数据 / 结果 / 异常”。
// 请求体仅捕获 JSON，响应体仅捕获 JSON，均限长，避免影响性能与日志体积。
type apiActivity struct {
	request string // 请求数据（query + JSON body，已脱敏限长）
	result  string // 响应结果（成功时的 data，已脱敏限长）
	errMsg  string // 异常消息（失败时的响应 message）
}

// bodyCaptureWriter 透传响应写入，同时旁路缓存前 maxBytes 字节用于日志。
// 仅覆盖 Write/WriteString，其余方法由内嵌的 gin.ResponseWriter 提供。
type bodyCaptureWriter struct {
	gin.ResponseWriter
	buf       bytes.Buffer
	maxBytes  int
	truncated bool
}

func (w *bodyCaptureWriter) Write(b []byte) (int, error) {
	w.capture(b)
	return w.ResponseWriter.Write(b)
}

func (w *bodyCaptureWriter) WriteString(s string) (int, error) {
	w.capture([]byte(s))
	return w.ResponseWriter.WriteString(s)
}

func (w *bodyCaptureWriter) capture(b []byte) {
	remain := w.maxBytes - w.buf.Len()
	if remain <= 0 {
		if len(b) > 0 {
			w.truncated = true
		}
		return
	}
	if len(b) <= remain {
		w.buf.Write(b)
		return
	}
	w.buf.Write(b[:remain])
	w.truncated = true
}

// startAPIActivity 在 c.Next() 前调用：捕获请求数据并包装响应写入器。
func startAPIActivity(c *gin.Context) (*apiActivity, *bodyCaptureWriter) {
	act := &apiActivity{request: captureRequestData(c)}
	w := &bodyCaptureWriter{ResponseWriter: c.Writer, maxBytes: maxAPICaptureBytes}
	c.Writer = w
	return act, w
}

// finish 在 c.Next() 后调用：解析响应体，提取结果与异常消息。
func (a *apiActivity) finish(w *bodyCaptureWriter) {
	// 仅解析 JSON 响应；文件下载等二进制响应只保留状态码，不做解析
	if !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
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

// captureRequestData 汇总请求数据：查询串 + JSON 请求体（均已脱敏限长）。
func captureRequestData(c *gin.Context) string {
	parts := make([]string, 0, 2)
	if q := sanitizeQuery(c.Request.URL.RawQuery); q != "" {
		parts = append(parts, "?"+q)
	}
	if b := captureJSONBody(c); b != "" {
		parts = append(parts, b)
	}
	return strings.Join(parts, " ")
}

// captureJSONBody 读取并还原 JSON 请求体（限长），保证后续绑定仍能读到完整内容。
func captureJSONBody(c *gin.Context) string {
	if c.Request.Body == nil {
		return ""
	}
	if !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
		return ""
	}
	orig := c.Request.Body
	buf, err := io.ReadAll(io.LimitReader(orig, maxAPICaptureBytes+1))
	// 无论是否截断，都把已读内容与剩余内容拼回，避免破坏后续读取
	c.Request.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), orig))
	if err != nil {
		return ""
	}
	return sanitizeJSON(buf)
}

// sanitizeQuery 对查询串按字段名脱敏（如 ?token=xxx）。
func sanitizeQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}
	for k := range values {
		if isSensitiveKey(k) {
			values.Set(k, "***")
		}
	}
	return values.Encode()
}

// sanitizeJSON 解析 JSON 后脱敏并重新序列化，最后限长；解析失败时按原文限长。
func sanitizeJSON(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return truncateUTF8(string(raw), maxAPICaptureBytes)
	}
	redactSensitive(v)
	out, err := json.Marshal(v)
	if err != nil {
		return truncateUTF8(string(raw), maxAPICaptureBytes)
	}
	return truncateUTF8(string(out), maxAPICaptureBytes)
}

// redactSensitive 递归替换敏感字段的值。
func redactSensitive(v interface{}) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			if isSensitiveKey(k) {
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

func isSensitiveKey(k string) bool {
	_, ok := sensitiveKeys[strings.ToLower(k)]
	return ok
}

// truncateUTF8 按字节上限截断，且不切断多字节字符。
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…(截断)"
}
