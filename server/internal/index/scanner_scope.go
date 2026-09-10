package index

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

	// 已索引的私有文件 code 集合，避免每轮扫描重复插入导致索引膨胀
	existingKeys := make(map[string]struct{})
	var existingRows []struct {
		FilePath string
	}
	if err := s.db.Model(&models.FileRecordPrivate{}).
		Where("root_name = ? AND status = ?", "private", models.FileStatusActive).
		Select("file_path").Find(&existingRows).Error; err == nil {
		for _, er := range existingRows {
			existingKeys[er.FilePath] = struct{}{}
		}
	}

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

			if _, exists := existingKeys[codeEntry.Name()]; exists {
				continue
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
