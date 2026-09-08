package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// Service 文件索引服务
// 提供文件索引的 CRUD、全量扫描、增量同步、一致性校验、哈希计算等能力。
// 作为组合 facade, 内部协调 Scanner/Syncer/ConsistencyChecker/HashWorker。
type Service struct {
	db         *gorm.DB
	rootNames  map[string]string
	scanner    *Scanner
	syncer     *Syncer
	checker    *ConsistencyChecker
	hashWorker *HashWorker

	// 任务记录服务（可选，用于记录扫描/哈希等任务到 task_records 表）
	taskService TaskRecorder

	// 定时扫描
	mu           sync.Mutex
	timerStopCh  chan struct{}
	timerRunning bool
	lastScanEnd  time.Time // 上次扫描完成时间
	currentScope ScanScope // 当前正在扫描的 scope

	// 文件监听
	watcher *Watcher

	// 最近同步的文件记录（key: rootName:relPath，用于防止 watcher 重复处理上传文件）
	recentlySynced map[string]time.Time
	recentlyMu     sync.Mutex
	recentlyStopCh chan struct{}
}

// TaskRecorder 任务记录接口（解耦 index 包对 services 包的依赖）
type TaskRecorder interface {
	CreateTask(taskType, taskName, trigger string) (*models.TaskRecord, error)
	StartTask(id uint) error
	UpdateTaskProgress(id uint, progress int, doneItems, totalItems int64) error
	UpdateTaskDetails(id uint, details string) error
	CompleteTask(id uint) error
	FailTask(id uint, errMsg string) error
}

// SetTaskService 注入任务记录服务
func (s *Service) SetTaskService(ts TaskRecorder) {
	s.taskService = ts
	s.scanner.taskService = ts
	s.hashWorker.taskService = ts
}

// SetFileIndexNotifier 注入文件名检索索引变更通知器（可选）。
// 文件增/删/移动时增量同步检索索引，保证搜索联想与拼写纠错与索引表一致。
func (s *Service) SetFileIndexNotifier(n FileIndexNotifier) {
	s.syncer.SetFileIndexNotifier(n)
}

// NewService 创建索引服务
func NewService(db *gorm.DB, rootNames map[string]string, tempFilePath, privatePath string) *Service {
	s := &Service{
		db:             db,
		rootNames:      rootNames,
		scanner:        NewScanner(db, rootNames, tempFilePath, privatePath),
		syncer:         NewSyncer(db, rootNames),
		checker:        NewConsistencyChecker(db, rootNames),
		hashWorker:     NewHashWorker(db, rootNames),
		recentlySynced: make(map[string]time.Time),
		recentlyStopCh: make(chan struct{}),
	}
	// 启动最近同步记录清理协程
	go s.cleanupRecentlySynced()
	return s
}

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
	return s.syncer.MoveFile(rootName, oldPath, newPath)
}

// ListRecords 分页查询文件记录
func (s *Service) ListRecords(query ListRecordsQuery) ([]models.FileRecordPublic, int64, error) {
	query.Normalize()

	// 根据 table 参数选择查询的表
	tableName := "file_records_public"
	switch query.Table {
	case "file_records_public":
		tableName = "file_records_public"
	case "file_records_temp":
		tableName = "file_records_temp"
	case "file_records_private":
		tableName = "file_records_private"
	}
	dbQuery := s.db.Table(tableName)

	// 根目录过滤
	if query.RootName != "" {
		dbQuery = dbQuery.Where("root_name = ?", query.RootName)
	}

	// 所有者过滤
	if query.OwnerID != "" {
		dbQuery = dbQuery.Where("owner_id = ?", query.OwnerID)
	}

	// 状态过滤
	switch query.Status {
	case "active":
		dbQuery = dbQuery.Where("status = ?", models.FileStatusActive)
	case "deleted":
		dbQuery = dbQuery.Where("status = ?", models.FileStatusDeleted)
		// "all" 不添加状态过滤
	}

	// 文件名搜索
	if query.Query != "" {
		dbQuery = dbQuery.Where("file_name LIKE ?", "%"+query.Query+"%")
	}

	// 总数
	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("查询总数失败: %w", err)
	}

	// 排序
	sortField := map[string]string{
		"name": "file_name",
		"size": "file_size",
		"time": "mod_time",
	}
	orderField := "file_name"
	if f, ok := sortField[query.Sort]; ok {
		orderField = f
	}
	orderDir := "ASC"
	if query.Order == "desc" {
		orderDir = "DESC"
	}
	orderClause := fmt.Sprintf("%s %s", orderField, orderDir)

	// 分页
	offset := (query.Page - 1) * query.PageSize
	var records []models.FileRecordPublic
	if err := dbQuery.Order(orderClause).
		Offset(offset).Limit(query.PageSize).
		Find(&records).Error; err != nil {
		return nil, 0, fmt.Errorf("查询记录失败: %w", err)
	}

	if records == nil {
		records = make([]models.FileRecordPublic, 0)
	}

	return records, total, nil
}

