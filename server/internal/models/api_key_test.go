package models

import "testing"

func TestNormalizeAllowedPaths(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"空表示所有路径", "", ""},
		{"星号表示所有路径", "*", ""},
		{"斜杠表示所有路径", "/", ""},
		{"单条路径补前导斜杠", "docs", "/docs"},
		{"多行与逗号分隔", "/docs\n/docs/public, other/sub/", "/docs\n/docs/public\n/other/sub"},
		{"去重并保留顺序", "/docs\n/docs\n/a", "/docs\n/a"},
	}
	for _, tc := range cases {
		got, err := NormalizeAllowedPaths(tc.raw)
		if err != nil {
			t.Fatalf("%s: 规范化失败: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: 期望 %q，实际 %q", tc.name, tc.want, got)
		}
	}

	if _, err := NormalizeAllowedPaths("/docs/../etc"); err == nil {
		t.Error("包含 .. 的路径应被拒绝")
	}
}

func TestAllowsPath(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		path  string
		allow bool
	}{
		{"未配置允许所有", "", "/any/file.txt", true},
		{"通配允许所有", "*", "/any/file.txt", true},
		{"精确目录", "/docs", "/docs", true},
		{"目录子路径", "/docs", "/docs/sub/a.txt", true},
		{"目录前缀不误伤", "/docs", "/docs2/a.txt", false},
		{"子目录外", "/docs/public", "/docs/private/a.txt", false},
		{"多路径命中其一", "/docs\n/media", "/media/a.txt", true},
		{"多路径均不命中", "/docs\n/media", "/other/a.txt", false},
		{"根路径默认拒绝", "/docs", "/", false},
	}
	for _, tc := range cases {
		if got := AllowsPath(tc.raw, tc.path); got != tc.allow {
			t.Errorf("%s: AllowsPath(%q, %q) = %v，期望 %v", tc.name, tc.raw, tc.path, got, tc.allow)
		}
	}
}
