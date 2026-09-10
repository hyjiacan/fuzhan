package models

import (
	"mime/multipart"
	"time"

	"gorm.io/gorm"
)

// UploadStatus 上传会话状态
type UploadStatus string

const (
	UploadStatusPending    UploadStatus = "pending"     // 待处理
	UploadStatusInProgress UploadStatus = "in_progress" // 上传中
	UploadStatusExpired    UploadStatus = "expired"     // 已过期
	UploadStatusCompleted  UploadStatus = "completed"   // 已完成
	UploadStatusFailed     UploadStatus = "failed"      // 失败
	UploadStatusCancelled  UploadStatus = "cancelled"   // 已取消
)

// TargetType 上传目标类型
type TargetType string

const (
	TargetTypeRegular TargetType = "regular" // 普通文件
	TargetTypeTemp    TargetType = "temp"    // 临时文件
	TargetTypePrivate TargetType = "private" // 私有存储
)

// UploadSession 上传会话
type UploadSession struct {
	ID          uint         `gorm:"primaryKey" json:"id"`
	FileName    string       `gorm:"size:255;not null" json:"fileName"`              // 文件名
	FileSize    int64        `gorm:"not null" json:"fileSize"`                       // 文件大小
	ChunkSize   int64        `gorm:"not null" json:"chunkSize"`                      // 分片大小
	TotalChunks int          `gorm:"not null" json:"totalChunks"`                    // 总分片数
	Checksum    string       `gorm:"size:64" json:"checksum"`                        // 文件校验和
	Status      UploadStatus `gorm:"size:20;not null;default:pending" json:"status"` // 会话状态
	TargetType  TargetType   `gorm:"size:20;not null;default:regular" json:"targetType"`
	TargetPath  string       `gorm:"size:512" json:"targetPath"`        // 目标路径
	TargetRoot  string       `gorm:"size:255" json:"targetRoot"`        // 目标根目录
	UserID      string       `gorm:"size:64" json:"userID"`             // 用户ID
	ClientIP    string       `gorm:"size:45" json:"clientIP,omitempty"` // 上传者IP（用于公开文件归属放行）
	ChunkDir    string       `gorm:"size:512" json:"chunkDir"`          // 分片目录
	// Code 访问码（仅临时上传携带，32 位十六进制 128bit 熵，用于命名与分享/下载）
	Code string `gorm:"size:64" json:"code,omitempty"`
	// DeleteOnDownload 下载后自动删除（仅临时上传使用）
	DeleteOnDownload bool            `gorm:"default:false" json:"deleteOnDownload,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	ExpiredAt        time.Time       `gorm:"index" json:"expiredAt"` // 过期时间
	DeletedAt        gorm.DeletedAt  `gorm:"index" json:"deletedAt,omitempty"`
	Chunks           []UploadedChunk `gorm:"foreignKey:SessionID" json:"chunks,omitempty"`
}

// TableName 指定表名
func (UploadSession) TableName() string {
	return "upload_sessions"
}

// UploadedChunk 已上传分片
type UploadedChunk struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	SessionID     uint      `gorm:"not null;uniqueIndex:idx_session_chunk" json:"sessionID"`  // 会话ID
	ChunkIndex    int       `gorm:"not null;uniqueIndex:idx_session_chunk" json:"chunkIndex"` // 分片索引
	ChunkChecksum string    `gorm:"size:64" json:"chunkChecksum"`                             // 分片校验和
	ChunkSize     int64     `gorm:"not null" json:"chunkSize"`                                // 分片大小
	CreatedAt     time.Time `json:"createdAt"`
}

// TableName 指定表名
func (UploadedChunk) TableName() string {
	return "uploaded_chunks"
}

// OperationRecord 操作记录
type OperationRecord struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	FileName string `gorm:"size:255;not null" json:"fileName"` // 文件名
	FileSize int64  `gorm:"not null" json:"fileSize"`          // 文件大小
	FilePath string `gorm:"size:512;not null" json:"path"`     // 相对于根目录的文件路径
	FullPath string `gorm:"size:1024" json:"fullPath"`         // 完整路径 rootName/filePath
	RootName string `gorm:"size:64;not null" json:"rootName"`  // 根目录名称
	FileType string `gorm:"size:50" json:"fileType"`           // 文件类型
	// FileRecordID 关联的公共文件索引记录 ID，用于移动/重命名后身份关联
	FileRecordID uint           `gorm:"index" json:"fileRecordId"`     // 公共文件索引记录 ID（0 表示未关联/历史数据）
	ClientIP     string         `gorm:"size:45;index" json:"clientIP"` // 客户端IP
	UserID       string         `gorm:"size:64;index" json:"userID"`   // 用户ID
	UploadType   TargetType     `gorm:"size:20;not null;default:regular" json:"uploadType"`
	Action       string         `gorm:"size:20;default:upload;index" json:"action"` // 操作类型: upload, download, search
	SearchQuery  string         `gorm:"size:255;index" json:"searchQuery"`          // 搜索关键词
	UploadTime   time.Time      `gorm:"not null;index" json:"uploadTime"`           // 上传时间
	CreatedAt    time.Time      `gorm:"index" json:"createdAt"`                     // 创建时间（用于统计查询）
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"deletedAt,omitempty"`
	// Notes 当前关联文件的备注（查询时回填，不落库）
	Notes string `gorm:"-" json:"notes"`
}