// GetStats 获取文件统计
func (s *Service) GetStats() (*RecordStatsResponse, error) {
	var activeCount, dirCount, deletedCount int64
	if err := s.db.Model(&models.FileRecordPublic{}).
		Where("status = ?", models.FileStatusActive).
		Count(&activeCount).Error; err != nil {
		return nil, err
	}

	var sizeSum int64
	if row := s.db.Model(&models.FileRecordPublic{}).
		Where("status = ? AND is_dir = ?", models.FileStatusActive, false).
		Select("COALESCE(SUM(file_size), 0)").
		Row(); row != nil {
		if err := row.Scan(&sizeSum); err != nil {
			return nil, err
		}
	}

	if err := s.db.Model(&models.FileRecordPublic{}).
		Where("status = ? AND is_dir = ?", models.FileStatusActive, true).
		Count(&dirCount).Error; err != nil {
		return nil, err
	}

	if err := s.db.Model(&models.FileRecordPublic{}).
		Where("status = ?", models.FileStatusDeleted).
		Count(&deletedCount).Error; err != nil {
		return nil, err
	}

	// 重复文件组数（有重复哈希值的组）
	var dupGroups int64
	if err := s.db.Raw(`
        SELECT COUNT(*) FROM (
            SELECT xxh3_hash FROM file_records_public
            WHERE status = ? AND xxh3_hash != '' AND file_size > 0
            GROUP BY xxh3_hash HAVING COUNT(*) > 1
        ) dups
    `, string(models.FileStatusActive)).Scan(&dupGroups).Error; err != nil {
		return nil, err
	}

	return &RecordStatsResponse{
		TotalFiles:      activeCount,
		TotalSize:       sizeSum,
		TotalDirs:       dirCount,
		DeletedFiles:    deletedCount,
		ActiveFiles:     activeCount,
		DuplicateGroups: dupGroups,
		TotalRootDirs:   len(s.rootNames),
	}, nil
}

