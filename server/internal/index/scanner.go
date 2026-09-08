package index

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
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

// runScan 执行实际扫描流程
func (s *Scanner) runScan(ctx context.Context, trigger string) {
	startTime := utils.Now()
	scanRecord := s.createScanRecord()

	// 创建任务记录（供任务管理页面展示）
	var taskID uint
	if s.taskService != nil {
		if task, err := s.taskService.CreateTask("scan", "全量扫描", trigger); err == nil {
			taskID = task.ID
			s.taskService.StartTask(taskID)
		}
	}

	// 先统计文件数（可能耗时，每10秒报告进度）
	utils.Info("全量扫描开始: 正在统计文件总数...")
	var totalFiles int64
	countStart := utils.Now()
	countDone := make(chan struct{}, 1)
	go func() {
		for _, rootPath := range s.rootNames {
			count, _ := countFiles(rootPath)
			totalFiles += count
		}
		countDone <- struct{}{}
	}()
	countTicker := time.NewTicker(3 * time.Second)
	defer countTicker.Stop()
	select {
	case <-countDone:
	case <-countTicker.C:
		utils.Info("文件总数统计中, 已耗时 " + time.Since(countStart).Round(time.Second).String())
		// 等待统计完成
		<-countDone
	}
	utils.Info("全量扫描文件总数统计完成",
		utils.Int64("total_files", totalFiles),
		utils.Duration("count_elapsed", time.Since(countStart)))
	s.getOrCreateProgress(ScanScopePublic).SetTotalFiles(totalFiles)
	s.updateScanRecord(scanRecord.ID, map[string]interface{}{
		"TotalFiles": totalFiles,
	})

	// 按名称排序根目录保证顺序
	names := make([]string, 0, len(s.rootNames))
	for name := range s.rootNames {
		names = append(names, name)
	}
	sort.Strings(names)

	// 使用 channel 收集扫描结果
	resultCh := make(chan rootScanResult, len(names))

	// 每个根目录启动一个 goroutine 并行扫描
	for _, name := range names {
		path := s.rootNames[name]
		go func(rn, rp string) {
			result, err := s.scanRootDir(ctx, rn, rp)
			resultCh <- rootScanResult{name: rn, result: result, err: err}
		}(name, path)
	}

	// 收集根目录扫描结果
	var rootResults []models.ScanRootResult

	// 等待所有根目录扫描完成（同时定期保存进度）
	waitTicker := time.NewTicker(10 * time.Second)
	defer waitTicker.Stop()
	persistTicker := time.NewTicker(30 * time.Second)
	defer persistTicker.Stop()
	var firstError error
	remaining := len(names)
	for remaining > 0 {
		select {
		case res := <-resultCh:
			remaining--
			if res.err != nil {
				utils.Error("根目录扫描失败",
					utils.String("root_name", res.name),
					utils.Err(res.err))
				if firstError == nil {
					firstError = res.err
				}
			}
			if res.result != nil {
				rootResults = append(rootResults, *res.result)
			}
		case <-waitTicker.C:
			p := s.getOrCreateProgress(ScanScopePublic).Get()
			utils.Info("等待根目录扫描完成",
				utils.Int("remaining", remaining),
				utils.Int64("total_scanned", p.ScannedFiles),
				utils.Int64("total_files", p.TotalFiles))
		case <-persistTicker.C:
			p := s.getOrCreateProgress(ScanScopePublic).Get()
			s.updateScanRecord(scanRecord.ID, map[string]interface{}{
				"ScannedFiles": p.ScannedFiles,
			})
		case <-ctx.Done():
			s.getOrCreateProgress(ScanScopePublic).SetError(ctx.Err().Error())
			s.finalizeScanRecord(scanRecord.ID, models.ScanRecordStatusFailed,
				ctx.Err().Error(), rootResults)
			return
		}
	}

	elapsed := time.Since(startTime)
	p := s.getOrCreateProgress(ScanScopePublic).Get()
	details := buildScanDetails(p.ScannedFiles, rootResults)
	if firstError != nil {
		s.getOrCreateProgress(ScanScopePublic).SetError(firstError.Error())
		s.finalizeScanRecord(scanRecord.ID, models.ScanRecordStatusFailed,
			firstError.Error(), rootResults)
		if taskID > 0 && s.taskService != nil {
			s.taskService.UpdateTaskProgress(taskID, 100, p.ScannedFiles, p.TotalFiles)
			s.taskService.UpdateTaskDetails(taskID, details)
			s.taskService.FailTask(taskID, firstError.Error())
		}
		utils.Error("全量扫描完成（有错误）",
			utils.Int64("scanned", p.ScannedFiles),
			utils.Duration("elapsed", elapsed),
			utils.Err(firstError))
	} else {
		s.getOrCreateProgress(ScanScopePublic).SetStatus(ScanStatusCompleted)
		s.finalizeScanRecord(scanRecord.ID, models.ScanRecordStatusCompleted,
			"", rootResults)
		if taskID > 0 && s.taskService != nil {
			s.taskService.UpdateTaskProgress(taskID, 100, p.ScannedFiles, p.TotalFiles)
			s.taskService.UpdateTaskDetails(taskID, details)
			s.taskService.CompleteTask(taskID)
		}
		utils.Info("全量扫描完成",
			utils.Int64("scanned", p.ScannedFiles),
			utils.Duration("elapsed", elapsed))
	}
}

