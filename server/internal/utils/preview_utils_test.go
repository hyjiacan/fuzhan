package utils

import (
	"net/http"
	"strings"
	"testing"
)

func TestIsPreviewable_CustomEmptyMatchesNothing(t *testing.T) {
	// custom 组与内置分组同语义：仅已配置条目参与匹配，两组留空不命中任何文件
	InitPreviewConfig(PreviewInitConfig{Enabled: true, Groups: map[string]PreviewGroupState{
		PreviewGroupImage:    {Enabled: true, Mimes: "image/*", Exts: "png"},
		PreviewGroupText:     {Enabled: true, Mimes: "text/*", Exts: ""},
		PreviewGroupAudio:    {Enabled: true, Mimes: "", Exts: ""},
		PreviewGroupVideo:    {Enabled: true, Mimes: "", Exts: ""},
		PreviewGroupDocument: {Enabled: true, Mimes: "", Exts: ""},
		PreviewGroupCustom:   {Enabled: true, Mimes: "", Exts: ""},
	}})
	if IsPreviewable("/a/b.bin", "application/octet-stream") {
		t.Fatalf("custom 组两组留空不应命中任何文件")
	}
	if IsPreviewable("/a/noext", "") {
		t.Fatalf("custom 组两组留空不应允许无扩展名文件预览")
	}

	// 配置后仅已配置条目生效
	InitPreviewConfig(PreviewInitConfig{Enabled: true, Groups: map[string]PreviewGroupState{
		PreviewGroupImage:    {Enabled: true, Mimes: "image/*", Exts: "png"},
		PreviewGroupText:     {Enabled: true, Mimes: "text/*", Exts: ""},
		PreviewGroupAudio:    {Enabled: true, Mimes: "", Exts: ""},
		PreviewGroupVideo:    {Enabled: true, Mimes: "", Exts: ""},
		PreviewGroupDocument: {Enabled: true, Mimes: "", Exts: ""},
		PreviewGroupCustom:   {Enabled: true, Mimes: "", Exts: "bin"},
	}})
	if !IsPreviewable("/a/b.bin", "application/octet-stream") {
		t.Fatalf("custom 组配置的扩展名列表应生效")
	}
	if IsPreviewable("/a/c.exe", "application/octet-stream") {
		t.Fatalf("未命中 custom 已配置列表的文件不应可预览")
	}
}

func TestIsPreviewable_GroupListsEnforced(t *testing.T) {
	// 关闭音视频分组、自定义组关闭：未命中任何启用分组列表的文件不可预览
	InitPreviewConfig(PreviewInitConfig{Enabled: true, Groups: map[string]PreviewGroupState{
		PreviewGroupImage:    {Enabled: true, Mimes: "image/*", Exts: "png,jpg"},
		PreviewGroupText:     {Enabled: true, Mimes: "text/*", Exts: "md,log"},
		PreviewGroupAudio:    {Enabled: false, Mimes: "audio/*", Exts: "mp3"},
		PreviewGroupVideo:    {Enabled: false, Mimes: "video/*", Exts: "mp4"},
		PreviewGroupDocument: {Enabled: true, Mimes: "application/pdf", Exts: "pdf"},
		PreviewGroupCustom:   {Enabled: false, Mimes: "application/json", Exts: "json"},
	}})
	if !IsPreviewable("/a/f.txt", "text/plain") {
		t.Fatalf("text/* 应命中文本组")
	}
	if !IsPreviewable("/a/img.png", "image/png") {
		t.Fatalf("image/png 应命中图片组")
	}
	if !IsPreviewable("/a/notes.md", "application/octet-stream") {
		t.Fatalf(".md 应命中文本组扩展名列表")
	}
	if IsPreviewable("/a/video.mp4", "video/mp4") {
		t.Fatalf("视频分组关闭时 video/mp4 不应可预览")
	}
	if IsPreviewable("/a/f.json", "application/json") {
		t.Fatalf("自定义组关闭时其列表不应参与判定")
	}

	// 开启音视频分组后按各自列表直接可预览
	g := defaultPreviewInitConfig().Groups
	InitPreviewConfig(PreviewInitConfig{Enabled: true, Groups: g})
	if !IsPreviewable("/a/video.mp4", "video/mp4") {
		t.Fatalf("视频分组开启时 video/mp4 应可预览")
	}
	if !IsPreviewable("/a/song.mp3", "audio/mpeg") {
		t.Fatalf("音频分组开启时 audio/mpeg 应可预览")
	}
}