// ListDuplicateGroups 查询重复文件分组
func (s *Service) ListDuplicateGroups(query DuplicateQuery) ([]DuplicateGroup, int64, error) {
	query.Normalize()

	// 分组总数（返回系统中所有重复分组总数，不含 MinSize 过滤；用 GORM 子查询而非 raw SQL，
	// 避免读取到过期快照导致处理重复项后 total 不随 groups 实时减少）
	totalDup := s.db.Model(&models.FileRecordPublic{}).
		Select("1 AS one").
		Where("status = ? AND xxh3_hash != '' AND is_dir = ? AND file_size > 0",
			string(models.FileStatusActive), false).
		Group("xxh3_hash").Having("COUNT(*) > 1")
	var total int64
	if err := s.db.Table("(?) AS t", totalDup).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("查询重复分组总数失败: %w", err)
	}

	// 分组排序
	orderField := "size_sum"
	orderDir := "DESC"
	if query.Sort == "count" {
		orderField = "count"
	} else if query.Sort == "hash" {
		orderField = "xxh3_hash"
	}
	if query.Order == "asc" {
		orderDir = "ASC"
	}

	// 先查询有重复的哈希列表（当 xxh3_hash 不为空且不统计目录）
	var hashCounts []struct {
		Xxh3Hash string
		Count    int64
		SizeSum  int64
	}
	countQuery := s.db.Model(&models.FileRecordPublic{}).
		Select("xxh3_hash, COUNT(*) as count, SUM(file_size) as size_sum").
		Where("status = ? AND xxh3_hash != '' AND is_dir = ? AND file_size > 0",
			string(models.FileStatusActive), false)

	if query.MinSize > 0 {
		countQuery = countQuery.Where("file_size >= ?", query.MinSize)
	}

	offset := (query.Page - 1) * query.PageSize
	if err := countQuery.Group("xxh3_hash").
		Having("COUNT(*) > 1").
		Order(fmt.Sprintf("%s %s", orderField, orderDir)).
		Offset(offset).Limit(query.PageSize).
		Scan(&hashCounts).Error; err != nil {
		return nil, 0, fmt.Errorf("查询重复分组失败: %w", err)
	}

	if len(hashCounts) == 0 {
		return make([]DuplicateGroup, 0), total, nil
	}

	// 查询每个分组中的文件详情
	groups := make([]DuplicateGroup, 0, len(hashCounts))
	for _, hc := range hashCounts {
		var files []DuplicateFileItem
		if err := s.db.Model(&models.FileRecordPublic{}).
			Select("id, file_name, file_path, full_path, root_name, file_size, mod_time").
			Where("xxh3_hash = ? AND status = ? AND is_dir = ? AND file_size > 0",
				hc.Xxh3Hash, models.FileStatusActive, false).
			Order("file_path ASC").
			Find(&files).Error; err != nil {
			utils.Warn("查询重复分组文件详情失败", utils.String("hash", hc.Xxh3Hash), utils.Err(err))
			continue
		}
		groups = append(groups, DuplicateGroup{
			Xxh3Hash:  hc.Xxh3Hash,
			FileCount: hc.Count,
			TotalSize: hc.SizeSum,
			Files:     files,
		})
	}

	return groups, total, nil
}

// UpdateNotes 更新文件备注
func (s *Service) UpdateNotes(id uint, notes string) error {
	result := s.db.Model(&models.FileRecordPublic{}).Where("id = ?", id).
		Update("notes", notes)
	if result.Error != nil {
		return fmt.Errorf("更新备注失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("文件记录不存在: %d", id)
	}
	return nil
}

// GetRecord 获取单个文件记录
func (s *Service) GetRecord(id uint) (*models.FileRecordPublic, error) {
	var record models.FileRecordPublic
	if err := s.db.First(&record, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("记录不存在: %d", id)
		}
		return nil, fmt.Errorf("查询记录失败: %w", err)
	}
	return &record, nil
}

// FindRecordByPath 根据文件名和路径查找文件记录
func (s *Service) FindRecordByPath(fileName, rootName, filePath string) (*models.FileRecordPublic, error) {
	var record models.FileRecordPublic
	if err := s.db.Where("file_name = ? AND root_name = ? AND file_path = ? AND status = 'active'",
		fileName, rootName, filePath).
		First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil // 未找到不返回错误
		}
		return nil, fmt.Errorf("查询文件记录失败: %w", err)
	}
	return &record, nil
}

// SearchFileRecords 搜索文件记录（用于 autocomplete）
func (s *Service) SearchFileRecords(query string, limit int) ([]models.FileRecordPublic, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var records []models.FileRecordPublic
	if err := s.db.Where("file_name LIKE ?", "%"+query+"%").
		Order("file_size DESC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("搜索文件记录失败: %w", err)
	}
	if records == nil {
		records = make([]models.FileRecordPublic, 0)
	}
	return records, nil
}

