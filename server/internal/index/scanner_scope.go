package index

import (
	"context"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

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
	lastFlush := time.Now()

	// 已索引的临时文件 code 集合，避免每轮扫描重复插入导致索引膨胀
	existingKeys := make(map[string]struct{})
	var existingRows []struct {
		FilePath string
	}
	if err := s.db.Model(&models.FileRecordTemp{}).
		Where("root_name = ? AND status = ?", "temp", models.FileStatusActive).
		Select("file_path").Find(&existingRows).Error; err == nil {
		for _, er := range existingRows {
			existingKeys[er.FilePath] = struct{}{}
		}
	}

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
		if _, exists := existingKeys[hash]; exists {
			continue
		}
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

		// 满批或达到时间兜底间隔即落库
		if len(batch) >= s.batchSize || time.Since(lastFlush) >= scanBatchFlushInterval {
			release := lockWrite()
			err := s.db.CreateInBatches(batch, s.batchSize).Error
			release()
			if err != nil {
				utils.Error("批量写入 file_records_temp 失败", utils.Err(err))
			}
			batch = batch[:0]
			lastFlush = time.Now()
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
