package models

// GetRecentUploadsRequest 获取最近上传记录请求参数
type GetRecentUploadsRequest struct {
	Count int `form:"count" json:"count"`
}

// RenameFileRequest 重命名文件请求参数
type RenameFileRequest struct {
	Path    string `json:"path" binding:"required,max=1024"`
	NewName string `json:"new_name" binding:"required,max=255"`
}

// MoveFileRequest 移动文件请求参数
type MoveFileRequest struct {
	Path       string `json:"path" binding:"required,max=1024"`
	TargetPath string `json:"target_path" binding:"required,max=1024"`
}

// DeleteFileRequest 删除文件请求参数
type DeleteFileRequest struct {
	Path string `form:"path" json:"path" binding:"required,max=1024"`
}
