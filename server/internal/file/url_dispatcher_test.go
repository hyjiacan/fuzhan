package file

import (
	"sync/atomic"
	"testing"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func dispatcherTestRepo(t *testing.T) *repositories.URLDownloadTaskRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:dispatcher_test?mode=memory&cache=shared"), &gorm.Config{
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
	return repositories.NewURLDownloadTaskRepository(db)
}

// armTimer 三分支：队列为空不设、有未到期任务则设、额度已满不设。
func TestURLDownloadDispatcher_ArmTimer(t *testing.T) {
	repo := dispatcherTestRepo(t)
	d := NewURLDownloadDispatcher(repo, nil, 1)

	// 1) 队列为空：不 arm
	d.armTimer()
	if d.timer.Stop() {
		t.Fatal("空队列不应 arm 定时器")
	}

	// 2) 存在未到期任务：arm
	task := &models.URLDownloadTask{
		ID:         "future",
		URL:        "http://example.com/a.bin",
		FileName:   "a.bin",
		Status:     models.URLDownloadStatusPending,
		TargetPath: "/public/.a.bin",
		QueuedAt:   utils.Now().Add(30 * time.Second),
	}
	if err := repo.Create(task); err != nil {
		t.Fatal(err)
	}
	d.armTimer()
	if !d.timer.Stop() {
		t.Fatal("存在未到期任务应 arm 定时器")
	}

	// 3) 额度已满（limit=1）：不 arm
	atomic.StoreInt32(&d.inFlight, 1)
	d.armTimer()
	if d.timer.Stop() {
		t.Fatal("额度已满不应 arm 定时器")
	}
}