func TestIsPreviewable_ExplicitEmptyGroupMatchesNothing(t *testing.T) {
	// 分组显式留空：该组不命中任何文件（custom 组同语义）
	InitPreviewConfig(PreviewInitConfig{Enabled: true, Groups: map[string]PreviewGroupState{
		PreviewGroupImage:    {Enabled: true, Mimes: "", Exts: ""},
		PreviewGroupText:     {Enabled: true, Mimes: "", Exts: ""},
		PreviewGroupAudio:    {Enabled: true, Mimes: "", Exts: ""},
		PreviewGroupVideo:    {Enabled: true, Mimes: "", Exts: ""},
		PreviewGroupDocument: {Enabled: true, Mimes: "", Exts: ""},
		PreviewGroupCustom:   {Enabled: false, Mimes: "image/*", Exts: "png"},
	}})
	if IsPreviewable("/a/img.png", "image/png") {
		t.Fatalf("内置分组列表留空时不应命中任何文件")
	}
}

func TestIsPreviewable_GlobalSwitch(t *testing.T) {
	InitPreviewConfig(PreviewInitConfig{Enabled: false, Groups: defaultPreviewInitConfig().Groups})
	if IsPreviewable("/a/f.txt", "text/plain") {
		t.Fatalf("全局预览开关关闭时任何文件都不应可预览")
	}
	if IsPreviewable("/a/b.bin", "application/octet-stream") {
		t.Fatalf("全局预览开关关闭时任何文件都不应可预览")
	}
	InitPreviewConfig(PreviewInitConfig{Enabled: true, Groups: defaultPreviewInitConfig().Groups})
	if !IsPreviewable("/a/f.txt", "text/plain") {
		t.Fatalf("全局预览开关开启后应恢复可预览")
	}
}

func TestIsPreviewable_DefaultCustomDisabled(t *testing.T) {
	// 默认配置下 custom 组关闭：未命中任何内置分组默认列表的文件不可预览
	InitPreviewConfig(defaultPreviewInitConfig())
	if IsPreviewable("/a/b.bin", "application/octet-stream") {
		t.Fatalf("默认配置下 custom 组应关闭，二进制文件不应可预览")
	}
	if IsPreviewable("/a/f.mkv", "video/x-matroska") {
		t.Fatalf("非浏览器原生类型不应出现在默认表中")
	}
	if IsPreviewable("/a/f.heic", "image/heic") {
		t.Fatalf("非浏览器原生类型不应出现在默认表中")
	}
	if !IsPreviewable("/a/f.webm", "video/webm") {
		t.Fatalf("浏览器原生视频类型应可预览")
	}
}

