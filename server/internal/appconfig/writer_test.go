package appconfig

import (
    "testing"
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
