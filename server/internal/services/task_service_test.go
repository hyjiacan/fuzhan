package services

import (
	"testing"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func reconcileTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	// 内存库需以单连接使用，避免跨连接查不到表
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.TaskRecord{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

// 启动对账：把遗留的 pending/running 收尾为失败，终态任务不受影响。
func TestTaskService_ReconcileInterruptedTasks(t *testing.T) {
	db := reconcileTestDB(t)
	svc := NewTaskService(db)

	now := utils.Now()
	for _, st := range []models.TaskStatus{
		models.TaskStatusRunning,
		models.TaskStatusPending,
	} {
		if err := db.Create(&models.TaskRecord{TaskType: "hash", TaskName: "x", Status: string(st), StartedAt: &now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 终态任务不应被改动
	for _, st := range []models.TaskStatus{
		models.TaskStatusCompleted,
		models.TaskStatusFailed,
		models.TaskStatusCancelled,
	} {
		if err := db.Create(&models.TaskRecord{TaskType: "scan", TaskName: "y", Status: string(st)}).Error; err != nil {
			t.Fatal(err)
		}
	}

	affected, err := svc.ReconcileInterruptedTasks()
	if err != nil {
		t.Fatalf("对账失败: %v", err)
	}
	if affected != 2 {
		t.Fatalf("期望标记 2 条遗留任务, 实际 %d", affected)
	}

	var running, pending, terminal int64
	db.Model(&models.TaskRecord{}).Where("status = ?", string(models.TaskStatusRunning)).Count(&running)
	db.Model(&models.TaskRecord{}).Where("status = ?", string(models.TaskStatusPending)).Count(&pending)
	db.Model(&models.TaskRecord{}).Where("status = ?", string(models.TaskStatusCompleted)).Count(&terminal)
	if running != 0 || pending != 0 {
		t.Fatalf("对账后不应残留 running/pending, running=%d pending=%d", running, pending)
	}
	if terminal != 1 {
		t.Fatalf("终态 completed 任务不应被改动, 实际 %d", terminal)
	}

	// 中断任务应带错误信息与结束时间
	var failed models.TaskRecord
	if err := db.Where("status = ?", string(models.TaskStatusFailed)).First(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if failed.ErrorMessage == "" || failed.EndedAt == nil {
		t.Fatalf("中断任务应含错误信息与结束时间, err=%q ended=%v", failed.ErrorMessage, failed.EndedAt)
	}
}

// 表为空时对账应为 0，不报错。
func TestTaskService_ReconcileInterruptedTasks_Empty(t *testing.T) {
	db := reconcileTestDB(t)
	affected, err := NewTaskService(db).ReconcileInterruptedTasks()
	if err != nil {
		t.Fatalf("空表对账不应报错: %v", err)
	}
	if affected != 0 {
		t.Fatalf("空表应影响 0 条, 实际 %d", affected)
	}
}
