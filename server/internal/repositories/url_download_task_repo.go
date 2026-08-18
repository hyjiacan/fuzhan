package repositories

import (
    "time"

    "fuzhan/internal/models"

    "gorm.io/gorm"
)

// URLDownloadTaskRepository URL 下载任务仓储
type URLDownloadTaskRepository struct {
    db *gorm.DB
}

// NewURLDownloadTaskRepository 创建 URL 下载任务仓储
func NewURLDownloadTaskRepository(db *gorm.DB) *URLDownloadTaskRepository {
    return &URLDownloadTaskRepository{db: db}
}

// Create 创建任务
func (r *URLDownloadTaskRepository) Create(task *models.URLDownloadTask) error {
    return r.db.Create(task).Error
}

// GetByID 根据 ID 获取任务
func (r *URLDownloadTaskRepository) GetByID(id string) (*models.URLDownloadTask, error) {
    var task models.URLDownloadTask
    err := r.db.First(&task, "id = ?", id).Error
    if err != nil {
        return nil, err
    }
    return &task, nil
}

// UpdateStatus 更新任务状态
func (r *URLDownloadTaskRepository) UpdateStatus(id string, status models.URLDownloadStatus, errorMsg string) error {
    updates := map[string]interface{}{
        "status": status,
    }
    if errorMsg != "" {
        updates["error_message"] = errorMsg
    }
    if status == models.URLDownloadStatusCompleted || status == models.URLDownloadStatusFailed || status == models.URLDownloadStatusCancelled {
        now := time.Now()
        updates["completed_at"] = &now
    }
    return r.db.Model(&models.URLDownloadTask{}).Where("id = ?", id).Updates(updates).Error
}

// UpdateProgress 更新下载进度
func (r *URLDownloadTaskRepository) UpdateProgress(id string, downloadedBytes int64) error {
    return r.db.Model(&models.URLDownloadTask{}).Where("id = ?", id).
        Update("downloaded_bytes", downloadedBytes).Error
}

// ListByStatus 按状态查询任务（分页，管理端用）
func (r *URLDownloadTaskRepository) ListByStatus(status string, page, pageSize int) ([]models.URLDownloadTask, int64, error) {
    var tasks []models.URLDownloadTask
    query := r.db.Model(&models.URLDownloadTask{})
    if status != "" {
        query = query.Where("`status` = ?", status)
    }
    var total int64
    if err := query.Count(&total).Error; err != nil {
        return nil, 0, err
    }
    err := query.Order("created_at DESC").
        Offset((page - 1) * pageSize).
        Limit(pageSize).
        Find(&tasks).Error
    return tasks, total, err
}

// ListAll 获取所有任务（管理端用，分页）
func (r *URLDownloadTaskRepository) ListAll(page, pageSize int) ([]models.URLDownloadTask, int64, error) {
    return r.ListByStatus("", page, pageSize)
}

// ListByUser 根据用户标识查询任务（分页）
// userID 优先（登录用户），anonymousID 次之（匿名用户）
// storageType 过滤：空字符串表示不过滤
func (r *URLDownloadTaskRepository) ListByUser(userID *string, anonymousID *string, storageType string, page, pageSize int) ([]models.URLDownloadTask, int64, error) {
    var tasks []models.URLDownloadTask
    query := r.db.Model(&models.URLDownloadTask{})

    if userID != nil && *userID != "" {
        query = query.Where("user_id = ?", *userID)
    } else if anonymousID != nil && *anonymousID != "" {
        query = query.Where("anonymous_id = ? AND user_id IS NULL", *anonymousID)
    } else {
        return tasks, 0, nil
    }

    if storageType != "" {
        query = query.Where("storage_type = ?", storageType)
    }

    var total int64
    if err := query.Count(&total).Error; err != nil {
        return nil, 0, err
    }

    err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&tasks).Error
    return tasks, total, err
}

// Delete 删除任务
func (r *URLDownloadTaskRepository) Delete(id string) error {
    return r.db.Delete(&models.URLDownloadTask{}, "id = ?", id).Error
}

// ResetToPending 将失败或已取消的任务重置为等待下载
func (r *URLDownloadTaskRepository) ResetToPending(id string) error {
    return r.db.Model(&models.URLDownloadTask{}).Where("id = ? AND (`status` = ? OR `status` = ?)", id, models.URLDownloadStatusFailed, models.URLDownloadStatusCancelled).
        Updates(map[string]interface{}{
            "status":          models.URLDownloadStatusPending,
            "error_message":   "",
            "downloaded_bytes": 0,
        }).Error
}

// ListNotNotified 获取未通知的任务列表
func (r *URLDownloadTaskRepository) ListNotNotified(userID *string, anonymousID *string) ([]models.URLDownloadTask, error) {
    var tasks []models.URLDownloadTask
    query := r.db.Where("notified = ?", false)

    if userID != nil && *userID != "" {
        query = query.Where("user_id = ?", *userID)
    } else if anonymousID != nil && *anonymousID != "" {
        query = query.Where("anonymous_id = ? AND user_id IS NULL", *anonymousID)
    } else {
        return tasks, nil
    }

    err := query.Order("created_at DESC").Find(&tasks).Error
    return tasks, err
}

// MarkNotified 标记任务为已通知（按 owner 过滤）
func (r *URLDownloadTaskRepository) MarkNotified(ids []string, userID *string, anonymousID *string) error {
    query := r.db.Model(&models.URLDownloadTask{}).Where("id IN ?", ids)
    if userID != nil && *userID != "" {
        query = query.Where("user_id = ?", *userID)
    } else if anonymousID != nil && *anonymousID != "" {
        query = query.Where("anonymous_id = ? AND user_id IS NULL", *anonymousID)
    }
    return query.Update("notified", true).Error
}

// MergeAnonymousToUser 将匿名任务合并到用户
func (r *URLDownloadTaskRepository) MergeAnonymousToUser(anonymousID string, userID string) error {
    return r.db.Model(&models.URLDownloadTask{}).
        Where("anonymous_id = ? AND user_id IS NULL", anonymousID).
        Update("user_id", userID).Error
}