// buildScanDetails 汇总扫描结果，生成任务明细文本
func buildScanDetails(scanned int64, rootResults []models.ScanRootResult) string {
	var added, deleted int64
	for _, r := range rootResults {
		added += r.Added
		deleted += r.Deleted
	}
	b := strings.Builder{}
	b.WriteString(fmt.Sprintf("扫描 %d 个文件", scanned))
	if added > 0 {
		fmt.Fprintf(&b, "，新增 %d", added)
	}
	if deleted > 0 {
		fmt.Fprintf(&b, "，删除 %d", deleted)
	}
	return b.String()
}

// createScanRecord 创建数据库扫描记录
func (s *Scanner) createScanRecord() *models.ScanRecord {
	record := &models.ScanRecord{
		Status:    models.ScanRecordStatusRunning,
		StartedAt: utils.Now(),
	}
	if err := s.db.Create(record).Error; err != nil {
		utils.Warn("创建扫描记录失败", utils.Err(err))
	}
	return record
}

// updateScanRecord 更新扫描记录字段
func (s *Scanner) updateScanRecord(id uint, fields map[string]interface{}) {
	fields["updated_at"] = utils.Now()
	if err := s.db.Model(&models.ScanRecord{}).Where("id = ?", id).Updates(fields).Error; err != nil {
		utils.Warn("更新扫描记录失败",
			utils.Int("record_id", int(id)),
			utils.Err(err))
	}
}

// finalizeScanRecord 完成扫描记录（设置状态、结束时间、结果）
func (s *Scanner) finalizeScanRecord(id uint, status models.ScanRecordStatus, errMsg string, rootResults []models.ScanRootResult) {
	now := utils.Now()
	fields := map[string]interface{}{
		"status":     status,
		"ended_at":   now,
		"updated_at": now,
	}

	p := s.getOrCreateProgress(ScanScopePublic).Get()
	fields["scanned_files"] = p.ScannedFiles

	if errMsg != "" {
		fields["error_message"] = errMsg
	}

	if len(rootResults) > 0 {
		var totalAdded, totalDeleted int64
		for _, r := range rootResults {
			totalAdded += r.Added
			totalDeleted += r.Deleted
		}
		fields["added_files"] = totalAdded
		fields["deleted_files"] = totalDeleted

		if data, err := json.Marshal(rootResults); err == nil {
			fields["root_results"] = string(data)
		}
	}

	if err := s.db.Model(&models.ScanRecord{}).Where("id = ?", id).Updates(fields).Error; err != nil {
		utils.Warn("完成扫描记录失败",
			utils.Int("record_id", int(id)),
			utils.Err(err))
	}
}

