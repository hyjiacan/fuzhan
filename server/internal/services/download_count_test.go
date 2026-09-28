package services

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"fuzhan/internal/models"
)

// newCountTestDB 构建含 file_records_public 的临时库
func newCountTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.FileRecordPublic{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() {
		if s, err := db.DB(); err == nil {
			_ = s.Close()
		}
	})
	return db
}

// TestDownloadCountRecorderBatch 验证增量异步攒批后能按路径正确批量落库。
func TestDownloadCountRecorderBatch(t *testing.T) {
	db := newCountTestDB(t)
	now := time.Now()
	paths := []string{"/root/a.pdf", "/root/b.pdf", "/root/c.pdf"}
	for _, p := range paths {
		rec := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
			FileName: p[6:], RootName: "root", FullPath: p, ModTime: now,
			LastSyncedAt: now, CreatedAt: now, UpdatedAt: now, Status: models.FileStatusActive,
		}}
		if err := db.Create(&rec).Error; err != nil {
			t.Fatalf("写入记录失败: %v", err)
		}
	}

	InitDownloadCountRecorder(db)

	// a 下载 3 次、b 下载 2 次、c 下载 1 次
	for i := 0; i < 3; i++ {
		SubmitDownloadCountIncrement(paths[0])
	}
	for i := 0; i < 2; i++ {
		SubmitDownloadCountIncrement(paths[1])
	}
	SubmitDownloadCountIncrement(paths[2])

	// 停止即排空落库
	StopDownloadCountRecorder()

	// 校验计数
	want := map[string]int64{paths[0]: 3, paths[1]: 2, paths[2]: 1}
	for path, n := range want {
		var got models.FileRecordPublic
		if err := db.Where("full_path = ?", path).First(&got).Error; err != nil {
			t.Fatalf("查找 %s 失败: %v", path, err)
		}
		if got.DownloadCount != n {
			t.Fatalf("%s 期望计数 %d，实际 %d", path, n, got.DownloadCount)
		}
	}
}
