// Package admin 处理管理员后台业务
package admin

import (
    "net/http"
    "os"
    "strconv"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
    "fuzhan/internal/appconfig"
    "fuzhan/internal/models"
    "fuzhan/pkg/response"
    "fuzhan/internal/repositories"
    "fuzhan/internal/utils"
)

// URLDownloadHandler 管理员 URL 下载任务处理器
type URLDownloadHandler struct {
    db   *gorm.DB
    repo *repositories.URLDownloadTaskRepository
}

// NewURLDownloadHandler 创建 URL 下载任务处理器
func NewURLDownloadHandler(db *gorm.DB) *URLDownloadHandler {
    return &URLDownloadHandler{
        db:   db,
        repo: repositories.NewURLDownloadTaskRepository(db),
    }
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
    if task.TempFilePath != "" {
        if err := os.Remove(task.TempFilePath); err != nil && !os.IsNotExist(err) {
            utils.Warn("删除临时文件失败", utils.String("path", task.TempFilePath), utils.Err(err))
        }
    }
    if task.StorageType == models.URLDownloadStorageTemp && task.TargetPath != "" {
        fullPath := appconfig.GlobalConfig.Storage.Temp.Path + "/" + task.TargetPath
        if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
            utils.Warn("删除目标文件失败", utils.String("path", fullPath), utils.Err(err))
        }
    }
    if err := h.repo.Delete(id); err != nil {
        response.HandleErrorCompat(c, http.StatusInternalServerError, "删除任务失败: "+err.Error(), nil)
        return
    }
    response.HandleSuccess(c, http.StatusOK, "任务已删除", nil)
}
