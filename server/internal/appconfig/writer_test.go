package appconfig

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestToYamlKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"name", "name"},
		{"appName", "app_name"},
		{"serverPort", "server_port"},
		{"maxFileSize", "max_file_size"},
		{"defaultExpireDays", "default_expire_days"},
	}

	for _, tt := range tests {
		result := ToYamlKey(tt.input)
		if result != tt.expected {
			t.Errorf("ToYamlKey(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestConfigUpdates(t *testing.T) {
	updates := NewConfigUpdates()
	updates.Add("key1", "value1")
	updates.Add("key2", 123)
	updates.Add("key3", true)

	if updates["key1"] != "value1" {
		t.Errorf("key1 = %q, want value1", updates["key1"])
	}
	if updates["key2"] != 123 {
		t.Errorf("key2 = %v, want 123", updates["key2"])
	}
	if updates["key3"] != true {
		t.Errorf("key3 = %v, want true", updates["key3"])
	}
}

func TestParseYamlPath(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"app.name", []string{"app", "name"}},
		{"server.port", []string{"server", "port"}},
		{"a.b.c", []string{"a", "b", "c"}},
		{"single", []string{"single"}},
		{"a..b", []string{"a", "b"}},
	}

	for _, tt := range tests {
		result := parseYamlPath(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("parseYamlPath(%q) = %v, want %v", tt.input, result, tt.expected)
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("parseYamlPath(%q)[%d] = %q, want %q", tt.input, i, v, tt.expected[i])
			}
		}
	}
}

// TestSaveWebhookEndpointsNested 验证端点的嵌套结构（列表内 map + 内部 list/map/bool/int）能正确序列化回读。
func TestSaveWebhookEndpointsNested(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/fuzhan.yaml"
	orig := "webhook:\n  enabled: false\n"
	if err := os.WriteFile(configPath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	endpoints := []interface{}{
		map[string]interface{}{
			"name":      "文件事件",
			"url":       "https://example.com/hook/file",
			"enabled":   true,
			"secret":    "s3cr3t",
			"events":    []interface{}{"file.*", "task.started"},
			"headers":   map[string]interface{}{"Authorization": "Bearer abc"},
			"query":     map[string]interface{}{"token": "tok123"},
			"timeout_seconds": 5,
		},
		map[string]interface{}{
			"url":    "https://example.com/hook/all",
			"events": []interface{}{},
		},
	}

	updates := NewConfigUpdates()
	updates.Add("webhook.endpoints", endpoints)

	if err := updates.Apply(configPath); err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取结果失败: %v", err)
	}
	t.Logf("写入结果:\n%s", string(content))

	// 重新解析，确认嵌套结构字段类型正确（不再是 "[a b]" 字符串）
	var doc struct {
		Webhook struct {
			Endpoints []WebhookEndpoint `yaml:"endpoints"`
		} `yaml:"webhook"`
	}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	if len(doc.Webhook.Endpoints) != 2 {
		t.Fatalf("Endpoints 长度 = %d, want 2", len(doc.Webhook.Endpoints))
	}
	first := doc.Webhook.Endpoints[0]
	if first.Name != "文件事件" || first.URL != "https://example.com/hook/file" {
		t.Errorf("第一条端点字段不符: %+v", first)
	}
	if first.Enabled == nil || !*first.Enabled {
		t.Errorf("enabled 应为 true: %+v", first.Enabled)
	}
	if first.Secret != "s3cr3t" {
		t.Errorf("secret 丢失: %q", first.Secret)
	}
	if len(first.Events) != 2 || first.Events[0] != "file.*" || first.Events[1] != "task.started" {
		t.Errorf("events 序列解析错误: %v", first.Events)
	}
	if first.Headers["Authorization"] != "Bearer abc" {
		t.Errorf("headers 解析错误: %v", first.Headers)
	}
	if first.Query["token"] != "tok123" {
		t.Errorf("query 解析错误: %v", first.Query)
	}
	if first.TimeoutSeconds != 5 {
		t.Errorf("timeout_seconds 解析错误: %d", first.TimeoutSeconds)
	}
	if second := doc.Webhook.Endpoints[1]; second.URL != "https://example.com/hook/all" {
		t.Errorf("第二条端点 URL 不符: %+v", second)
	}
}

// TestSaveConfigEmptySequence 验证空序列（如清空允许扩展名）能正确写回配置文件
func TestSaveConfigEmptySequence(t *testing.T) {
	dir := t.TempDir()
	configPath := dir + "/fuzhan.yaml"
	orig := "storage:\n  allowed_extensions:\n    - .jpg\n    - .png\n"
	if err := os.WriteFile(configPath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	updates := NewConfigUpdates()
	updates.Add("storage.allowed_extensions", []interface{}{})

	if err := updates.Apply(configPath); err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取结果失败: %v", err)
	}
	t.Logf("写入结果:\n%s", string(content))

	// 重新解析，确认 allowed_extensions 为空序列
	var doc struct {
		Storage struct {
			AllowedExtensions []string `yaml:"allowed_extensions"`
		} `yaml:"storage"`
	}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		t.Fatalf("解析结果失败: %v", err)
	}
	if doc.Storage.AllowedExtensions != nil && len(doc.Storage.AllowedExtensions) != 0 {
		t.Errorf("AllowedExtensions 应为空，实为 %v", doc.Storage.AllowedExtensions)
	}
}
