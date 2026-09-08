package index

import (
	"os"
	"sync"
	"time"
)

// ScanScope 扫描范围
type ScanScope string

const (
	// ScanScopePublic 公开文件索引
	ScanScopePublic ScanScope = "public"
	// ScanScopeTemp 临时文件索引
	ScanScopeTemp ScanScope = "temp"
	// ScanScopePrivate 私有文件索引
	ScanScopePrivate ScanScope = "private"
)

// ScanStatus 扫描状态
type ScanStatus string

const (
	// ScanStatusIdle 空闲状态
	ScanStatusIdle ScanStatus = "idle"
	// ScanStatusRunning 扫描进行中
	ScanStatusRunning ScanStatus = "running"
	// ScanStatusCompleted 扫描完成
	ScanStatusCompleted ScanStatus = "completed"
	// ScanStatusFailed 扫描失败
	ScanStatusFailed ScanStatus = "failed"
)

// ScanProgress 全量扫描进度
// 通过 sync.RWMutex 保证并发安全
type ScanProgress struct {
	mu           sync.RWMutex
	Status       ScanStatus `json:"status"`
	TotalFiles   int64      `json:"totalFiles"`
	ScannedFiles int64      `json:"scannedFiles"`
	CurrentFile  string     `json:"currentFile"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	StartedAt    time.Time  `json:"startedAt"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

// ScanProgressView 扫描进度的安全只读视图（不含互斥锁，用于 JSON 序列化）
type ScanProgressView struct {
	Status       ScanStatus `json:"status"`
	TotalFiles   int64      `json:"totalFiles"`
	ScannedFiles int64      `json:"scannedFiles"`
	CurrentFile  string     `json:"currentFile"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	StartedAt    time.Time  `json:"startedAt"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

// Get 获取扫描进度的安全副本
func (p *ScanProgress) Get() ScanProgress {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return ScanProgress{
		Status:       p.Status,
		TotalFiles:   p.TotalFiles,
		ScannedFiles: p.ScannedFiles,
		CurrentFile:  p.CurrentFile,
		ErrorMessage: p.ErrorMessage,
		StartedAt:    p.StartedAt,
		CompletedAt:  p.CompletedAt,
	}
}

// View 返回可用于 JSON 序列化的安全视图
func (p *ScanProgress) View() ScanProgressView {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return ScanProgressView{
		Status:       p.Status,
		TotalFiles:   p.TotalFiles,
		ScannedFiles: p.ScannedFiles,
		CurrentFile:  p.CurrentFile,
		ErrorMessage: p.ErrorMessage,
		StartedAt:    p.StartedAt,
		CompletedAt:  p.CompletedAt,
	}
}

// SetStatus 设置扫描状态
func (p *ScanProgress) SetStatus(s ScanStatus) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Status = s
	if s == ScanStatusRunning && p.StartedAt.IsZero() {
		p.StartedAt = time.Now()
	}
	if s == ScanStatusCompleted || s == ScanStatusFailed {
		now := time.Now()
		p.CompletedAt = &now
	}
}

// CompareAndSetStatus 原子地比较并设置状态
// 只有当当前状态等于 old 时才设置为 new, 返回 true
func (p *ScanProgress) CompareAndSetStatus(old, new ScanStatus) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Status != old {
		return false
	}
	p.Status = new
	if new == ScanStatusRunning && p.StartedAt.IsZero() {
		p.StartedAt = time.Now()
	}
	if new == ScanStatusCompleted || new == ScanStatusFailed {
		now := time.Now()
		p.CompletedAt = &now
	}
	return true
}

// IncScanned 增加已扫描文件计数
func (p *ScanProgress) IncScanned() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ScannedFiles++
}

// SetScanned 设置已扫描文件计数
func (p *ScanProgress) SetScanned(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ScannedFiles = n
}

// SetTotalFiles 设置总文件数
func (p *ScanProgress) SetTotalFiles(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.TotalFiles = n
}

// SetCurrentFile 设置当前正在扫描的文件名
func (p *ScanProgress) SetCurrentFile(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.CurrentFile = name
}

// Reset 重置扫描进度
func (p *ScanProgress) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Status = ScanStatusIdle
	p.TotalFiles = 0
	p.ScannedFiles = 0
	p.CurrentFile = ""
	p.ErrorMessage = ""
	p.StartedAt = time.Time{}
	p.CompletedAt = nil
}

// SetError 设置扫描错误
func (p *ScanProgress) SetError(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Status = ScanStatusFailed
	p.ErrorMessage = msg
	now := time.Now()
	p.CompletedAt = &now
}

// ConsistencyReport 一致性校验报告
type ConsistencyReport struct {
	CheckedAt   time.Time         `json:"checkedAt"`
	Duration    time.Duration     `json:"duration"`
	TotalFS     int64             `json:"totalFS"`
	TotalDB     int64             `json:"totalDB"`
	MissingInDB []ConsistencyDiff `json:"missingInDB"`
	MissingInFS []ConsistencyDiff `json:"missingInFS"`
	Mismatched  []ConsistencyDiff `json:"mismatched"`
	AutoFixed   int               `json:"autoFixed"`
	FixErrors   []ConsistencyDiff `json:"fixErrors,omitempty"`
}

// ConsistencyDiff 单一不一致条目
type ConsistencyDiff struct {
	RootName    string `json:"rootName"`
	FilePath    string `json:"filePath"`
	FullPath    string `json:"fullPath"`
	FileName    string `json:"fileName"`
	Description string `json:"description"`
}

