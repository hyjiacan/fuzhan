package file

import (
	"os"
	"path/filepath"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
)

// buildURLRetryPaths 只使用 task 字段与 appconfig.RootNames，可在最小 handler 上测试。
func TestBuildURLRetryPaths(t *testing.T) {
	root := t.TempDir()
	orig := appconfig.RootNames
	appconfig.RootNames = map[string]string{"docs": root, "pv": root + string(filepath.Separator) + "priv"}
	defer func() { appconfig.RootNames = orig }()

	h := &UploadSessionHandler{}
	target := filepath.Join(root, "sub", "report.pdf")

	// 带根目录的普通任务：还原根名、相对子目录，并生成专用临时上传路径
	up, tp, sDir, sRoot, sType := h.buildURLRetryPaths(&models.URLDownloadTask{
		ID: "t-1", FileName: "report.pdf", TargetPath: target,
		StorageType: models.URLDownloadStorageRegular, RootName: "docs",
	})
	if tp != target {
		t.Fatalf("目标路径应不变, got %q", tp)
	}
	if sType != models.TargetTypeRegular || sRoot != "docs" || sDir != "sub" {
		t.Fatalf("应还原 regular/docs/sub, got type=%v root=%q dir=%q", sType, sRoot, sDir)
	}
	wantUp := filepath.Join(filepath.Dir(target), ".report.pdf.t-1.retry.uploading")
	if up != wantUp {
		t.Fatalf("上传临时路径错误, got %q want %q", up, wantUp)
	}

	// 根目录下的文件：相对目录为空
	up2, _, sDir2, sRoot2, _ := h.buildURLRetryPaths(&models.URLDownloadTask{
		ID: "t-2", TargetPath: filepath.Join(root, "a.txt"),
		StorageType: models.URLDownloadStorageRegular, RootName: "docs",
	})
	if sDir2 != "" || sRoot2 != "docs" {
		t.Fatalf("根目录文件相对目录应为空, got dir=%q root=%q", sDir2, sRoot2)
	}
	if have, want := up2, filepath.Join(root, ".a.txt.t-2.retry.uploading"); have != want {
		t.Fatalf("根目录上传临时路径错误, got %q want %q", have, want)
	}

	// 私有存储：root 保留，类型为 private
	_, _, _, sRoot3, sType3 := h.buildURLRetryPaths(&models.URLDownloadTask{
		ID: "t-3", TargetPath: filepath.Join(root, "priv", "p.bin"),
		StorageType: models.URLDownloadStoragePrivate, RootName: "pv",
	})
	if sType3 != models.TargetTypePrivate || sRoot3 != "pv" {
		t.Fatalf("私有任务应还原 private/pv, got type=%v root=%q", sType3, sRoot3)
	}

	// 临时存储：类型为 temp，不依赖根名/目录
	_, tp4, sDir4, sRoot4, sType4 := h.buildURLRetryPaths(&models.URLDownloadTask{
		ID: "t-4", TargetPath: filepath.Join(root, "f.bin"),
		StorageType: models.URLDownloadStorageTemp, RootName: "",
	})
	if sType4 != models.TargetTypeTemp || sDir4 != "" || sRoot4 != "" || tp4 == "" {
		t.Fatalf("临时任务应还原 temp 且无根目录, got type=%v dir=%q root=%q tp=%q", sType4, sDir4, sRoot4, tp4)
	}

	// 空目标路径：全部为空
	up0, tp0, _, _, _ := h.buildURLRetryPaths(&models.URLDownloadTask{})
	if up0 != "" || tp0 != "" {
		t.Fatalf("空任务应返回空, got up=%q tp=%q", up0, tp0)
	}
}

// resumableUploadingPath：仅在服务器支持 Range 且存在有效的未下满部分文件时返回续传。
func TestResumableUploadingPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "r.bin")
	partial := filepath.Join(root, ".r.bin.part")
	h := &UploadSessionHandler{}

	// 无部分文件 → 不可续传
	if _, ok := h.resumableUploadingPath(&models.URLDownloadTask{
		ResumeSupported: true, TempFilePath: partial, FileSize: 1000, TargetPath: target,
	}, target); ok {
		t.Fatal("无部分文件不应续传")
	}

	// 服务器不支持 Range → 不可续传（即使有部分文件）
	if _, ok := h.resumableUploadingPath(&models.URLDownloadTask{
		TempFilePath: partial, FileSize: 1000, TargetPath: target,
	}, target); ok {
		t.Fatal("服务器不支持 Range 不应续传")
	}

	// 部分文件已下满（== 目标大小）→ 不可续传（已是完整文件）
	if err := os.WriteFile(partial, make([]byte, 1000), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.resumableUploadingPath(&models.URLDownloadTask{
		ResumeSupported: true, TempFilePath: partial, FileSize: 1000, TargetPath: target,
	}, target); ok {
		t.Fatal("已下满不应续传")
	}

	// 部分文件在目标同目录且 0<大小<目标 → 可续传
	if err := os.WriteFile(partial, make([]byte, 500), 0644); err != nil {
		t.Fatal(err)
	}
	got, ok := h.resumableUploadingPath(&models.URLDownloadTask{
		ResumeSupported: true, TempFilePath: partial, FileSize: 1000, TargetPath: target,
	}, target)
	if !ok || got != partial {
		t.Fatalf("应返回可续传的部分文件, got=%q ok=%v", got, ok)
	}

	// 空目标路径 → 不可续传
	if _, ok := h.resumableUploadingPath(&models.URLDownloadTask{
		ResumeSupported: true, TempFilePath: partial, FileSize: 1000, TargetPath: target,
	}, ""); ok {
		t.Fatal("空目标路径不应续传")
	}
}
