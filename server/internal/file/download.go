package file

import (
	"github.com/gin-gonic/gin"
	"fuzhan/internal/services"
)

// DownloadHandlers 下载相关处理器
type DownloadHandlers struct {
    DownloadService *services.DownloadService
}

// NewDownloadHandlers 创建下载处理器实例
func NewDownloadHandlers(downloadService *services.DownloadService) *DownloadHandlers {
    return &DownloadHandlers{
        DownloadService: downloadService,
    }
}

// DownloadFile 下载文件
func (dh *DownloadHandlers) DownloadFile(c *gin.Context) {
	dh.DownloadService.DownloadFile(c.Writer, c.Request)
}

// AdminDownloadFile 管理页面下载文件（不记录历史）
func (dh *DownloadHandlers) AdminDownloadFile(c *gin.Context) {
	dh.DownloadService.AdminDownloadFile(c.Writer, c.Request)
}