// ListRecordsQuery 文件记录列表查询参数
type ListRecordsQuery struct {
	Page     int    `form:"page"`
	PageSize int    `form:"pageSize"`
	Query    string `form:"query"`    // 文件名模糊搜索
	RootName string `form:"rootName"` // 根目录过滤
	OwnerID  string `form:"ownerID"`  // 所有者过滤（temp 用 IP，private 用 用户ID）
	Status   string `form:"status"`   // active / deleted / all
	Sort     string `form:"sort"`     // name / size / time
	Order    string `form:"order"`    // asc / desc
	Table    string `form:"table"`    // 索引表: file_records(默认) / file_records_public / file_records_temp / file_records_private
}

// DefaultPage 默认页码
const DefaultPage = 1

// DefaultPageSize 默认每页数量
const DefaultPageSize = 50

// MaxPageSize 最大每页数量
const MaxPageSize = 200

// Normalize 规范化查询参数, 设置默认值
func (q *ListRecordsQuery) Normalize() {
	if q.Page <= 0 {
		q.Page = DefaultPage
	}
	if q.PageSize <= 0 || q.PageSize > MaxPageSize {
		q.PageSize = DefaultPageSize
	}
	if q.Status == "" {
		q.Status = "active"
	}
	if q.Sort == "" {
		q.Sort = "name"
	}
	if q.Order == "" {
		q.Order = "asc"
	}
}

// DuplicateGroup 重复文件分组
type DuplicateGroup struct {
	Xxh3Hash  string              `json:"xxh3Hash"`
	FileCount int64               `json:"fileCount"`
	TotalSize int64               `json:"totalSize"`
	Files     []DuplicateFileItem `json:"files,omitempty"`
}

// DuplicateFileItem 重复文件中的单个文件
type DuplicateFileItem struct {
	ID       uint      `json:"id"`
	FileName string    `json:"fileName"`
	FilePath string    `json:"filePath"`
	FullPath string    `json:"fullPath"`
	RootName string    `json:"rootName"`
	FileSize int64     `json:"fileSize"`
	ModTime  time.Time `json:"modTime"`
}

// DuplicateQuery 重复文件查询参数
type DuplicateQuery struct {
	MinSize  int64  `form:"minSize"`
	Page     int    `form:"page"`
	PageSize int    `form:"pageSize"`
	Sort     string `form:"sort"` // size / count / hash
	Order    string `form:"order"`
}

// Normalize 规范化查询参数
func (q *DuplicateQuery) Normalize() {
	if q.Page <= 0 {
		q.Page = DefaultPage
	}
	if q.PageSize <= 0 || q.PageSize > MaxPageSize {
		q.PageSize = DefaultPageSize
	}
	if q.Sort == "" {
		q.Sort = "size"
	}
	if q.Order == "" {
		q.Order = "desc"
	}
}

// DependencyTreeNode 依赖树节点
type DependencyTreeNode struct {
	ID           uint                  `json:"id"`
	DependencyID uint                  `json:"dependencyId"` // 依赖关系 ID（用于删除）
	FileName     string                `json:"fileName"`
	FilePath     string                `json:"filePath"`
	FullPath     string                `json:"fullPath"`
	RootName     string                `json:"rootName"`
	FileSize     int64                 `json:"fileSize"`
	IsDir        bool                  `json:"isDir"`
	Relation     string                `json:"relation"`
	DownloadURL  string                `json:"downloadURL"`
	Children     []*DependencyTreeNode `json:"children"`
	Downstream   []*DependencyTreeNode `json:"downstream"`
}

// UpdateNotesRequest 更新文件备注请求
// ID 从 URL 路径参数获取，不需要在 JSON body 中提供
type UpdateNotesRequest struct {
	ID    uint   `json:"id"`
	Notes string `json:"notes"`
}

// fileSizeFromInfo 从 os.FileInfo 获取文件大小（目录返回 0）
func fileSizeFromInfo(info os.FileInfo) int64 {
	if info.IsDir() {
		return 0
	}
	return info.Size()
}

// RecordStatsResponse 文件统计响应
type RecordStatsResponse struct {
	TotalFiles      int64 `json:"totalFiles"`
	TotalSize       int64 `json:"totalSize"`
	TotalDirs       int64 `json:"totalDirs"`
	DeletedFiles    int64 `json:"deletedFiles"`
	ActiveFiles     int64 `json:"activeFiles"`
	DuplicateGroups int64 `json:"duplicateGroups"` // 有重复的哈希组数
	PendingSync     int64 `json:"pendingSync"`     // 待同步数 (LastSyncedAt 为空或较旧)
	TotalRootDirs   int   `json:"totalRootDirs"`   // 根目录数
}

// ScanStatusResponse 扫描状态响应（供 Footer 状态栏使用）
type ScanStatusResponse struct {
	LastScanTime       *time.Time `json:"lastScanTime"`       // 上次扫描完成时间
	NextScanTime       *time.Time `json:"nextScanTime"`       // 下次计划扫描时间
	IsScanning         bool       `json:"isScanning"`         // 是否正在扫描
	ScanScope          string     `json:"scanScope"`          // 当前扫描范围（public/temp/private）
	ScanProgress       int64      `json:"scanProgress"`       // 当前进度（已处理文件数）
	ScanTotal          int64      `json:"scanTotal"`          // 总文件数
	ScanCronExpression string     `json:"scanCronExpression"` // 扫描 cron 表达式
}
