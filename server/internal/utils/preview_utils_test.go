package utils

import "testing"

func TestIsPreviewable_EmptyConfigMeansAllPreviewable(t *testing.T) {
	InitPreviewConfig("", "")
	if !IsPreviewable("/a/b.bin", "application/octet-stream") {
		t.Fatalf("空预览配置应允许所有文件预览")
	}
	if !IsPreviewable("/a/noext", "") {
		t.Fatalf("空预览配置应允许无扩展名文件预览")
	}
}

func TestIsPreviewable_ConfiguredEnforced(t *testing.T) {
	InitPreviewConfig("text/*,image/png", "md,log")
	// MIME 匹配
	if !IsPreviewable("/a/f.txt", "text/plain") {
		t.Fatalf("text/plain 应可预览")
	}
	if !IsPreviewable("/a/img.png", "image/png") {
		t.Fatalf("image/png 应可预览")
	}
	// 扩展名匹配（补充）
	if !IsPreviewable("/a/notes.md", "application/octet-stream") {
		t.Fatalf(".md 应可预览")
	}
	// reachthrough: config 非空时其它类型仍受限
	if IsPreviewable("/a/video.mp4", "video/mp4") {
		t.Fatalf("已配置预览类型时 video/mp4 不应可预览")
	}
}

func TestMimeTable_DownloadContentType(t *testing.T) {
	InitMimeTable(map[string]string{
		"epub":  "application/epub+zip",
		".EPUB": "application/x-rar-compressed", // 不区分大小写、忽略点号，应覆盖上面
		"exe":   "application/x-msdownload",
		"txt":   "custom/text",
		"":      "application/ignore-empty", // 空扩展名忽略
		"bad":   "",                         // 空 MIME 忽略
	})
	cases := []struct {
		ext  string
		want string
	}{
		{"epub", "application/x-rar-compressed"}, // 不区分大小写
		{"EPUB", "application/x-rar-compressed"}, // 忽略点号
		{"exe", "application/x-msdownload"},
		{"txt", "custom/text"},
		{"pdf", ""}, // 未配置返回空，交由调用方回退原有行为
		{"unknown", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := GetDownloadContentType(c.ext); got != c.want {
			t.Errorf("GetDownloadContentType(%q) = %q, want %q", c.ext, got, c.want)
		}
	}

	// 预览识别的 GetMimeType 不受用户表影响，仅走内置表兜底
	if got := GetMimeType("txt"); got != "text/plain" {
		t.Errorf("GetMimeType(\"txt\") 不应受用户表影响，= %q, want text/plain", got)
	}
	if got := GetMimeType("exe"); got != "application/octet-stream" {
		t.Errorf("GetMimeType(\"exe\") = %q, want octet-stream", got)
	}
	if got := GetMimeType("pdf"); got != "application/pdf" {
		t.Errorf("GetMimeType(\"pdf\") = %q, want application/pdf", got)
	}
}