// scanRootDir 扫描单个根目录, 返回根目录扫描结果
func (s *Scanner) scanRootDir(ctx context.Context, rootName, rootPath string) (*models.ScanRootResult, error) {
	utils.Info("开始扫描根目录",
		utils.String("root_name", rootName),
		utils.String("root_path", rootPath))

	result := &models.ScanRootResult{RootName: rootName}

	// 加载已有活跃记录, 建立 path→ID 映射
	existing := make(map[string]uint) // filePath → recordID
	var records []models.FileRecordPublic
	if err := s.db.Where("root_name = ? AND status = ?",
		rootName, models.FileStatusActive).Find(&records).Error; err != nil {
		result.Error = err.Error()
		return result, err
	}
	for _, r := range records {
		existing[r.FilePath] = r.ID
	}

	// 批量插入缓冲区
	var batch []models.FileRecordPublic

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		// 写入 file_records_public（批量写加写锁，避免与 HashWorker 抢占写锁）
		release := lockWrite()
		defer release()
		if err := s.db.CreateInBatches(batch, s.batchSize).Error; err != nil {
			return err
		}
		return nil
	}

	var scanned int64 // 当前根目录已扫描文件数
	var added int64   // 当前根目录新增索引数
	progressTicker := time.NewTicker(10 * time.Second)
	defer progressTicker.Stop()

	walkErr := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			utils.Warn("扫描跳过无法访问的文件",
				utils.String("path", path),
				utils.Err(err))
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		relPath, relErr := filepath.Rel(rootPath, path)
		if relErr != nil || relPath == "." {
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		// 标准化 file_path：确保有前导 /，FullPath 格式：/rootName/path
		if !strings.HasPrefix(relPath, "/") {
			relPath = "/" + relPath
		}

		modTime := info.ModTime()
		scanned++

		// 定期输出进度
		select {
		case <-progressTicker.C:
			p := s.getOrCreateProgress(ScanScopePublic).Get()
			utils.Info("扫描进度",
				utils.String("root_name", rootName),
				utils.Int64("root_scanned", scanned),
				utils.Int64("total_scanned", p.ScannedFiles),
				utils.Int64("total_files", p.TotalFiles),
				utils.String("current", path))
		default:
		}

		// 检查是否已在索引中
		if _, exists := existing[relPath]; exists {
			// 已存在, 从待处理集合中移除
			delete(existing, relPath)
			s.getOrCreateProgress(ScanScopePublic).IncScanned()
			s.getOrCreateProgress(ScanScopePublic).SetCurrentFile(path)
			return nil
		}

		// 新增记录（hash 留空，由 HashWorker 后续处理）
		added++

		now := utils.Now()
		rec := models.FileRecordPublic{
			FileRecordBase: models.FileRecordBase{
				FileName:     info.Name(),
				FilePath:     relPath,
				RootName:     rootName,
				FullPath:     "/" + rootName + relPath,
				FileSize:     fileSizeFromInfo(info),
				IsDir:        info.IsDir(),
				ModTime:      modTime,
				Status:       models.FileStatusActive,
				LastSyncedAt: now,
			},
		}
		batch = append(batch, rec)

		if len(batch) >= s.batchSize {
			if ferr := flushBatch(); ferr != nil {
				return ferr
			}
			batch = batch[:0]
		}

		s.getOrCreateProgress(ScanScopePublic).IncScanned()
		s.getOrCreateProgress(ScanScopePublic).SetCurrentFile(path)
		return nil
	})

	// 刷新剩余批次
	if len(batch) > 0 {
		if ferr := flushBatch(); ferr != nil && walkErr == nil {
			walkErr = ferr
		}
	}

	result.Scanned = scanned
	result.Added = added

	if walkErr != nil {
		result.Error = walkErr.Error()
		utils.Warn("根目录扫描异常结束",
			utils.String("root_name", rootName),
			utils.Int64("scanned", scanned),
			utils.Int64("added", added),
			utils.Err(walkErr))
		return result, walkErr
	}

	// 仅在扫描无错误时处理已删除的文件标记
	// 如果扫描中途失败（如批量写入错误）, 不标记任何记录为已删除,
	// 避免因扫描中断而错误地将未访问到的文件标记为已删除
	deleted := len(existing)
	if deleted > 0 {
		idsToDelete := make([]uint, 0, deleted)
		for _, id := range existing {
			idsToDelete = append(idsToDelete, id)
		}
		now := utils.Now()
		release := lockWrite()
		if err := s.db.Model(&models.FileRecordPublic{}).
			Where("id IN ?", idsToDelete).
			Updates(map[string]interface{}{
				"status":     models.FileStatusDeleted,
				"updated_at": now,
			}).Error; err != nil {
			release()
			utils.Error("标记已删除文件失败",
				utils.Int("count", deleted),
				utils.Err(err))
		} else {
			release()
			utils.Info("已标记文件为已删除",
				utils.Int("count", deleted),
				utils.String("root_name", rootName))
		}
	}
	result.Deleted = int64(deleted)

	utils.Info("根目录扫描完成",
		utils.String("root_name", rootName),
		utils.Int64("scanned", scanned),
		utils.Int64("added", added),
		utils.Int("deleted", deleted))

	return result, nil
}