// DeleteRecord 硬删除索引记录（管理员操作）
func (s *Service) DeleteRecord(id uint) error {
	result := s.db.Delete(&models.FileRecordPublic{}, id)
	if result.Error != nil {
		return fmt.Errorf("删除索引记录失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("索引记录不存在: %d", id)
	}
	return nil
}

// KeepDuplicate 保留指定文件记录，删除同哈希的其他重复文件（索引记录 + 磁盘文件）
// 返回被删除的文件列表
type KeptDuplicateResult struct {
	DeletedFiles []DuplicateFileItem `json:"deletedFiles"`
}

func (s *Service) KeepDuplicate(id uint) (*KeptDuplicateResult, error) {
	// 获取要保留的记录
	record, err := s.GetRecord(id)
	if err != nil {
		return nil, fmt.Errorf("获取文件记录失败: %w", err)
	}
	if record.Xxh3Hash == "" {
		return nil, fmt.Errorf("文件记录没有哈希值，无法查找重复文件")
	}

	// 查询同哈希的其他记录
	var duplicates []models.FileRecordPublic
	if err := s.db.Where("xxh3_hash = ? AND status = ? AND is_dir = ? AND id != ?",
		record.Xxh3Hash, models.FileStatusActive, false, record.ID).
		Find(&duplicates).Error; err != nil {
		return nil, fmt.Errorf("查询重复文件失败: %w", err)
	}

	if len(duplicates) == 0 {
		return &KeptDuplicateResult{DeletedFiles: make([]DuplicateFileItem, 0)}, nil
	}

	var deleted []DuplicateFileItem
	for _, dup := range duplicates {
		// 构建并删除磁盘文件
		rootPath, ok := s.rootNames[dup.RootName]
		if !ok {
			utils.Warn("保留重复文件时未找到根目录路径",
				utils.String("root_name", dup.RootName))
			continue
		}
		fullPath := filepath.Join(rootPath, dup.FilePath)
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			utils.Warn("删除重复文件磁盘文件失败",
				utils.String("path", fullPath),
				utils.Err(err))
			// 继续删除索引记录，不中断
		}

		// 删除索引记录
		if err := s.db.Delete(&models.FileRecordPublic{}, dup.ID).Error; err != nil {
			utils.Warn("删除重复文件索引记录失败",
				utils.Int("id", int(dup.ID)),
				utils.Err(err))
			continue
		}

		deleted = append(deleted, DuplicateFileItem{
			ID:       dup.ID,
			FileName: dup.FileName,
			FilePath: dup.FilePath,
			FullPath: dup.FullPath,
			RootName: dup.RootName,
			FileSize: dup.FileSize,
			ModTime:  dup.ModTime,
		})
	}

	return &KeptDuplicateResult{DeletedFiles: deleted}, nil
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

// getDefaultCronExpr 获取默认 cron 表达式
func getDefaultCronExpr() string {
	return "0 1 * * *" // 每天凌晨 1:00
}

// StartScanTimer 启动定时扫描器，根据 cron 表达式周期执行全量扫描
// 在 main.go 初始化后调用，配置变更时会自动重启
func (s *Service) StartScanTimer() {
	s.StopScanTimer() // 确保先停掉旧的

	cronExpr := appconfig.GlobalConfig.Index.ScanCronExpression
	if cronExpr == "" {
		cronExpr = getDefaultCronExpr()
	}

	// 验证 cron 表达式
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		utils.Warn("定时扫描 cron 表达式无效，使用默认值",
			utils.String("cron", cronExpr),
			utils.Err(err))
		cronExpr = getDefaultCronExpr()
		schedule, _ = parser.Parse(cronExpr)
	}

	s.timerStopCh = make(chan struct{})
	s.timerRunning = true

	go s.runScanTimer(schedule, cronExpr)

	utils.Info("定时扫描器已启动",
		utils.String("cron", cronExpr),
		utils.String("next_scan", schedule.Next(time.Now()).Format("2006-01-02 15:04:05")))
}

// StopScanTimer 停止定时扫描器
func (s *Service) StopScanTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timerStopCh != nil {
		close(s.timerStopCh)
		s.timerStopCh = nil
	}
	s.timerRunning = false
}

// RestartScanTimer 重启定时扫描器（用于配置热生效）
func (s *Service) RestartScanTimer() {
	utils.Info("配置变更，重启定时扫描器")
	s.StopScanTimer()
	s.StartScanTimer()
}

