package models

import "time"

// TempFile 临时文件模型
type TempFile struct {
    ID              uint      `gorm:"primaryKey" json:"id"`
    Code            string    `gorm:"uniqueIndex;size:8;not null" json:"code"` // 8位访问码
    Filename        string    `gorm:"size:255;not null" json:"filename"`       // 文件名
    FileSize        int64     `gorm:"not null" json:"fileSize"`               // 文件大小
    FilePath        string    `gorm:"size:512" json:"-"`                      // 存储路径（内部使用）
    ClientIP        string    `gorm:"size:45;index" json:"clientIP"`          // 上传者IP
    Dir             string    `gorm:"size:512;index" json:"dir"`              // 上传时指定的子目录
    DownloadCount   int       `gorm:"default:0" json:"downloadCount"`         // 下载次数
    Downloaded      bool      `gorm:"default:false" json:"downloaded"`        // 是否已下载
    DeleteOnDownload bool     `gorm:"default:false" json:"deleteOnDownload"`  // 下载后自动删除
    ExpiredAt       time.Time `gorm:"index" json:"expiredAt"`                 // 过期时间
    CreatedAt       time.Time `json:"createdAt"`
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
    FileSize      int64     `json:"fileSize"`
    ClientIP      string    `json:"clientIP"`
    DownloadCount int       `json:"downloadCount"`
    ExpiredAt     time.Time `json:"expiredAt"`
    CreatedAt     time.Time `json:"createdAt"`
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