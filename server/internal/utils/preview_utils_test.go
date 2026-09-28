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
	// 注意：不可把归一化后相同的 key（如 "epub" 与 ".EPUB"）放进同一个 map 字面量，
	// 否则其结果取决于 Go map 的随机迭代顺序，导致测试不稳定。
	InitMimeTable(map[string]string{
		"epub":  "application/epub+zip",
		".EXE":  "application/x-msdownload", // 忽略点号 + 大写归一
		"Txt":   "custom/text",              // 大小写归一
		"":      "application/ignore-empty", // 空扩展名忽略
		"bad":   "",                         // 空 MIME 忽略
		"nodot": "",
	})
	cases := []struct {
		ext  string
		want string
	}{
		{"epub", "application/epub+zip"},
		{"EPUB", "application/epub+zip"},    // 不区分大小写
		{"exe", "application/x-msdownload"}, // 忽略点号 + 大小写归一
		{".EXE", "application/x-msdownload"},
		{"TXT", "custom/text"},
		{"pdf", ""}, // 未配置返回空，交由调用方回退原有行为
		{"unknown", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := GetDownloadContentType(c.ext); got != c.want {
			t.Errorf("GetDownloadContentType(%q) = %q, want %q", c.ext, got, c.want)
		}
	}

	// 预览识别用的 GetPreviewMimeType 命中用户表
	if got := GetPreviewMimeType("epub"); got != "application/epub+zip" {
		t.Errorf("GetPreviewMimeType(\"epub\") = %q, want application/epub+zip", got)
	}
	if got := GetPreviewMimeType("exe"); got != "application/x-msdownload" {
		t.Errorf("GetPreviewMimeType(\"exe\") = %q, want application/x-msdownload", got)
	}
	if got := GetPreviewMimeType("pdf"); got != "" {
		t.Errorf("GetPreviewMimeType(\"pdf\") = %q, want 空(未配置需回退内置)", got)
	}
	if got := GetPreviewMimeType(""); got != "" {
		t.Errorf("GetPreviewMimeType(\"\") = %q, want 空", got)
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

	// 覆盖语义：后一次 InitMimeTable 重新加载整体生效（发生在所有前述断言之后，避免清空首次加载的 exe）
	InitMimeTable(map[string]string{"epub": "application/epub+zip"})
	if got := GetDownloadContentType("epub"); got != "application/epub+zip" {
		t.Errorf("重新加载后 GetDownloadContentType(\"epub\") = %q, want application/epub+zip", got)
	}
	InitMimeTable(map[string]string{".EPUB": "application/x-rar-compressed"})
	if got := GetDownloadContentType("epub"); got != "application/x-rar-compressed" {
		t.Errorf("冲突 key 重新加载后 GetDownloadContentType(\"epub\") = %q, want application/x-rar-compressed", got)
	}
}
