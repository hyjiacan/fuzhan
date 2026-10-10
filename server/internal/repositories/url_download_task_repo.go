package repositories

import (
	"errors"
	"time"

	"fuzhan/internal/utils"

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

// UpdateStatus 更新任务状态（无条件）。
// 并发场景（worker 终态写入与用户取消竞争）应改用 UpdateStatusFrom 加状态守卫。
func (r *URLDownloadTaskRepository) UpdateStatus(id string, status models.URLDownloadStatus, errorMsg string) error {
	_, err := r.UpdateStatusFrom(id, nil, status, errorMsg)
	return err
}

// UpdateStatusFrom 条件更新任务状态：仅当任务当前状态在 from 中时才更新为目标状态。
// from 为空表示不加状态条件。返回是否实际更新（false 表示状态已被并发修改，如用户已取消）。
// 用于保证 worker 的终态写入不会覆盖取消等并发状态变更。
func (r *URLDownloadTaskRepository) UpdateStatusFrom(id string, from []models.URLDownloadStatus, status models.URLDownloadStatus, errorMsg string) (bool, error) {
	updates := map[string]interface{}{
		"status": status,
	}
	if errorMsg != "" {
		updates["error_message"] = errorMsg
	}
	if status == models.URLDownloadStatusCompleted || status == models.URLDownloadStatusFailed || status == models.URLDownloadStatusCancelled {
		now := utils.Now()
		updates["completed_at"] = &now
	}
	query := r.db.Model(&models.URLDownloadTask{}).Where("id = ?", id)
	if len(from) > 0 {
		query = query.Where("status IN ?", from)
	}
	res := query.Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// UpdateProgress 更新下载进度
func (r *URLDownloadTaskRepository) UpdateProgress(id string, downloadedBytes int64) error {
	return r.db.Model(&models.URLDownloadTask{}).Where("id = ?", id).
		Update("downloaded_bytes", downloadedBytes).Error
}

// UpdateTargetPath 回写任务最终文件路径。
// 临时文件在完成时会被移动到哈希命名的新位置，需回写真实路径供后续（如校验值计算）使用。
func (r *URLDownloadTaskRepository) UpdateTargetPath(id string, targetPath string) error {
	return r.db.Model(&models.URLDownloadTask{}).Where("id = ?", id).
		Update("target_path", targetPath).Error
}

// ListByStatus 按状态查询任务（分页，管理端用）
func (r *URLDownloadTaskRepository) ListByStatus(status string, page, pageSize int) ([]models.URLDownloadTask, int64, error) {
	var tasks []models.URLDownloadTask
	query := r.db.Model(&models.URLDownloadTask{})
	if status != "" {
		query = query.Where("status = ?", status)
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

// ResetToPending 将失败或已取消的任务重置为等待下载（用户手动重试）。
// 同时清零自动重试计数并刷新入队时间，使手动重试获得全新的重试额度并排到队尾。
func (r *URLDownloadTaskRepository) ResetToPending(id string) error {
	return r.db.Model(&models.URLDownloadTask{}).Where("id = ? AND (status = ? OR status = ?)", id, models.URLDownloadStatusFailed, models.URLDownloadStatusCancelled).
		Updates(map[string]interface{}{
			"status":           models.URLDownloadStatusPending,
			"error_message":    "",
			"downloaded_bytes": 0,
			"retry_count":      0,
			"queued_at":        utils.Now(),
			"completed_at":     nil,
		}).Error
}

// RequeueForRetry 将失败任务重新入队以进行自动重试：重试计数 +1、状态回到 pending、
// 清空错误信息、按退避时间刷新入队时间（排到队尾且延后领取）。保留 downloaded_bytes 与临时文件，以便断点续传。
// 仅当任务仍处于 downloading（即确由 worker 在执行）时才生效，避免复活已被用户取消的任务。
// delay 为最小重试间隔（<=0 表示不延迟）。
func (r *URLDownloadTaskRepository) RequeueForRetry(id string, delay time.Duration) (bool, error) {
	queuedAt := utils.Now()
	if delay > 0 {
		queuedAt = queuedAt.Add(delay)
	}
	res := r.db.Model(&models.URLDownloadTask{}).
		Where("id = ? AND status = ?", id, models.URLDownloadStatusDownloading).
		Updates(map[string]interface{}{
			"status":        models.URLDownloadStatusPending,
			"retry_count":   gorm.Expr("retry_count + 1"),
			"error_message": "",
			"queued_at":     queuedAt,
			"completed_at":  nil,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
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

// ClaimNextPending 原子领取最早的等待中任务：将其置为 downloading 并返回。
// 排序按入队时间（queued_at）升序，使重试任务排到队尾；历史未设置 queued_at 的记录视为最早。
// 仅领取 queued_at 已到期的任务，从而支持重试退避（重试任务在退避期结束前不会被领取）。
// 返回 (nil, nil) 表示当前没有可领取的任务。多个 worker 并发调用时不会重复领取同一任务。
func (r *URLDownloadTaskRepository) ClaimNextPending() (*models.URLDownloadTask, error) {
	var task models.URLDownloadTask
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("status = ? AND queued_at <= ?", models.URLDownloadStatusPending, utils.Now()).
			Order("queued_at ASC, created_at ASC").First(&task).Error; err != nil {
			return err
		}
		res := tx.Model(&models.URLDownloadTask{}).
			Where("id = ? AND status = ?", task.ID, models.URLDownloadStatusPending).
			Update("status", models.URLDownloadStatusDownloading)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		task.Status = models.URLDownloadStatusDownloading
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// ExistsCompletedByURL 判断该 URL 是否已有成功完成的上传记录（全局范围，不限用户）
func (r *URLDownloadTaskRepository) ExistsCompletedByURL(url string) (bool, error) {
	var count int64
	err := r.db.Model(&models.URLDownloadTask{}).
		Where("url = ? AND status = ?", url, models.URLDownloadStatusCompleted).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// RequeueInterrupted 启动对账：把上一进程遗留的 downloading 任务重新入队为 pending，
// 并返回这些任务，供调用方清理其孤儿临时下载文件（TempFilePath）。
// 与 ReconcileInterrupted 不同，这里不判失败，以便重启后由后台队列继续执行。
// 应在进程启动、尚未创建任何新下载任务前调用，避免误判新任务。
func (r *URLDownloadTaskRepository) RequeueInterrupted() ([]models.URLDownloadTask, error) {
	var tasks []models.URLDownloadTask
	if err := r.db.Where("status = ?", models.URLDownloadStatusDownloading).Find(&tasks).Error; err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, nil
	}
	if err := r.db.Model(&models.URLDownloadTask{}).
		Where("status = ?", models.URLDownloadStatusDownloading).
		Updates(map[string]interface{}{
			"status":        models.URLDownloadStatusPending,
			"error_message": "",
		}).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}
