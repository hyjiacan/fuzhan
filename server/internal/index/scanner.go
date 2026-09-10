package index

import (
	"context"
	"sync"

	"fuzhan/internal/models"
	"gorm.io/gorm"
)

// Scanner 全量扫描器
// 负责对公开/临时/私有文件进行全量扫描, 为每个文件建立索引记录。
// 扫描逻辑: 遍历文件系统/数据库, 新增不存在的记录。
// 支持多 scope 扫描，每个 scope 独立进度跟踪。
type Scanner struct {
	db           *gorm.DB
	rootNames    map[string]string
	tempFilePath string
	privatePath  string
	progresses   map[ScanScope]*ScanProgress // 每个 scope 独立进度
	mu           sync.Mutex
	batchSize    int
	taskService  TaskRecorder // 可选，用于记录扫描任务到 task_records
}

// NewScanner 创建全量扫描器
func NewScanner(db *gorm.DB, rootNames map[string]string, tempFilePath, privatePath string) *Scanner {
	return &Scanner{
		db:           db,
		rootNames:    rootNames,
		tempFilePath: tempFilePath,
		privatePath:  privatePath,
		progresses:   make(map[ScanScope]*ScanProgress),
		batchSize:    500,
	}
}

// getOrCreateProgress 获取或创建指定 scope 的进度对象
func (s *Scanner) getOrCreateProgress(scope ScanScope) *ScanProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.progresses[scope]; ok {
		return p
	}
	p := &ScanProgress{}
	s.progresses[scope] = p
	return p
}

// StartScan 启动全量扫描（向后兼容，默认扫描 public）
func (s *Scanner) StartScan(ctx context.Context) error {
	return s.StartScanScope(ctx, ScanScopePublic, "manual")
}

// StartScanScope 启动指定 scope 的全量扫描
// 异步执行, 不阻塞调用方。扫描进度可通过 GetProgress 查询。
// 每个 scope 独立进度跟踪，互不干扰。
func (s *Scanner) StartScanScope(ctx context.Context, scope ScanScope, trigger string) error {
	progress := s.getOrCreateProgress(scope)

	// 只有在空闲/完成/失败状态时才启动扫描
	// 运行中状态直接返回
	currentStatus := progress.Get().Status
	if currentStatus == ScanStatusRunning {
		return nil // 已在运行中
	}

	progress.SetStatus(ScanStatusRunning)
	progress.SetCurrentFile("")
	progress.SetTotalFiles(0)

	switch scope {
	case ScanScopePublic:
		go s.runScan(ctx, trigger)
	case ScanScopeTemp:
		// 临时文件不建立索引，直接标记完成
		progress.SetTotalFiles(0)
		progress.SetStatus(ScanStatusCompleted)
	case ScanScopePrivate:
		go s.runPrivateScan(ctx)
	}
	return nil
}

// GetProgress 获取扫描进度（向后兼容，返回 public 进度）
func (s *Scanner) GetProgress() ScanProgress {
	return s.getOrCreateProgress(ScanScopePublic).Get()
}

// GetScopeProgress 获取指定 scope 的扫描进度
func (s *Scanner) GetScopeProgress(scope ScanScope) ScanProgress {
	return s.getOrCreateProgress(scope).Get()
}

// rootScanResult 单个根目录扫描结果（内部类型, 用于 channel 传递）
type rootScanResult struct {
	name   string
	result *models.ScanRootResult
	err    error
}
