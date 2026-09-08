package models

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// setupURLTaskTestDB 创建内存 SQLite 数据库并自动迁移
func setupURLTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&URLDownloadTask{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	return db
}

// TestURLDownloadTask_TableName 验证表名
func TestURLDownloadTask_TableName(t *testing.T) {
	task := URLDownloadTask{}
	if got := task.TableName(); got != "url_download_tasks" {
		t.Errorf("TableName() = %q，期望 %q", got, "url_download_tasks")
	}
}

// TestURLDownloadTask_Create 创建有效任务
func TestURLDownloadTask_Create(t *testing.T) {
	db := setupURLTaskTestDB(t)
	id := uuid.New().String()
	userID := "test-user-uuid"

	task := &URLDownloadTask{
		ID:          id,
		UserID:      &userID,
		URL:         "https://example.com/file.zip",
		FileName:    "file.zip",
		TargetPath:  "/data/temp/file.zip",
		StorageType: URLDownloadStorageTemp,
		Status:      URLDownloadStatusPending,
	}

	if err := db.Create(task).Error; err != nil {
		t.Fatalf("创建任务失败: %v", err)
	}

	var saved URLDownloadTask
	if err := db.First(&saved, "id = ?", id).Error; err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}

	if saved.FileName != "file.zip" {
		t.Errorf("FileName = %q，期望 %q", saved.FileName, "file.zip")
	}
	if saved.StorageType != URLDownloadStorageTemp {
		t.Errorf("StorageType = %q，期望 %q", saved.StorageType, URLDownloadStorageTemp)
	}
	if saved.Status != URLDownloadStatusPending {
		t.Errorf("Status = %q，期望 %q", saved.Status, URLDownloadStatusPending)
	}
	if saved.Notified != false {
		t.Errorf("Notified = %v，期望 false", saved.Notified)
	}
}

// TestURLDownloadTask_AllStorageTypes 验证所有存储类型
func TestURLDownloadTask_AllStorageTypes(t *testing.T) {
	db := setupURLTaskTestDB(t)
	storageTypes := []URLDownloadStorageType{
		URLDownloadStorageTemp,
		URLDownloadStoragePrivate,
		URLDownloadStorageRegular,
	}

	for _, st := range storageTypes {
		t.Run(string(st), func(t *testing.T) {
			task := &URLDownloadTask{
				ID:          uuid.New().String(),
				URL:         "https://example.com/file",
				FileName:    "file",
				StorageType: st,
				Status:      URLDownloadStatusPending,
			}
			if err := db.Create(task).Error; err != nil {
				t.Fatalf("创建存储类型 %q 的任务失败: %v", st, err)
			}
		})
	}
}

// TestURLDownloadTask_AllStatuses 验证所有状态
func TestURLDownloadTask_AllStatuses(t *testing.T) {
	db := setupURLTaskTestDB(t)
	statuses := []URLDownloadStatus{
		URLDownloadStatusPending,
		URLDownloadStatusDownloading,
		URLDownloadStatusCompleted,
		URLDownloadStatusFailed,
	}

	for _, st := range statuses {
		t.Run(string(st), func(t *testing.T) {
			task := &URLDownloadTask{
				ID:          uuid.New().String(),
				URL:         "https://example.com/file",
				FileName:    "file",
				StorageType: URLDownloadStorageTemp,
				Status:      st,
			}
			if err := db.Create(task).Error; err != nil {
				t.Fatalf("创建状态 %q 的任务失败: %v", st, err)
			}
		})
	}
}

// TestURLDownloadTask_AnonymousUser 匿名用户任务：UserID 为空
func TestURLDownloadTask_AnonymousUser(t *testing.T) {
	db := setupURLTaskTestDB(t)
	anonymousID := uuid.New().String()

	task := &URLDownloadTask{
		ID:          uuid.New().String(),
		AnonymousID: &anonymousID,
		URL:         "https://example.com/file",
		FileName:    "file",
		StorageType: URLDownloadStorageTemp,
		Status:      URLDownloadStatusPending,
	}

	if err := db.Create(task).Error; err != nil {
		t.Fatalf("创建匿名用户任务失败: %v", err)
	}

	var saved URLDownloadTask
	if err := db.First(&saved, "id = ?", task.ID).Error; err != nil {
		t.Fatalf("查询匿名用户任务失败: %v", err)
	}

	if saved.UserID != nil {
		t.Error("匿名用户 UserID 应为 nil")
	}
	if saved.AnonymousID == nil || *saved.AnonymousID != anonymousID {
		t.Errorf("AnonymousID = %v，期望 %q", saved.AnonymousID, anonymousID)
	}
}