// runScanTimer 定时扫描核心循环，基于 cron 表达式调度
func (s *Service) runScanTimer(schedule cron.Schedule, cronExpr string) {
	// 捕获 stopCh 本地副本，防止 StopScanTimer 将 s.timerStopCh 置 nil
	// 后导致 nil channel 永久阻塞（goroutine 泄漏）
	s.mu.Lock()
	stopCh := s.timerStopCh
	s.mu.Unlock()

	// 计算下次执行时间
	nextTime := schedule.Next(time.Now())
	utils.Info("定时扫描下次执行时间",
		utils.String("next", nextTime.Format("2006-01-02 15:04:05")))

	for {
		now := utils.Now()
		if !now.Before(nextTime) {
			// 执行定时扫描
			utils.Info("定时扫描触发",
				utils.String("cron", cronExpr),
				utils.String("last_scan", func() string {
					s.mu.Lock()
					last := s.lastScanEnd
					s.mu.Unlock()
					if last.IsZero() {
						return "从未扫描"
					}
					return last.Format("2006-01-02 15:04:05")
				}()))
			s.runScheduledScan()

			// 计算下次执行时间
			nextTime = schedule.Next(time.Now())
			utils.Info("定时扫描下次执行时间",
				utils.String("next", nextTime.Format("2006-01-02 15:04:05")))

			// 检查配置变更（支持热生效）
			newCronExpr := appconfig.GlobalConfig.Index.ScanCronExpression
			if newCronExpr == "" {
				newCronExpr = getDefaultCronExpr()
			}
			if newCronExpr != cronExpr {
				parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
				if newSchedule, err := parser.Parse(newCronExpr); err == nil {
					cronExpr = newCronExpr
					schedule = newSchedule
					nextTime = schedule.Next(time.Now())
					utils.Info("定时扫描 cron 表达式已变更",
						utils.String("cron", cronExpr),
						utils.String("next", nextTime.Format("2006-01-02 15:04:05")))
				}
			}
		}

		// 等待到下次执行时间，同时响应停止信号
		waitDuration := nextTime.Sub(time.Now())
		if waitDuration < 0 {
			waitDuration = 0
		}

		// 每 30 秒检查一次停止信号，避免长时间阻塞
		checkTicker := time.NewTicker(30 * time.Second)
		select {
		case <-stopCh:
			checkTicker.Stop()
			utils.Info("定时扫描器已停止")
			return
		case <-time.After(waitDuration):
			checkTicker.Stop()
			// 时间到，进入下一轮循环执行扫描
		case <-checkTicker.C:
			checkTicker.Stop()
			// 继续循环重新检查时间
		}
	}
}

// runScheduledScan 执行一次完整的定时扫描（public → temp → private）
func (s *Service) runScheduledScan() {
	scopes := []ScanScope{ScanScopePublic, ScanScopeTemp, ScanScopePrivate}

	for _, scope := range scopes {
		// 按配置跳过未启用的范围
		switch scope {
		case ScanScopeTemp:
			if !appconfig.GetConfig().Storage.Temp.Enabled {
				utils.Info("定时扫描跳过临时文件范围（临时文件功能未启用）")
				continue
			}
		case ScanScopePrivate:
			if !appconfig.GetConfig().Storage.Private.Enabled {
				utils.Info("定时扫描跳过私有文件范围（私有存储功能未启用）")
				continue
			}
		}

		s.mu.Lock()
		s.currentScope = scope
		s.mu.Unlock()

		utils.Info("定时扫描进行中", utils.String("scope", string(scope)))
		err := s.StartScanByScope(scope, "timer")
		if err != nil {
			utils.Warn("定时扫描范围失败",
				utils.String("scope", string(scope)),
				utils.Err(err))
			// 继续下一个 scope，不中断
		}

		// 等待当前 scope 扫描完成，同时响应停止信号
		for {
			// 检查停止信号，避免扫描期间 StopScanTimer 被调后长时间无响应
			s.mu.Lock()
			running := s.timerRunning
			s.mu.Unlock()
			if !running {
				return
			}

			progress := s.scanner.GetScopeProgress(scope)
			if progress.Status == ScanStatusCompleted || progress.Status == ScanStatusFailed {
				if progress.Status == ScanStatusCompleted {
					utils.Info("定时扫描范围完成",
						utils.String("scope", string(scope)),
						utils.Int64("scanned_files", progress.ScannedFiles))
				} else {
					utils.Warn("定时扫描范围异常",
						utils.String("scope", string(scope)),
						utils.String("status", string(progress.Status)),
						utils.String("error", progress.ErrorMessage))
				}
				break
			}
			time.Sleep(2 * time.Second)
		}
	}

	s.mu.Lock()
	s.lastScanEnd = utils.Now()
	s.currentScope = ""
	s.mu.Unlock()

	utils.Info("定时扫描全部完成",
		utils.String("finish_time", s.lastScanEnd.Format("2006-01-02 15:04:05")))
}

