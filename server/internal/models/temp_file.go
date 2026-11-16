package models

import "time"

// TempFile 临时文件模型
type TempFile struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	Code             string    `gorm:"uniqueIndex;size:8;not null" json:"code"` // 8位访问码
	Filename         string    `gorm:"size:255;not null" json:"filename"`       // 文件名
	FileSize         int64     `gorm:"not null" json:"file_size"`               // 文件大小
	FilePath         string    `gorm:"size:512" json:"-"`                       // 存储路径（内部使用）
	ClientIP         string    `gorm:"size:45;index" json:"client_ip"`          // 上传者IP
	UserID           string    `gorm:"size:64;index" json:"user_id,omitempty"`  // 上传者用户UUID，匿名上传为空，用于注销时清理
	Dir              string    `gorm:"size:512;index" json:"dir"`               // 上传时指定的子目录
	DownloadCount    int       `gorm:"default:0" json:"download_count"`         // 下载次数
	Downloaded       bool      `gorm:"default:false" json:"downloaded"`         // 是否已下载
	DeleteOnDownload bool      `gorm:"default:false" json:"delete_on_download"` // 下载后自动删除
	ExpiredAt        time.Time `gorm:"index" json:"expired_at"`                 // 过期时间
	CreatedAt        time.Time `json:"created_at"`
}

// TableName 指定表名
func (TempFile) TableName() string {
	return "temp_files"
}

// TempFileResponse 临时文件API响应
type TempFileResponse struct {
	ID            uint      `json:"id"`
	Code          string    `json:"code"`
	Filename      string    `json:"filename"`
	FileSize      int64     `json:"file_size"`
	ClientIP      string    `json:"client_ip"`
	DownloadCount int       `json:"download_count"`
	ExpiredAt     time.Time `json:"expired_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// ToResponse 转换为API响应
func (t *TempFile) ToResponse() *TempFileResponse {
	return &TempFileResponse{
		ID:            t.ID,
		Code:          t.Code,
		Filename:      t.Filename,
		FileSize:      t.FileSize,
		ClientIP:      t.ClientIP,
		DownloadCount: t.DownloadCount,
		ExpiredAt:     t.ExpiredAt,
		CreatedAt:     t.CreatedAt,
	}
}

// TempQuotaInfo 配额信息
type TempQuotaInfo struct {
	Used  int64 `json:"used"`  // 已使用空间
	Limit int64 `json:"limit"` // 配额限制
}

// TempListResponse 临时文件列表响应
type TempListResponse struct {
	Files   []TempFileResponse `json:"files"`
	Subdirs []string           `json:"subdirs"`
	Quota   TempQuotaInfo      `json:"quota"`
}