func TestClassifyPreviewGroup(t *testing.T) {
	InitPreviewConfig(defaultPreviewInitConfig())
	cases := []struct {
		ext, mime, want string
	}{
		{"png", "image/png", "image"},
		{"svg", "", "image"},
		{"mp3", "audio/mpeg", "audio"},
		{"flac", "", "audio"},
		{"mp4", "video/mp4", "video"},
		{"webm", "", "video"},
		{"pdf", "application/pdf", "document"},
		{"txt", "text/plain", "text"},
		{"go", "", "text"},
		{"ts", "", "text"}, // TypeScript 默认归文本而非视频
		{"xyz", "application/octet-stream", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := ClassifyPreviewGroup(c.ext, c.mime); got != c.want {
			t.Errorf("ClassifyPreviewGroup(%q, %q) = %q, want %q", c.ext, c.mime, got, c.want)
		}
	}

	// 分组关闭后不再参与归类
	g := defaultPreviewInitConfig().Groups
	g[PreviewGroupAudio] = PreviewGroupState{Enabled: false, Mimes: DefaultPreviewGroupMimes(PreviewGroupAudio), Exts: DefaultPreviewGroupExts(PreviewGroupAudio)}
	InitPreviewConfig(PreviewInitConfig{Enabled: true, Groups: g})
	if got := ClassifyPreviewGroup("mp3", "audio/mpeg"); got != "" {
		t.Errorf("音频分组关闭时 ClassifyPreviewGroup 应返回空，got %q", got)
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

func TestApplyUnsafePreviewFallback(t *testing.T) {
	cases := []struct {
		name     string
		fileName string
		wantHit  bool
	}{
		{name: "html triggers fallback", fileName: "evil.html", wantHit: true},
		{name: "svg triggers fallback", fileName: "logo.svg", wantHit: true},
		{name: "js triggers fallback", fileName: "app.js", wantHit: true},
		{name: "mjs triggers fallback", fileName: "app.mjs", wantHit: true},
		{name: "json triggers fallback", fileName: "data.json", wantHit: true},
		{name: "xml triggers fallback", fileName: "a.xml", wantHit: true},
		{name: "uppercase extension triggers fallback", fileName: "A.HTML", wantHit: true},
		{name: "safe txt does not", fileName: "notes.txt", wantHit: false},
		{name: "safe pdf does not", fileName: "doc.pdf", wantHit: false},
		{name: "no extension does not", fileName: "README", wantHit: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := http.Header{}
			got := ApplyUnsafePreviewFallback(h, c.fileName)
			if got != c.wantHit {
				t.Fatalf("ApplyUnsafePreviewFallback(%q) = %v, want %v", c.fileName, got, c.wantHit)
			}
			if !c.wantHit {
				return
			}
			if ct := h.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
				t.Fatalf("Content-Type = %q, want text/plain; charset=utf-8", ct)
			}
			if cd := h.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") {
				t.Fatalf("Content-Disposition = %q, want attachment", cd)
			}
		})
	}
}

func TestApplyUnsafePreviewFallback_HeaderInjectionSanitized(t *testing.T) {
	// 文件名含 CR/LF/引号时：filename="..." 段必须清洗为下划线，整体不得出现裸 CR/LF
	// （filename* 为 RFC5987 的 %-encode 形式 "\r\n"->"%0D%0A"，是安全的 ASCII 序列，不产生真实换行）。
	h := http.Header{}
	ApplyUnsafePreviewFallback(h, "evil\r\nX-Injected: 1.html")
	cd := h.Get("Content-Disposition")
	if strings.ContainsAny(cd, "\r\n") {
		t.Fatalf("Content-Disposition 含裸 CR/LF（响应头注入）: %q", cd)
	}
	// 只检查 filename="..." 段（filename* 的 %-encode 是安全 ASCII，不产生真实换行/注入）
	filenameSegment := cd[:strings.Index(cd, "; filename*")]
	if !strings.HasPrefix(filenameSegment, "attachment; filename=\"evil__X-Injected: 1.html\"") {
		t.Fatalf("filename 段应把 CR/LF 清洗为下划线且无注入: %q", filenameSegment)
	}

	h2 := http.Header{}
	ApplyUnsafePreviewFallback(h2, `a"b\c.html`)
	cd2 := h2.Get("Content-Disposition")
	if strings.ContainsAny(cd2, "\r\n") {
		t.Fatalf("Content-Disposition 含裸 CR/LF: %q", cd2)
	}
	if !strings.Contains(cd2, "filename=\"a_b_c.html\"") {
		t.Fatalf("filename 段应把引号/反斜杠清洗为下划线: %q", cd2)
	}
}
