// Package admin 处理管理员后台业务
package admin

import (
	"net/http"
	"os"
	"strconv"

	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// URLDownloadHandler 管理员 URL 下载任务处理器
type URLDownloadHandler struct {
	db   *gorm.DB
	repo *repositories.URLDownloadTaskRepository
	// notify 用于任务重置后唤醒后台调度器；由装配方注入，避免与 file 包形成依赖环。
	notify func()
	// cancel 用于中断执行中的任务；由装配方注入。
	cancel func(taskID string) bool
}

// NewURLDownloadHandler 创建 URL 下载任务处理器
func NewURLDownloadHandler(db *gorm.DB) *URLDownloadHandler {
	return &URLDownloadHandler{
		db:   db,
		repo: repositories.NewURLDownloadTaskRepository(db),
	}
}

// SetTaskHooks 注入调度器钩子：notify 唤醒队列、cancel 中断执行中的任务。
func (h *URLDownloadHandler) SetTaskHooks(notify func(), cancel func(taskID string) bool) {
	h.notify = notify
	h.cancel = cancel
}

// ListURLTasks 获取 URL 下载任务列表
func (h *URLDownloadHandler) ListURLTasks(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	status := c.Query("status")
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	var tasks []models.URLDownloadTask
	var total int64
	var err error
	if status != "" {
		tasks, total, err = h.repo.ListByStatus(status, page, pageSize)
	} else {
		tasks, total, err = h.repo.ListAll(page, pageSize)
	}
	if err != nil {
		response.HandleErrorCompat(c, http.StatusInternalServerError, "查询任务列表失败: "+err.Error(), nil)
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"items": tasks, "total": total, "page": page, "pageSize": pageSize,
	})
}

// RetryURLTask 重试失败的 URL 下载任务
func (h *URLDownloadHandler) RetryURLTask(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		response.HandleBadRequest(c, "任务ID不能为空", nil)
		return
	}
	task, err := h.repo.GetByID(id)
	if err != nil {
		response.HandleErrorCompat(c, http.StatusNotFound, "任务不存在", nil)
		return
	}
	if task.Status != models.URLDownloadStatusFailed {
		response.HandleBadRequest(c, "只有失败状态的任务可以重试", nil)
		return
	}
	if task.TempFilePath != "" {
		if err := os.Remove(task.TempFilePath); err != nil && !os.IsNotExist(err) {
			utils.Warn("清理临时文件失败", utils.String("path", task.TempFilePath), utils.Err(err))
		}
	}
	if err := h.repo.ResetToPending(id); err != nil {
		response.HandleErrorCompat(c, http.StatusInternalServerError, "重置任务失败: "+err.Error(), nil)
		return
	}
	// 唤醒后台调度器，避免依赖兜底定时器导致最多 1s 延迟
	if h.notify != nil {
		h.notify()
	}
	response.HandleSuccess(c, http.StatusOK, "任务已重置，等待重新下载", nil)
}

// DeleteURLTask 删除 URL 下载任务
func (h *URLDownloadHandler) DeleteURLTask(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		response.HandleBadRequest(c, "任务ID不能为空", nil)
		return
	}
	task, err := h.repo.GetByID(id)
	if err != nil {
		response.HandleErrorCompat(c, http.StatusNotFound, "任务不存在", nil)
		return
	}
	// 先中断执行中的任务，避免删除记录后 worker 继续写入产生孤儿文件
	if h.cancel != nil {
		h.cancel(id)
	}
	if task.TempFilePath != "" {
		if err := os.Remove(task.TempFilePath); err != nil && !os.IsNotExist(err) {
			utils.Warn("删除临时文件失败", utils.String("path", task.TempFilePath), utils.Err(err))
		}
	}
	// TargetPath 已是绝对路径，直接删除；此前二次拼接临时目录前缀导致路径错误
	if task.StorageType == models.URLDownloadStorageTemp && task.TargetPath != "" {
		if err := os.Remove(task.TargetPath); err != nil && !os.IsNotExist(err) {
			utils.Warn("删除目标文件失败", utils.String("path", task.TargetPath), utils.Err(err))
		}
	}
	if err := h.repo.Delete(id); err != nil {
		response.HandleErrorCompat(c, http.StatusInternalServerError, "删除任务失败: "+err.Error(), nil)
		return
	}
	response.HandleSuccess(c, http.StatusOK, "任务已删除", nil)
}
