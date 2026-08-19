package services

import (
	"time"

	"fuzhan/internal/models"

	"gorm.io/gorm"
)

// TaskService 任务管理服务
type TaskService struct {
	db *gorm.DB
}

// NewTaskService 创建任务服务
func NewTaskService(db *gorm.DB) *TaskService {
	return &TaskService{db: db}
}

// CreateTask 创建任务记录
func (s *TaskService) CreateTask(taskType, taskName string) (*models.TaskRecord, error) {
	now := time.Now()
	task := &models.TaskRecord{
		TaskType:  taskType,
		TaskName:  taskName,
		Status:    string(models.TaskStatusPending),
		Progress:  0,
		StartedAt: &now,
	}
	if err := s.db.Create(task).Error; err != nil {
		return nil, err
	}
	return task, nil
}

// UpdateTaskProgress 更新任务进度
func (s *TaskService) UpdateTaskProgress(id uint, progress int, doneItems, totalItems int64) error {
	updates := map[string]interface{}{
		"progress":   progress,
		"done_items": doneItems,
		"total_items": totalItems,
	}
	return s.db.Model(&models.TaskRecord{}).Where("id = ?", id).Updates(updates).Error
}

// CompleteTask 完成任务
func (s *TaskService) CompleteTask(id uint) error {
	now := time.Now()
	return s.db.Model(&models.TaskRecord{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":   string(models.TaskStatusCompleted),
		"progress": 100,
		"ended_at": &now,
	}).Error
}

// FailTask 标记任务失败
func (s *TaskService) FailTask(id uint, errMsg string) error {
	now := time.Now()
	return s.db.Model(&models.TaskRecord{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":        string(models.TaskStatusFailed),
		"error_message": errMsg,
		"ended_at":      &now,
	}).Error
}

// StartTask 标记任务开始运行
func (s *TaskService) StartTask(id uint) error {
	now := time.Now()
	return s.db.Model(&models.TaskRecord{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":     string(models.TaskStatusRunning),
		"started_at": &now,
	}).Error
}

// GetActiveTasks 获取所有活跃任务（运行中或待处理）
func (s *TaskService) GetActiveTasks() ([]models.TaskRecord, error) {
	var tasks []models.TaskRecord
	err := s.db.Where("status IN ?", []string{
		string(models.TaskStatusPending),
		string(models.TaskStatusRunning),
	}).Order("created_at DESC").Find(&tasks).Error
	return tasks, err
}

// GetRecentTasks 获取最近完成的任务历史
func (s *TaskService) GetRecentTasks(limit int) ([]models.TaskRecord, error) {
	var tasks []models.TaskRecord
	err := s.db.Where("status IN ?", []string{
		string(models.TaskStatusCompleted),
		string(models.TaskStatusFailed),
		string(models.TaskStatusCancelled),
	}).Order("ended_at DESC").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// GetTaskByID 获取单个任务详情
func (s *TaskService) GetTaskByID(id uint) (*models.TaskRecord, error) {
	var task models.TaskRecord
	err := s.db.First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// GetTaskHistory 获取任务历史（支持分页和类型过滤）
func (s *TaskService) GetTaskHistory(taskType string, page, pageSize int) ([]models.TaskRecord, int64, error) {
	var tasks []models.TaskRecord
	query := s.db.Model(&models.TaskRecord{})
	if taskType != "" {
		query = query.Where("task_type = ?", taskType)
	}
	var total int64
	query.Count(&total)
	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&tasks).Error
	return tasks, total, err
}

// CancelTask 取消任务
func (s *TaskService) CancelTask(id uint) error {
	now := time.Now()
	return s.db.Model(&models.TaskRecord{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":   string(models.TaskStatusCancelled),
		"ended_at": &now,
	}).Error
}

// CleanOldRecords 清理旧任务记录（保留最近N条）
func (s *TaskService) CleanOldRecords(keep int) error {
	subQuery := s.db.Model(&models.TaskRecord{}).Select("id").Order("created_at DESC").Limit(keep)
	return s.db.Where("id NOT IN (?)", subQuery).Delete(&models.TaskRecord{}).Error
}