// TableName 指定表名
func (OperationRecord) TableName() string {
	return "operation_records"
}

// UploadRequest 上传请求的数据结构
type UploadRequest struct {
	Dir      string `form:"dir" json:"dir" binding:"required,filepath,max=1024"`
	Filename string `form:"filename" json:"filename" binding:"required,filetype,max=255"`
	RootName string `form:"rootName" json:"rootName" binding:"required,max=255"`
	FileSize int64  `form:"fileSize" json:"fileSize" binding:"min=0"`
}

// CheckUploadStatusRequest 检查上传状态请求的数据结构
type CheckUploadStatusRequest struct {
	Filename string `form:"filename" json:"filename" binding:"required,filetype,max=255"`
	Dir      string `form:"dir" json:"dir" binding:"required,filepath,max=1024"`
	RootName string `form:"rootName" json:"rootName" binding:"required,max=255"`
	FileSize int64  `form:"fileSize" json:"fileSize" binding:"min=0"`
}

// UploadChunkRequest 上传分片请求的数据结构
type UploadChunkRequest struct {
	Filename    string                `form:"filename" json:"filename" binding:"required,filetype,max=255"`
	Chunk       *multipart.FileHeader `form:"chunk" binding:"required"`
	ChunkIndex  int                   `form:"chunkIndex" json:"chunkIndex" binding:"required,min=0"`
	TotalChunks int                   `form:"totalChunks" json:"totalChunks" binding:"required,min=1"`
	Checksum    string                `form:"checksum" json:"checksum" binding:"max=64"`
	Dir         string                `form:"dir" json:"dir" binding:"required,filepath,max=1024"`
	RootName    string                `form:"rootName" json:"rootName" binding:"required,max=255"`
	FileSize    int64                 `form:"fileSize" json:"fileSize" binding:"min=0"`
}

// FinalizeUploadRequest 完成分片上传请求的数据结构
type FinalizeUploadRequest struct {
	Filename    string `form:"filename" json:"filename" binding:"required,filetype,max=255"`
	Dir         string `form:"dir" json:"dir" binding:"required,filepath,max=1024"`
	RootName    string `form:"rootName" json:"rootName" binding:"required,max=255"`
	TotalChunks int    `form:"totalChunks" json:"totalChunks" binding:"required,min=1"`
	FileSize    int64  `form:"fileSize" json:"fileSize" binding:"min=0"`
}

// ExtendTempFileRequest 延长临时文件有效期请求的数据结构
type ExtendTempFileRequest struct {
	ExpireDate  string `json:"expireDate" binding:"max=32"`
	NeverExpire bool   `json:"neverExpire"`
}

// UserInfo 用户信息结构体
type UserInfo struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}
