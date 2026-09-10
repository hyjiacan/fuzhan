package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

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

// validateScanRoot 校验根目录能否被 filepath.Walk 可靠遍历。
// filepath.Walk 内部用 os.Lstat 判定根类型：对 junction/符号链接/挂载不可用等根,
// Lstat 返回的 IsDir() 为 false, Walk 不会下钻, 会得到 0 个条目。
// 此时若继续执行"未命中即删"逻辑, 会把有效索引批量标记为 deleted（数据丢失）。
// 返回 true 表示可遍历; false + reason 表示应跳过删除标记以保护索引。
func validateScanRoot(rootPath string) (bool, string) {
	li, err := os.Lstat(rootPath)
	if err != nil {
		return false, "根目录无法访问: " + err.Error()
	}
	if !li.IsDir() {
		return false, fmt.Sprintf("根目录不是可直接遍历的文件夹 (Lstat IsDir=false, mode=%v), 请配置目标的真实路径而不是挂在点/junction", li.Mode())
	}
	return true, ""
}

// scanRootDir 扫描单个根目录, 返回根目录扫描结果
func (s *Scanner) scanRootDir(ctx context.Context, rootName, rootPath string) (*models.ScanRootResult, error) {
	utils.Info("开始扫描根目录",
		utils.String("root_name", rootName),
		utils.String("root_path", rootPath))

	result := &models.ScanRootResult{RootName: rootName}

	// 护栏：根不可遍历（junction/符号链接/挂载不可用）时跳过删除标记,
	// 否则 filepath.Walk 得到 0 条目会把有效索引批量误删
	if ok, reason := validateScanRoot(rootPath); !ok {
		utils.Warn("根目录不可遍历, 跳过扫描与删除标记以避免误删索引",
			utils.String("root_name", rootName),
			utils.String("root_path", rootPath),
			utils.String("reason", reason))
		result.Error = "根目录不可遍历: " + reason
		return result, nil
	}

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