// TestURLDownloadTask_AnonymousUserOnly 匿名用户通知查询条件：anonymous_id AND user_id IS NULL
func TestURLDownloadTask_AnonymousUserOnly(t *testing.T) {
	db := setupURLTaskTestDB(t)
	anonymousID := uuid.New().String()
	userIDStr := "test-user-uuid"

	// 创建匿名任务
	anonTask := &URLDownloadTask{
		ID:          uuid.New().String(),
		AnonymousID: &anonymousID,
		URL:         "https://example.com/anon",
		FileName:    "anon",
		StorageType: URLDownloadStorageTemp,
		Status:      URLDownloadStatusCompleted,
		Notified:    false,
	}
	if err := db.Create(anonTask).Error; err != nil {
		t.Fatalf("创建匿名任务失败: %v", err)
	}

	// 创建同一 anonymousID 但已登录的任务
	loggedInTask := &URLDownloadTask{
		ID:          uuid.New().String(),
		UserID:      &userIDStr,
		AnonymousID: &anonymousID,
		URL:         "https://example.com/user",
		FileName:    "user",
		StorageType: URLDownloadStorageTemp,
		Status:      URLDownloadStatusCompleted,
		Notified:    false,
	}
	if err := db.Create(loggedInTask).Error; err != nil {
		t.Fatalf("创建已登录用户任务失败: %v", err)
	}

	// 匿名查询：只能查到 user_id IS NULL 的任务
	var anonResults []URLDownloadTask
	db.Where("anonymous_id = ? AND user_id IS NULL AND notified = ?", anonymousID, false).Find(&anonResults)

	if len(anonResults) != 1 {
		t.Fatalf("匿名查询期望 1 条记录，实际 %d 条", len(anonResults))
	}
	if anonResults[0].FileName != "anon" {
		t.Errorf("匿名查询结果文件名为 %q，期望 %q", anonResults[0].FileName, "anon")
	}

	// 已登录查询：通过 user_id
	var userResults []URLDownloadTask
	db.Where("user_id = ? AND notified = ?", userIDStr, false).Find(&userResults)
	if len(userResults) != 1 {
		t.Fatalf("用户查询期望 1 条记录，实际 %d 条", len(userResults))
	}
}

// TestURLDownloadTask_DownloadProgress 下载进度更新
func TestURLDownloadTask_DownloadProgress(t *testing.T) {
	db := setupURLTaskTestDB(t)
	id := uuid.New().String()

	task := &URLDownloadTask{
		ID:          id,
		URL:         "https://example.com/large.zip",
		FileName:    "large.zip",
		StorageType: URLDownloadStorageTemp,
		Status:      URLDownloadStatusDownloading,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("创建任务失败: %v", err)
	}

	// 更新下载进度
	downloaded := int64(4 * 1024 * 1024) // 4MB
	if err := db.Model(&URLDownloadTask{}).Where("id = ?", id).Update("downloaded_bytes", downloaded).Error; err != nil {
		t.Fatalf("更新下载进度失败: %v", err)
	}

	var saved URLDownloadTask
	if err := db.First(&saved, "id = ?", id).Error; err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if saved.DownloadedBytes != downloaded {
		t.Errorf("DownloadedBytes = %d，期望 %d", saved.DownloadedBytes, downloaded)
	}
}

