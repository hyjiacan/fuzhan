package index

import (
	"context"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

// StartScan 启动全量扫描（默认 public）
func (s *Service) StartScan() error {
	return s.StartScanByScope(ScanScopePublic, "manual")
}

// StartScanByScope 启动指定 scope 的全量扫描
// 扫描完成后自动触发哈希计算
// 注意：临时文件不建立索引，直接跳过（在 Scanner.StartScanScope 中处理）
func (s *Service) StartScanByScope(scope ScanScope, trigger string) error {
	// 使用独立的 Background Context，不绑定 HTTP 请求生命周期
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	err := s.scanner.StartScanScope(ctx, scope, trigger)
	if err != nil {
		cancel()
		return err
	}
	// 扫描完成后释放 context
	go func() {
		for {
			progress := s.scanner.GetScopeProgress(scope)
			if progress.Status == ScanStatusCompleted || progress.Status == ScanStatusFailed {
				cancel()
				if progress.Status == ScanStatusCompleted {
					s.hashWorker.Trigger(context.Background())
					s.mu.Lock()
					s.lastScanEnd = utils.Now()
					s.currentScope = ""
					s.mu.Unlock()
					utils.Info("手动扫描完成",
						utils.String("scope", string(scope)),
						utils.String("finish_time", s.lastScanEnd.Format("2006-01-02 15:04:05")))
				}
				return
			}
			time.Sleep(5 * time.Second)
		}
	}()
	return nil
}

// GetScanProgress 获取扫描进度（返回 public 进度）
func (s *Service) GetScanProgress() ScanProgress {
	return s.scanner.GetProgress()
}

// GetScanProgressByScope 获取指定 scope 的扫描进度
func (s *Service) GetScanProgressByScope(scope ScanScope) ScanProgress {
	return s.scanner.GetScopeProgress(scope)
}

// TriggerHash 触发哈希计算（不阻塞，哈希 worker 后台执行）
func (s *Service) TriggerHash(ctx context.Context) {
	s.hashWorker.Trigger(ctx)
}

// GetHashProgress 获取哈希计算进度
func (s *Service) GetHashProgress() HashProgress {
	return s.hashWorker.Progress()
}

// ScannedStats 返回所有 scope 的扫描状态摘要
func (s *Service) ScannedStats() map[ScanScope]string {
	stats := make(map[ScanScope]string)
	for _, scope := range []ScanScope{ScanScopePublic, ScanScopeTemp, ScanScopePrivate} {
		p := s.scanner.GetScopeProgress(scope)
		stats[scope] = string(p.Status)
	}
	return stats
}

// RunConsistencyCheck 执行一致性校验
func (s *Service) RunConsistencyCheck(ctx context.Context) (*ConsistencyReport, error) {
	return s.checker.RunCheck(ctx)
}

// SyncFile 同步文件（增量上传/覆盖）
func (s *Service) SyncFile(rootName, filePath string) error {
	return s.syncer.SyncFile(rootName, filePath)
}

// RemoveFile 移除文件索引（软删除）
func (s *Service) RemoveFile(rootName, filePath string) error {
	return s.syncer.RemoveFile(rootName, filePath)
}

// MoveFile 移动文件索引
func (s *Service) MoveFile(rootName, oldPath, newPath string) error {
	err := s.syncer.MoveFile(rootName, oldPath, newPath)
	// 移动后标记新旧路径为最近已同步，避免文件监听器对移动产生的
	// Create/Remove 事件二次处理后造成索引记录丢失或重建（30 秒自动过期）
	if oldPath != newPath {
		s.MarkRecentlySynced(rootName, oldPath)
		s.MarkRecentlySynced(rootName, newPath)
	}
	return err
}

// IsEmpty 检查索引表是否为空（检查 file_records_public）
func (s *Service) IsEmpty() (bool, error) {
	return s.IsScopeEmpty(ScanScopePublic)
}

// IsScopeEmpty 检查指定 scope 的索引表是否为空
func (s *Service) IsScopeEmpty(scope ScanScope) (bool, error) {
	var count int64
	var err error

	switch scope {
	case ScanScopePublic:
		err = s.db.Model(&models.FileRecordPublic{}).Count(&count).Error
	case ScanScopeTemp:
		err = s.db.Model(&models.FileRecordTemp{}).Count(&count).Error
	case ScanScopePrivate:
		err = s.db.Model(&models.FileRecordPrivate{}).Count(&count).Error
	}

	if err != nil {
		return true, err
	}
	return count == 0, nil
}

// LastScanTime 获取最后扫描完成时间（基于内存记录，确保首次返回准确值）
func (s *Service) LastScanTime() *time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastScanEnd.IsZero() {
		return nil
	}
	t := s.lastScanEnd
	return &t
}
