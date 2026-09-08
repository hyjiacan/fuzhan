package models

// ListDirectoriesRequest 列出目录请求参数
type ListDirectoriesRequest struct {
	Path string `form:"path" json:"path" binding:"max=1024"`
}

// GetRecentUploadsRequest 获取最近上传记录请求参数
type GetRecentUploadsRequest struct {
	Count int `form:"count" json:"count"`
}

// RenameFileRequest 重命名文件请求参数
type RenameFileRequest struct {
	OldPath string `json:"oldPath" binding:"required,max=1024"`
	NewName string `json:"newName" binding:"required,max=255"`
}

// MoveFileRequest 移动文件请求参数
type MoveFileRequest struct {
	OldPath string `json:"oldPath" binding:"required,max=1024"`
	NewPath string `json:"newPath" binding:"required,max=1024"`
}

// DeleteFileRequest 删除文件请求参数
type DeleteFileRequest struct {
	Path string `form:"path" json:"path" binding:"required,max=1024"`
}