// TestURLDownloadTask_CompleteTask 任务完成状态更新
func TestURLDownloadTask_CompleteTask(t *testing.T) {
	db := setupURLTaskTestDB(t)
	id := uuid.New().String()
	now := time.Now()

	task := &URLDownloadTask{
		ID:          id,
		URL:         "https://example.com/file.zip",
		FileName:    "file.zip",
		StorageType: URLDownloadStorageTemp,
		Status:      URLDownloadStatusDownloading,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("创建任务失败: %v", err)
	}

	// 标记完成
	updates := map[string]interface{}{
		"status":           URLDownloadStatusCompleted,
		"downloaded_bytes": 1024,
		"file_size":        1024,
		"completed_at":     now,
	}
	if err := db.Model(&URLDownloadTask{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		t.Fatalf("更新任务完成状态失败: %v", err)
	}

	var saved URLDownloadTask
	if err := db.First(&saved, "id = ?", id).Error; err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if saved.Status != URLDownloadStatusCompleted {
		t.Errorf("Status = %q，期望 %q", saved.Status, URLDownloadStatusCompleted)
	}
	if saved.CompletedAt == nil {
		t.Error("CompletedAt 不应为 nil")
	}
}

// TestURLDownloadTask_FailedTask 失败任务
func TestURLDownloadTask_FailedTask(t *testing.T) {
	db := setupURLTaskTestDB(t)
	id := uuid.New().String()
	errMsg := "404 Not Found: 资源不存在"

	task := &URLDownloadTask{
		ID:          id,
		URL:         "https://example.com/not-exist",
		FileName:    "not-exist",
		StorageType: URLDownloadStorageTemp,
		Status:      URLDownloadStatusDownloading,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("创建任务失败: %v", err)
	}

	// 标记失败
	if err := db.Model(&URLDownloadTask{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":        URLDownloadStatusFailed,
		"error_message": errMsg,
	}).Error; err != nil {
		t.Fatalf("更新任务失败状态失败: %v", err)
	}

	var saved URLDownloadTask
	if err := db.First(&saved, "id = ?", id).Error; err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if saved.Status != URLDownloadStatusFailed {
		t.Errorf("Status = %q，期望 %q", saved.Status, URLDownloadStatusFailed)
	}
	if saved.ErrorMessage != errMsg {
		t.Errorf("ErrorMessage = %q，期望 %q", saved.ErrorMessage, errMsg)
	}
}

// TestURLDownloadTask_ETag 验证 ETag 字段
func TestURLDownloadTask_ETag(t *testing.T) {
	db := setupURLTaskTestDB(t)
	etag := `"abc123def"`

	task := &URLDownloadTask{
		ID:              uuid.New().String(),
		URL:             "https://example.com/file",
		FileName:        "file",
		StorageType:     URLDownloadStorageTemp,
		Status:          URLDownloadStatusPending,
		ETag:            etag,
		ResumeSupported: true,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("创建任务失败: %v", err)
	}

	var saved URLDownloadTask
	if err := db.First(&saved, "id = ?", task.ID).Error; err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if saved.ETag != etag {
		t.Errorf("ETag = %q，期望 %q", saved.ETag, etag)
	}
	if saved.ResumeSupported != true {
		t.Error("ResumeSupported 应为 true")
	}
}

// TestURLDownloadTask_NotifiedFlag 通知标记
func TestURLDownloadTask_NotifiedFlag(t *testing.T) {
	db := setupURLTaskTestDB(t)
	id := uuid.New().String()

	task := &URLDownloadTask{
		ID:          id,
		URL:         "https://example.com/file",
		FileName:    "file",
		StorageType: URLDownloadStorageTemp,
		Status:      URLDownloadStatusCompleted,
		Notified:    false,
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("创建任务失败: %v", err)
	}

	// 标记为已通知
	if err := db.Model(&URLDownloadTask{}).Where("id = ?", id).Update("notified", true).Error; err != nil {
		t.Fatalf("更新通知标记失败: %v", err)
	}

	var saved URLDownloadTask
	if err := db.First(&saved, "id = ?", id).Error; err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if saved.Notified != true {
		t.Error("Notified 应为 true")
	}

	// 验证该任务不在未通知列表中
	var count int64
	if err := db.Model(&URLDownloadTask{}).Where("id = ? AND notified = ?", id, false).Count(&count).Error; err != nil {
		t.Fatalf("查询未通知任务失败: %v", err)
	}
	if count != 0 {
		t.Errorf("已标记的任务仍在未通知列表中")
	}
}
