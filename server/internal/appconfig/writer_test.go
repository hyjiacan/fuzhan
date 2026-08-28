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
