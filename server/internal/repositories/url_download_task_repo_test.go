package repositories

import (
	"testing"

	"fuzhan/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func urlTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.URLDownloadTask{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

func createURLTask(t *testing.T, db *gorm.DB, id string, status models.URLDownloadStatus, tempPath string) {
	t.Helper()
	task := &models.URLDownloadTask{
		ID:           id,
		URL:          "http://example.com/a.bin",
		FileName:     "a.bin",
		Status:       status,
		TargetPath:   "/public/.a.bin",
		TempFilePath: tempPath,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
}

// 启动对账：非终态任务被收尾为失败（带错误信息与结束时间），终态任务不受影响。
func TestURLDownloadTaskRepo_ReconcileInterrupted(t *testing.T) {
	db := urlTaskTestDB(t)
	repo := NewURLDownloadTaskRepository(db)

	createURLTask(t, db, "t-downloading", models.URLDownloadStatusDownloading, "/tmp/.a.bin.uploading")
	createURLTask(t, db, "t-pending", models.URLDownloadStatusPending, "/tmp/.b.bin.uploading")
	createURLTask(t, db, "t-completed", models.URLDownloadStatusCompleted, "/tmp/.old.uploading")

	interrupted, err := repo.ReconcileInterrupted("")
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	if len(interrupted) != 2 {
		t.Fatalf("期望返回 2 条遗留任务, 实际 %d", len(interrupted))
	}
	// 应带回 TempFilePath，供调用方清理孤儿文件
	gotTemp := map[string]string{}
	for _, tk := range interrupted {
		gotTemp[tk.ID] = tk.TempFilePath
	}
	if gotTemp["t-downloading"] != "/tmp/.a.bin.uploading" || gotTemp["t-pending"] != "/tmp/.b.bin.uploading" {
		t.Fatalf("应携带 TempFilePath 定位孤儿文件, 实际 %v", gotTemp)
	}

	// 状态已更新为 failed 且带中断信息与结束时间
	var failedCount, downloadingCount, completedCount int64
	db.Model(&models.URLDownloadTask{}).Where("status = ?", models.URLDownloadStatusFailed).Count(&failedCount)
	db.Model(&models.URLDownloadTask{}).Where("status = ?", models.URLDownloadStatusDownloading).Count(&downloadingCount)
	db.Model(&models.URLDownloadTask{}).Where("status = ?", models.URLDownloadStatusCompleted).Count(&completedCount)
	if failedCount != 2 || downloadingCount != 0 {
		t.Fatalf("对账后 failed=%d downloading=%d, 期望 2/0", failedCount, downloadingCount)
	}
	if completedCount != 1 {
		t.Fatalf("终态 completed 不应被改动, 实际 %d", completedCount)
	}

	var one models.URLDownloadTask
	if err := db.Where("id = ?", "t-downloading").First(&one).Error; err != nil {
		t.Fatal(err)
	}
	if one.ErrorMessage == "" || one.CompletedAt == nil {
		t.Fatalf("中断任务应含错误信息与结束时间, err=%q completed=%v", one.ErrorMessage, one.CompletedAt)
	}
}

// 无残留非终态任务时应返回空且不报错。
func TestURLDownloadTaskRepo_ReconcileInterrupted_Empty(t *testing.T) {
	db := urlTaskTestDB(t)
	createURLTask(t, db, "c1", models.URLDownloadStatusCompleted, "")
	interrupted, err := NewURLDownloadTaskRepository(db).ReconcileInterrupted("")
	if err != nil {
		t.Fatalf("空对账不应报错: %v", err)
	}
	if len(interrupted) != 0 {
		t.Fatalf("期望返回空, 实际 %d", len(interrupted))
	}
}
