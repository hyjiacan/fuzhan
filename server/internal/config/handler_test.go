package config

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "fuzhan/internal/appconfig"

	"github.com/gin-gonic/gin"
)

// minimalTestConfigPath 在临时目录写入一份最小可用的基础 YAML，并把它设为
// appconfig.ConfigPath，让 SaveConfig 末尾的 updates.Apply 能读取并回写。
func minimalTestConfigPath(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "test.yaml")
	if err := os.WriteFile(cfgPath, []byte("app:\n  name: 浮栈\n"), 0o644); err != nil {
		t.Fatalf("写入测试配置文件失败: %v", err)
	}
	return cfgPath
}

// postConfig 构造一个带 body 的 gin 上下文并调用 SaveConfig。
func postConfig(t *testing.T, path, body string, h *Handler) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.SaveConfig(c)
}

// TestSaveConfig_PersistsSecurity 验证安全配置（信任代理头 / CORS）经
// SaveConfig 写入 GlobalConfig 并持久化到配置文件，覆盖 security 节点保存链路。
func TestSaveConfig_PersistsSecurity(t *testing.T) {
	// 记录并恢复全局状态，避免污染同一包内其它测试
	prevPath := appconfig.ConfigPath
	prevCfg := appconfig.GlobalConfig
	appconfig.ConfigPath = minimalTestConfigPath(t)
	defer func() {
		appconfig.ConfigPath = prevPath
		appconfig.GlobalConfig = prevCfg
	}()
	appconfig.GlobalConfig = appconfig.Config{} // 关闭私有/临时存储，跳过目录创建副作用

	body := `{"security":{"trustProxy":true,"allowedOrigins":["https://a.example.com","https://b.example.com"]}}`
	postConfig(t, "/api/v1/config", body, &Handler{})

	if !appconfig.GlobalConfig.Security.TrustProxy {
		t.Error("SaveConfig 后 GlobalConfig.Security.TrustProxy = false, want true")
	}
	want := []string{"https://a.example.com", "https://b.example.com"}
	if got := appconfig.GlobalConfig.Security.AllowedOrigins; !equalStrings(got, want) {
		t.Errorf("SaveConfig 后 AllowedOrigins = %v, want %v", got, want)
	}

	out, err := os.ReadFile(appconfig.ConfigPath)
	if err != nil {
		t.Fatalf("读取持久化配置失败: %v", err)
	}
	yamlText := string(out)
	if !strings.Contains(yamlText, "trust_proxy: true") {
		t.Errorf("持久化 YAML 缺少 security.trust_proxy 节点: %s", yamlText)
	}
	if !strings.Contains(yamlText, "a.example.com") || !strings.Contains(yamlText, "b.example.com") {
		t.Errorf("持久化 YAML 缺少 allowed_origins 列表: %s", yamlText)
	}
}

// TestSaveConfig_ClearsSecurity 验证安全配置可被显式清空（置 trust_proxy 为
// false、来源为空列表），确保「清空即不限制」的语义能正确落盘。
func TestSaveConfig_ClearsSecurity(t *testing.T) {
	prevPath := appconfig.ConfigPath
	prevCfg := appconfig.GlobalConfig
	appconfig.ConfigPath = minimalTestConfigPath(t)
	defer func() {
		appconfig.ConfigPath = prevPath
		appconfig.GlobalConfig = prevCfg
	}()
	// 预置一套旧的安全配置，模拟历史值
	appconfig.GlobalConfig = appconfig.Config{
		Security: appconfig.SecurityConfig{
			TrustProxy:     true,
			AllowedOrigins: []string{"https://old.example.com"},
		},
		Storage: appconfig.StorageConfig{
			Temp: appconfig.TempConfig{Enabled: false},
		},
	}

	body := `{"security":{"trustProxy":false,"allowedOrigins":[]}}`
	postConfig(t, "/api/v1/config", body, &Handler{})

	if appconfig.GlobalConfig.Security.TrustProxy {
		t.Error("SaveConfig 清空后 TrustProxy = true, want false")
	}
	if len(appconfig.GlobalConfig.Security.AllowedOrigins) != 0 {
		t.Errorf("SaveConfig 清空后 AllowedOrigins = %v, want empty", appconfig.GlobalConfig.Security.AllowedOrigins)
	}
}

// TestSaveConfig_SecurityAbsent_KeepsValues 验证前端只提交部分节点（此处不带
// security）时，安全配置不会被误重置为默认，这是 present() 门禁的核心保证。
func TestSaveConfig_SecurityAbsent_KeepsValues(t *testing.T) {
	prevPath := appconfig.ConfigPath
	prevCfg := appconfig.GlobalConfig
	appconfig.ConfigPath = minimalTestConfigPath(t)
	defer func() {
		appconfig.ConfigPath = prevPath
		appconfig.GlobalConfig = prevCfg
	}()
	appconfig.GlobalConfig = appconfig.Config{
		Security: appconfig.SecurityConfig{
			TrustProxy:     true,
			AllowedOrigins: []string{"https://keep.example.com"},
		},
		Storage: appconfig.StorageConfig{
			Temp: appconfig.TempConfig{Enabled: false},
		},
	}

	// 只提交 app 节点名，刻意不含 security
	postConfig(t, "/api/v1/config", `{"app":{"name":"浮栈"}}`, &Handler{})

	if !appconfig.GlobalConfig.Security.TrustProxy {
		t.Error("未提交 security 时 TrustProxy 被重置 = false, want 保留 true")
	}
	if want := []string{"https://keep.example.com"}; !equalStrings(appconfig.GlobalConfig.Security.AllowedOrigins, want) {
		t.Errorf("未提交 security 时 AllowedOrigins = %v, want 保留 %v", appconfig.GlobalConfig.Security.AllowedOrigins, want)
	}
}

// equalStrings 按顺序比较两个字符串切片。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