// GetScanStatus 获取当前扫描状态（供 Footer API 使用）
func (s *Service) GetScanStatus() ScanStatusResponse {
	s.mu.Lock()
	lastScan := s.lastScanEnd
	s.mu.Unlock()

	cronExpr := appconfig.GlobalConfig.Index.ScanCronExpression
	if cronExpr == "" {
		cronExpr = getDefaultCronExpr()
	}

	var nextScan *time.Time
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if schedule, err := parser.Parse(cronExpr); err == nil {
		ns := schedule.Next(time.Now())
		nextScan = &ns
	}

	// 检查当前是否有扫描在进行（取任一 scope 的状态）
	isScanning := false
	scope := ""
	var processed, total int64
	for _, sc := range []ScanScope{ScanScopePublic, ScanScopeTemp, ScanScopePrivate} {
		p := s.scanner.GetScopeProgress(sc)
		if p.Status == ScanStatusRunning {
			isScanning = true
			scope = string(sc)
			processed = p.ScannedFiles
			total = p.TotalFiles
			break
		}
	}

	return ScanStatusResponse{
		LastScanTime:       copyTimePtr(&lastScan),
		NextScanTime:       nextScan,
		IsScanning:         isScanning,
		ScanScope:          scope,
		ScanProgress:       processed,
		ScanTotal:          total,
		ScanCronExpression: cronExpr,
	}
}

// copyTimePtr 返回 time.Time 指针的安全拷贝
func copyTimePtr(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	cp := *t
	return &cp
}

// MarkRecentlySynced 标记文件为最近已同步，防止 watcher 重复处理
// 上传完成后调用此方法，watcher 在处理 Create/Write 事件时会跳过此文件
func (s *Service) MarkRecentlySynced(rootName, relPath string) {
	key := rootName + ":" + relPath
	s.recentlyMu.Lock()
	s.recentlySynced[key] = utils.Now()
	s.recentlyMu.Unlock()
}

// IsRecentlySynced 检查文件是否最近已同步（30 秒内）
func (s *Service) IsRecentlySynced(rootName, relPath string) bool {
	key := rootName + ":" + relPath
	s.recentlyMu.Lock()
	t, ok := s.recentlySynced[key]
	s.recentlyMu.Unlock()
	if !ok {
		return false
	}
	return time.Since(t) < 30*time.Second
}

// cleanupRecentlySynced 定期清理过期的最近同步记录（每分钟执行一次）
func (s *Service) cleanupRecentlySynced() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.recentlyMu.Lock()
			now := utils.Now()
			for key, t := range s.recentlySynced {
				if now.Sub(t) > 30*time.Second {
					delete(s.recentlySynced, key)
				}
			}
			s.recentlyMu.Unlock()
		case <-s.recentlyStopCh:
			return
		}
	}
}

// StartWatcher 启动文件监听器（实时增量索引）
func (s *Service) StartWatcher() {
	s.watcher = NewWatcher(s.syncer, s.rootNames)
	// 注入最近同步检查回调，防止 watcher 重复处理上传流程已同步的文件
	s.watcher.skipRecentlySynced = s.IsRecentlySynced
	if err := s.watcher.Start(); err != nil {
		utils.Warn("文件监听器启动失败", utils.Err(err))
		s.watcher = nil
	}
}

// StopWatcher 停止文件监听器
func (s *Service) StopWatcher() {
	if s.watcher != nil {
		s.watcher.Stop()
		s.watcher = nil
	}
}

// RestartWatcher 重启文件监听器（用于配置热生效）
func (s *Service) RestartWatcher() {
	utils.Info("配置变更，重启文件监听器")
	s.StopWatcher()

	// 更新 rootNames（可能因配置变更）
	s.rootNames = appconfig.RootNames
	s.syncer.rootNames = appconfig.RootNames

	s.StartWatcher()
}
