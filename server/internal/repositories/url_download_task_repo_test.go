package repositories

import (
	"testing"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func urlTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 与生产一致：时间统一以 UTC 存储，保证 queued_at 排序稳定
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
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

// 启动对账：遗留的 downloading 任务被重新入队为 pending（不判失败），
// 并带回 TempFilePath 供调用方清理孤儿文件；pending/终态任务不受影响。
func TestURLDownloadTaskRepo_RequeueInterrupted(t *testing.T) {
	db := urlTaskTestDB(t)
	repo := NewURLDownloadTaskRepository(db)

	createURLTask(t, db, "t-downloading", models.URLDownloadStatusDownloading, "/tmp/.a.bin.uploading")
	createURLTask(t, db, "t-pending", models.URLDownloadStatusPending, "/tmp/.b.bin.uploading")
	createURLTask(t, db, "t-completed", models.URLDownloadStatusCompleted, "/tmp/.old.uploading")

	requeued, err := repo.RequeueInterrupted()
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	if len(requeued) != 1 {
		t.Fatalf("期望返回 1 条被重新入队的任务, 实际 %d", len(requeued))
	}
	if requeued[0].ID != "t-downloading" || requeued[0].TempFilePath != "/tmp/.a.bin.uploading" {
		t.Fatalf("应带回被中断任务的 TempFilePath, 实际 %+v", requeued[0])
	}

	var pendingCount, downloadingCount, completedCount int64
	db.Model(&models.URLDownloadTask{}).Where("status = ?", models.URLDownloadStatusPending).Count(&pendingCount)
	db.Model(&models.URLDownloadTask{}).Where("status = ?", models.URLDownloadStatusDownloading).Count(&downloadingCount)
	db.Model(&models.URLDownloadTask{}).Where("status = ?", models.URLDownloadStatusCompleted).Count(&completedCount)
	if downloadingCount != 0 || pendingCount != 2 {
		t.Fatalf("对账后 pending=%d downloading=%d, 期望 2/0", pendingCount, downloadingCount)
	}
	if completedCount != 1 {
		t.Fatalf("终态 completed 不应被改动, 实际 %d", completedCount)
	}
}

// 无残留 downloading 任务时应返回空且不报错。
func TestURLDownloadTaskRepo_RequeueInterrupted_Empty(t *testing.T) {
	db := urlTaskTestDB(t)
	createURLTask(t, db, "c1", models.URLDownloadStatusCompleted, "")
	requeued, err := NewURLDownloadTaskRepository(db).RequeueInterrupted()
	if err != nil {
		t.Fatalf("空对账不应报错: %v", err)
	}
	if len(requeued) != 0 {
		t.Fatalf("期望返回空, 实际 %d", len(requeued))
	}
}

// 原子领取：按序领取 pending 并置为 downloading；队列为空时返回 nil。
func TestURLDownloadTaskRepo_ClaimNextPending(t *testing.T) {
	db := urlTaskTestDB(t)
	repo := NewURLDownloadTaskRepository(db)

	if task, err := repo.ClaimNextPending(); err != nil || task != nil {
		t.Fatalf("空队列应返回 nil, got task=%v err=%v", task, err)
	}

	createURLTask(t, db, "a", models.URLDownloadStatusPending, "/tmp/.a.uploading")
	createURLTask(t, db, "b", models.URLDownloadStatusPending, "/tmp/.b.uploading")

	claimed := map[string]bool{}
	for i := 0; i < 2; i++ {
		task, err := repo.ClaimNextPending()
		if err != nil || task == nil {
			t.Fatalf("领取失败: task=%v err=%v", task, err)
		}
		if task.Status != models.URLDownloadStatusDownloading {
			t.Fatalf("领取后状态应为 downloading, got %v", task.Status)
		}
		claimed[task.ID] = true
	}
	if !claimed["a"] || !claimed["b"] {
		t.Fatalf("应分别领取到 a 与 b, got %v", claimed)
	}
	if task, _ := repo.ClaimNextPending(); task != nil {
		t.Fatalf("队列已空应返回 nil, got %v", task)
	}
}

// 自动重试重新入队：重试计数 +1、状态回到 pending、刷新入队时间以排到队尾。
func TestURLDownloadTaskRepo_RequeueForRetry(t *testing.T) {
	db := urlTaskTestDB(t)
	repo := NewURLDownloadTaskRepository(db)
	base := utils.Now().Add(-time.Hour)

	mk := func(id string, queued time.Time) {
		task := &models.URLDownloadTask{
			ID:           id,
			URL:          "http://example.com/a.bin",
			FileName:     "a.bin",
			Status:       models.URLDownloadStatusPending,
			QueuedAt:     queued,
			TargetPath:   "/public/.a.bin",
			TempFilePath: "/tmp/." + id + ".uploading",
		}
		if err := db.Create(task).Error; err != nil {
			t.Fatal(err)
		}
	}
	mk("a", base)
	mk("b", base.Add(time.Minute))

	// 按入队时间先领取 a，模拟其下载失败
	if task, err := repo.ClaimNextPending(); err != nil || task == nil || task.ID != "a" {
		t.Fatalf("应先领取 a, got task=%v err=%v", task, err)
	}

	if err := repo.RequeueForRetry("a"); err != nil {
		t.Fatalf("重新入队失败: %v", err)
	}
	got, err := repo.GetByID("a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.URLDownloadStatusPending || got.RetryCount != 1 {
		t.Fatalf("重试后应 pending 且 retry_count=1, got status=%v retry=%d", got.Status, got.RetryCount)
	}
	if !got.QueuedAt.After(base.Add(time.Minute)) {
		t.Fatalf("重试应刷新入队时间以排到队尾, got %v", got.QueuedAt)
	}

	// 队尾：b 的入队时间更早，应优先于重试后的 a 被领取
	if first, err := repo.ClaimNextPending(); err != nil || first == nil || first.ID != "b" {
		t.Fatalf("重试任务应排到队尾, 期望先领取 b, got %v err=%v", first, err)
	}
	if second, err := repo.ClaimNextPending(); err != nil || second == nil || second.ID != "a" {
		t.Fatalf("随后应领取 a, got %v err=%v", second, err)
	}
}