// runTempScan 扫描临时文件，写入 file_records_temp
func (s *Scanner) runTempScan(ctx context.Context) {
	progress := s.getOrCreateProgress(ScanScopeTemp)
	startTime := utils.Now()

	var tempFiles []models.TempFile
	if err := s.db.Find(&tempFiles).Error; err != nil {
		progress.SetError(err.Error())
		utils.Error("查询临时文件列表失败", utils.Err(err))
		return
	}

	total := int64(len(tempFiles))
	progress.SetTotalFiles(total)
	utils.Info("临时文件索引扫描开始",
		utils.Int64("total", total))

	if total == 0 {
		progress.SetStatus(ScanStatusCompleted)
		utils.Info("临时文件索引扫描完成: 无文件")
		return
	}

	now := utils.Now()
	var batch []models.FileRecordTemp

	// 每10秒报告进度
	progressTicker := time.NewTicker(10 * time.Second)
	defer progressTicker.Stop()

	for i, tf := range tempFiles {
		select {
		case <-ctx.Done():
			progress.SetError(ctx.Err().Error())
			return
		default:
		}
		select {
		case <-progressTicker.C:
			pct := 0
			if total > 0 {
				pct = int(float64(i) / float64(total) * 100)
			}
			utils.Info("临时文件索引扫描进度",
				utils.Int("processed", i+1),
				utils.Int64("total", total),
				utils.Int("percent", pct),
				utils.String("current", tf.Filename),
				utils.Duration("elapsed", time.Since(startTime)))
		default:
		}

		hash := tf.Code // 使用 code 作为简化标识
		batch = append(batch, models.FileRecordTemp{
			FileRecordBase: models.FileRecordBase{
				FileName:     tf.Filename,
				FilePath:     hash,
				RootName:     "temp",
				FullPath:     "temp/" + hash,
				FileSize:     tf.FileSize,
				IsDir:        false,
				ModTime:      tf.CreatedAt,
				Status:       models.FileStatusActive,
				OwnerID:      tf.ClientIP,
				LastSyncedAt: now,
			},
		})
		progress.SetCurrentFile(tf.Filename)
		progress.SetScanned(int64(i + 1))

		if len(batch) >= s.batchSize {
			release := lockWrite()
			err := s.db.CreateInBatches(batch, s.batchSize).Error
			release()
			if err != nil {
				utils.Error("批量写入 file_records_temp 失败", utils.Err(err))
			}
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		release := lockWrite()
		err := s.db.CreateInBatches(batch, s.batchSize).Error
		release()
		if err != nil {
			utils.Error("批量写入 file_records_temp 失败(尾批)", utils.Err(err))
		}
	}

	progress.SetStatus(ScanStatusCompleted)
	utils.Info("临时文件索引扫描完成",
		utils.Int("count", len(tempFiles)),
		utils.Duration("elapsed", time.Since(startTime)))
}

// runPrivateScan 扫描私有文件，写入 file_records_private
func (s *Scanner) runPrivateScan(ctx context.Context) {
	progress := s.getOrCreateProgress(ScanScopePrivate)
	startTime := utils.Now()

	if s.privatePath == "" {
		progress.SetError("私有文件路径未配置")
		return
	}

	usersDir := filepath.Join(s.privatePath, "users")
	userEntries, err := os.ReadDir(usersDir)
	if err != nil {
		progress.SetError(err.Error())
		utils.Error("读取私有文件用户目录失败", utils.Err(err))
		return
	}

	// 先统计总文件数
	utils.Info("私有文件索引扫描开始: 正在统计用户文件...")
	var totalFiles int64
	for _, ue := range userEntries {
		if !ue.IsDir() {
			continue
		}
		codeDir := filepath.Join(usersDir, ue.Name())
		entries, _ := os.ReadDir(codeDir)
		for _, ce := range entries {
			if ce.IsDir() {
				totalFiles++
			}
		}
	}

	if totalFiles == 0 {
		progress.SetStatus(ScanStatusCompleted)
		utils.Info("私有文件索引扫描完成: 无文件")
		return
	}

	progress.SetTotalFiles(totalFiles)
	now := utils.Now()
	var batch []models.FileRecordPrivate
	var scanned int64

	// 每10秒报告进度
	progressTicker := time.NewTicker(10 * time.Second)
	defer progressTicker.Stop()

	for _, userEntry := range userEntries {
		if !userEntry.IsDir() {
			continue
		}
		userID := userEntry.Name()

		select {
		case <-ctx.Done():
			progress.SetError(ctx.Err().Error())
			return
		default:
		}

		userDir := filepath.Join(usersDir, userID)
		codeEntries, err := os.ReadDir(userDir)
		if err != nil {
			continue
		}

		for _, codeEntry := range codeEntries {
			if !codeEntry.IsDir() {
				continue
			}

			select {
			case <-ctx.Done():
				return
			default:
			}

			dataPath := filepath.Join(userDir, codeEntry.Name(), "data")
			dataInfo, err := os.Stat(dataPath)
			if err != nil {
				continue
			}

			// 读取 meta.json 获取文件名
			metaPath := filepath.Join(userDir, codeEntry.Name(), "meta.json")
			metaBytes, readErr := os.ReadFile(metaPath)
			fileName := dataInfo.Name()
			if readErr == nil {
				var meta struct {
					Filename string `json:"filename"`
				}
				if json.Unmarshal(metaBytes, &meta) == nil && meta.Filename != "" {
					fileName = meta.Filename
				}
			}

			batch = append(batch, models.FileRecordPrivate{
				FileRecordBase: models.FileRecordBase{
					FileName:     fileName,
					FilePath:     codeEntry.Name(),
					RootName:     "private",
					FullPath:     "private/" + codeEntry.Name(),
					FileSize:     dataInfo.Size(),
					IsDir:        false,
					ModTime:      dataInfo.ModTime(),
					Status:       models.FileStatusActive,
					OwnerID:      userID,
					LastSyncedAt: now,
				},
			})
			scanned++
			progress.SetCurrentFile(fileName)
			progress.SetScanned(scanned)

			select {
			case <-progressTicker.C:
				pct := 0
				if totalFiles > 0 {
					pct = int(float64(scanned) / float64(totalFiles) * 100)
				}
				utils.Info("私有文件索引扫描进度",
					utils.Int64("processed", scanned),
					utils.Int64("total", totalFiles),
					utils.Int("percent", pct),
					utils.String("current", fileName),
					utils.String("user", userID),
					utils.Duration("elapsed", time.Since(startTime)))
			default:
			}

			if len(batch) >= s.batchSize {
				release := lockWrite()
				err := s.db.CreateInBatches(batch, s.batchSize).Error
				release()
				if err != nil {
					utils.Error("批量写入 file_records_private 失败", utils.Err(err))
				}
				batch = batch[:0]
			}
		}
	}

	if len(batch) > 0 {
		release := lockWrite()
		err := s.db.CreateInBatches(batch, s.batchSize).Error
		release()
		if err != nil {
			utils.Error("批量写入 file_records_private 失败(尾批)", utils.Err(err))
		}
	}

	progress.SetStatus(ScanStatusCompleted)
	utils.Info("私有文件索引扫描完成",
		utils.Int64("count", scanned),
		utils.Duration("elapsed", time.Since(startTime)))
}

// countFiles 统计目录下文件数（用于进度估算）
func countFiles(rootPath string) (int64, error) {
	var count int64
	countStart := utils.Now()
	countTicker := time.NewTicker(10 * time.Second)
	defer countTicker.Stop()

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			count++
		}
		select {
		case <-countTicker.C:
			utils.Info("文件统计中",
				utils.String("root", rootPath),
				utils.Int64("files", count),
				utils.Duration("elapsed", time.Since(countStart)))
		default:
		}
		return nil
	})
	return count, err
}
