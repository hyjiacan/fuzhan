package config

import (
	"net/http"

	appconfig "fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// BindingExampleHandler 展示如何使用 Gin 的数据绑定功能
type BindingExampleHandler struct{}

// NewBindingExampleHandler 创建绑定示例处理器实例
func NewBindingExampleHandler() *BindingExampleHandler {
	return &BindingExampleHandler{}
}

// UploadWithBinding 使用数据绑定处理上传请求
func (beh *BindingExampleHandler) UploadWithBinding(c *gin.Context) {
	var req models.UploadRequest
	if err := c.ShouldBind(&req); err != nil {
		if validationErrors, ok := err.(appconfig.ValidationErrors); ok {
			response.HandleBadRequest(c, "请求数据验证失败", validationErrors)
			return
		}
		response.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
		return
	}
	response.HandleSuccess(c, http.StatusOK, "上传请求处理成功", map[string]interface{}{
		"dir":      req.Dir,
		"filename": req.Filename,
		"rootName": req.RootName,
		"fileSize": req.FileSize,
	})
}

// RenameFileWithBinding 使用数据绑定处理重命名文件请求
func (beh *BindingExampleHandler) RenameFileWithBinding(c *gin.Context) {
	var req models.RenameFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if validationErrors, ok := err.(appconfig.ValidationErrors); ok {
			response.HandleBadRequest(c, "请求数据验证失败", validationErrors)
			return
		}
		response.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
		return
	}
	response.HandleSuccess(c, http.StatusOK, "重命名请求处理成功", map[string]interface{}{
		"oldPath": req.OldPath,
		"newName": req.NewName,
	})
}
