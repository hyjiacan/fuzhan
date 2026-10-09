package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
)

// readLogFile 读取日志文件内容。
func readLogFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取日志失败 %s: %v", path, err)
	}
	return string(data)
}

// TestAPIActivityLogging 验证 API 访问日志包含"做了什么/数据/结果/异常"，且敏感字段脱敏。
func TestAPIActivityLogging(t *testing.T) {
	gin.SetMode(gin.TestMode)
	utils.InitMetrics()
	dir := t.TempDir()
	if err := utils.InitLogger(utils.LogConfig{
		Directory:    dir,
		Level:        "debug",
		AccessLog:    true,
		SplitByLevel: true,
	}); err != nil {
		t.Fatalf("初始化日志失败: %v", err)
	}
	// 关闭写入器释放文件句柄，否则 Windows 下临时目录无法清理
	defer utils.CloseLogger()

	r := gin.New()
	r.Use(MonitoringMiddleware())

	r.POST("/api/v1/echo", func(c *gin.Context) {
		var body map[string]interface{}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "绑定失败: " + err.Error()})
			return
		}
		// 请求体必须被完整还原：中间件捕获后仍能读到全部内容
		if body["name"] != "报告.pdf" {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "请求体被截断"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "ok",
			"data":    gin.H{"id": 7, "token": "should-be-redacted"},
		})
	})
	r.GET("/api/v1/fail", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": `构建目标路径失败: 无效的目标根目录: "X"`,
		})
	})

	// 成功请求：带 JSON body
	req := httptest.NewRequest(http.MethodPost, "/api/v1/echo", strings.NewReader(`{"name":"报告.pdf","password":"p@ss"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(httptest.NewRecorder(), req)

	// 失败请求：无 body
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/fail", nil))

	utils.Sync()
	content := readLogFile(t, filepath.Join(dir, "api.log"))

	// 做了什么 + 请求数据 + 结果字段
	for _, want := range []string{"API操作", "/api/v1/echo", "报告.pdf", `"action"`, `"request"`, `"result"`} {
		if !strings.Contains(content, want) {
			t.Errorf("api.log 缺少 %q\n%s", want, content)
		}
	}
	// 异常消息
	if !strings.Contains(content, "无效的目标根目录") {
		t.Errorf("api.log 未记录异常消息\n%s", content)
	}
	// 脱敏
	if strings.Contains(content, "p@ss") {
		t.Errorf("请求中的 password 未脱敏\n%s", content)
	}
	if strings.Contains(content, "should-be-redacted") {
		t.Errorf("响应中的 token 未脱敏\n%s", content)
	}
}

func TestSanitizeJSON(t *testing.T) {
	got := sanitizeJSON([]byte(`{"password":"x","nested":{"api_key":"k","keep":1},"list":[{"secret":"s"}]}`))
	for _, leaked := range []string{`"x"`, `"k"`, `"s"`} {
		if strings.Contains(got, leaked) {
			t.Errorf("敏感值 %s 未脱敏: %s", leaked, got)
		}
	}
	if !strings.Contains(got, `"keep":1`) {
		t.Errorf("非敏感字段被误改: %s", got)
	}
}

func TestSanitizeQuery(t *testing.T) {
	got := sanitizeQuery("token=abc&page=2")
	if strings.Contains(got, "abc") {
		t.Errorf("query 中的 token 未脱敏: %s", got)
	}
	if !strings.Contains(got, "page=2") {
		t.Errorf("非敏感参数被误改: %s", got)
	}
}

func TestTruncateUTF8(t *testing.T) {
	s := strings.Repeat("中", 10) // 每个 3 字节
	got := truncateUTF8(s, 4)    // 截到不足 4 字节的多字节边界
	if len(got) > 4+len("…(截断)") {
		t.Errorf("截断长度异常: %q", got)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("截断破坏了多字节字符: %q", got)
	}
}
