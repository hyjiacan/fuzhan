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
