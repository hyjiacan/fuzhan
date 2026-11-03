package index

import (
	"os"
	"path/filepath"
	"testing"

	"fuzhan/internal/models"
)

// statusOf 读取指定路径索引记录的状态（不存在时返回空字符串）
func statusOf(t *testing.T, syncer *Syncer, rootName, relPath string) models.FileStatus {
	t.Helper()
	var rec models.FileRecordPublic
	if err := syncer.db.Where("root_name = ? AND file_path = ?", rootName, relPath).
		Order("id DESC").First(&rec).Error; err != nil {
		t.Fatalf("查询索引记录失败: %v", err)
	}
	return rec.Status
}

// 覆盖上传通过 os.Rename 替换目标文件时，Windows 会先上报目标路径的 Remove，
// 此时文件仍在磁盘上。watcher 不应据此软删索引，否则记录会从界面消失。
func TestWatcherRemoveIgnoresExistingFile(t *testing.T) {
	db := setupTestDB(t)
	root := t.TempDir()
	rootNames := map[string]string{"docs": root}
	syncer := NewSyncer(db, rootNames)
	w := NewWatcher(syncer, rootNames)

	writeFile(t, filepath.Join(root, "a.txt"), "old")
	if err := syncer.SyncFile("docs", "/a.txt"); err != nil {
		t.Fatalf("SyncFile 失败: %v", err)
	}

	// 模拟覆盖：内容已替换，目标路径始终存在，收到瞬时 Remove 事件
	writeFile(t, filepath.Join(root, "a.txt"), "new")
	w.handleRemove("docs", "a.txt")

	if got := statusOf(t, syncer, "docs", "/a.txt"); got != models.FileStatusActive {
		t.Fatalf("路径仍存在时不应软删索引，期望 active，实际 %s", got)
	}

	// 真实删除：路径不存在，应正常软删
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	w.handleRemove("docs", "a.txt")

	if got := statusOf(t, syncer, "docs", "/a.txt"); got != models.FileStatusDeleted {
		t.Fatalf("路径已删除时应软删索引，期望 deleted，实际 %s", got)
	}
}

// 上传落盘期间目标路径被登记为"上传中"，即使此刻路径暂时不存在，
// watcher 也必须忽略 Remove，避免覆盖上传的索引记录被误删。
func TestWatcherRemoveSkipsUploadTarget(t *testing.T) {
	db := setupTestDB(t)
	root := t.TempDir()
	rootNames := map[string]string{"docs": root}
	syncer := NewSyncer(db, rootNames)
	w := NewWatcher(syncer, rootNames)
	svc := &Service{uploadTargets: make(map[string]int)}
	w.isUploadTarget = svc.IsUploadTarget

	writeFile(t, filepath.Join(root, "a.txt"), "old")
	if err := syncer.SyncFile("docs", "/a.txt"); err != nil {
		t.Fatalf("SyncFile 失败: %v", err)
	}

	// 上传流程登记时使用索引路径（带前导 /），watcher 查询使用相对路径（无前导 /）
	unregister := svc.BeginUploadTarget("docs", "/a.txt")
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatal(err)
	}
	w.handleRemove("docs", "a.txt")
	if got := statusOf(t, syncer, "docs", "/a.txt"); got != models.FileStatusActive {
		t.Fatalf("上传中目标不应被软删，期望 active，实际 %s", got)
	}

	// 注销后恢复为普通删除语义
	unregister()
	w.handleRemove("docs", "a.txt")
	if got := statusOf(t, syncer, "docs", "/a.txt"); got != models.FileStatusDeleted {
		t.Fatalf("注销后应按真实删除处理，期望 deleted，实际 %s", got)
	}
}
