package cli

import (
	"net/http"

	"fuzhan/internal/response"
)

// EncodeResponse 编码并发送JSON响应（用于标准 http.ResponseWriter）
// 委托给 response 包
func EncodeResponse(w http.ResponseWriter, data interface{}, err string, statusCode int) {
	response.EncodeResponse(w, data, err, statusCode)
}

// AddUploadRecord 添加上传记录
// 委托给 response 包
func AddUploadRecord(ipAddress string, filename string, filePath string) {
	response.AddUploadRecord(ipAddress, filename, filePath)
}

// CheckDirectoryQuota 检查共享目录配额
// 委托给 response 包
func CheckDirectoryQuota(rootPath string, quota int64, additionalSize int64) error {
	return response.CheckDirectoryQuota(rootPath, quota, additionalSize)
}
