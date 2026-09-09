package index

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"gorm.io/gorm"
)

// ConsistencyChecker 一致性校验器
// 定期比较 FileRecord 索引与文件系统的实际状态,
// 发现不一致时自动修复并记录报告。
type ConsistencyChecker struct {
	db        *gorm.DB
	rootNames map[string]string
}

// NewConsistencyChecker 创建一致性校验器
func NewConsistencyChecker(db *gorm.DB, rootNames map[string]string) *ConsistencyChecker {
	return &ConsistencyChecker{
		db:        db,
		rootNames: rootNames,
	}
}

// RunCheck 执行一次一致性校验
func (c *ConsistencyChecker) RunCheck(ctx context.Context) (*ConsistencyReport, error) {
	startTime := utils.Now()
	utils.Info("一致性校验开始")

	report := &ConsistencyReport{
		CheckedAt: startTime,
	}

	// 按根目录名称排序保证顺序
	names := make([]string, 0, len(c.rootNames))
	for name := range c.rootNames {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, rootName := range names {
		rootPath := c.rootNames[rootName]

		select {
		case <-ctx.Done():
			return report, ctx.Err()
		default:
		}

		if err := c.checkRootDir(ctx, rootName, rootPath, report); err != nil {
			utils.Error("根目录一致性校验失败",
				utils.String("root_name", rootName),
				utils.Err(err))
		}
	}

	report.Duration = time.Since(startTime)
	utils.Info("一致性校验完成",
		utils.Int("missing_in_fs", len(report.MissingInFS)),
		utils.Int("missing_in_db", len(report.MissingInDB)),
		utils.Int("mismatched", len(report.Mismatched)),
		utils.Int("auto_fixed", report.AutoFixed),
		utils.Duration("duration", report.Duration))

	return report, nil
}

// checkRootDir 检查单个根目录的一致性
func (c *ConsistencyChecker) checkRootDir(ctx context.Context, rootName, rootPath string, report *ConsistencyReport) error {
	// 护栏：根不可遍历（junction/符号链接/挂载不可用）时跳过, 否则会把
	// 有效索引经 fixMissingInFS 批量标记为 deleted（数据丢失）
	if ok, reason := validateScanRoot(rootPath); !ok {
		utils.Warn("一致性校验跳过根目录: 根不可遍历",
			utils.String("root_name", rootName),
			utils.String("root_path", rootPath),
			utils.String("reason", reason))
		return nil
	}

	// 1. 加载所有活跃记录
	var dbRecords []models.FileRecordPublic
	if err := c.db.Where("root_name = ? AND status = ?",
		rootName, models.FileStatusActive).Find(&dbRecords).Error; err != nil {
		return err
	}
	dbMap := make(map[string]models.FileRecordPublic, len(dbRecords))
	for _, r := range dbRecords {
		dbMap[r.FilePath] = r
	}
	report.TotalDB += int64(len(dbRecords))

	// 2. 遍历文件系统
	fsSeen := make(map[string]bool)

	walkErr := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			utils.Warn("校验跳过无法访问的文件", utils.String("path", path), utils.Err(err))
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

		fsSeen[relPath] = true
		report.TotalFS++

		dbRec, exists := dbMap[relPath]
		if !exists {
			// 文件在文件系统上但不在 DB 中 -> 新增
			report.MissingInDB = append(report.MissingInDB, ConsistencyDiff{
				RootName:    rootName,
				FilePath:    relPath,
				FullPath:    rootName + relPath,
				FileName:    info.Name(),
				Description: "文件系统存在但索引缺失",
			})
			c.fixMissingInDB(rootName, relPath, info)
			report.AutoFixed++
			return nil
		}

		// 文件同时在文件系统和 DB 中 -> 检查属性
		delete(dbMap, relPath) // 从 dbMap 移除, 剩下的就是 DB 多余

		if info.IsDir() || info.Size() == 0 {
			return nil // 不检查目录和空文件的哈希
		}

		// 检查大小和修改时间是否变化
		var fileSize int64
		if !info.IsDir() {
			fileSize = info.Size()
		}
		modTimeChanged := !dbRec.ModTime.Equal(info.ModTime())
		sizeChanged := dbRec.FileSize != fileSize

		if modTimeChanged || sizeChanged {
			report.Mismatched = append(report.Mismatched, ConsistencyDiff{
				RootName:    rootName,
				FilePath:    relPath,
				FullPath:    rootName + relPath,
				FileName:    info.Name(),
				Description: "文件属性变更（大小或修改时间）",
			})
			c.fixMismatched(rootName, relPath, info)
			report.AutoFixed++
			return nil
		}

		// 检查哈希是否为空
		if dbRec.Xxh3Hash == "" && !info.IsDir() {
			report.Mismatched = append(report.Mismatched, ConsistencyDiff{
				RootName:    rootName,
				FilePath:    relPath,
				FullPath:    rootName + relPath,
				FileName:    info.Name(),
				Description: "哈希值缺失",
			})
			c.fixMismatched(rootName, relPath, info)
			report.AutoFixed++
		}

		return nil
	})

	// 3. 处理 DB 中有但文件系统上不存在的记录
	for path, rec := range dbMap {
		report.MissingInFS = append(report.MissingInFS, ConsistencyDiff{
			RootName:    rootName,
			FilePath:    path,
			FullPath:    rootName + path,
			FileName:    rec.FileName,
			Description: "索引存在但文件系统已删除",
		})
		c.fixMissingInFS(rec)
		report.AutoFixed++
	}

	return walkErr
}

// fixMissingInDB 修复文件系统存在但索引缺失的情况
func (c *ConsistencyChecker) fixMissingInDB(rootName, relPath string, info os.FileInfo) {
	now := utils.Now()
	rec := models.FileRecordPublic{
		FileRecordBase: models.FileRecordBase{
			FileName:     info.Name(),
			FilePath:     relPath,
			RootName:     rootName,
			FullPath:     "/" + rootName + relPath,
			FileSize:     fileSizeFromInfo(info),
			IsDir:        info.IsDir(),
			ModTime:      info.ModTime(),
			Status:       models.FileStatusActive,
			LastSyncedAt: now,
		},
	}

	if err := c.db.Create(&rec).Error; err != nil {
		utils.Error("自动修复新增索引记录失败",
			utils.String("path", relPath),
			utils.Err(err))
	} else {
		utils.Warn("自动修复: 新增索引记录",
			utils.String("path", relPath),
			utils.String("root", rootName))
	}
}

// fixMissingInFS 修复 DB 存在但文件系统已删除的情况
func (c *ConsistencyChecker) fixMissingInFS(rec models.FileRecordPublic) {
	now := utils.Now()
	if err := c.db.Model(&rec).Updates(map[string]interface{}{
		"status":     models.FileStatusDeleted,
		"updated_at": now,
	}).Error; err != nil {
		utils.Error("自动修复标记删除失败",
			utils.String("path", rec.FilePath),
			utils.Err(err))
	} else {
		utils.Warn("自动修复: 标记索引为已删除",
			utils.String("path", rec.FilePath),
			utils.String("root", rec.RootName))
	}
}

// fixMismatched 修复属性不匹配的记录（不计算 hash，由 HashWorker 处理）
func (c *ConsistencyChecker) fixMismatched(rootName, relPath string, info os.FileInfo) {
	now := utils.Now()
	if err := c.db.Model(&models.FileRecordPublic{}).
		Where("root_name = ? AND file_path = ?", rootName, relPath).
		Updates(map[string]interface{}{
			"file_size":      fileSizeFromInfo(info),
			"mod_time":       info.ModTime(),
			"last_synced_at": now,
			"updated_at":     now,
		}).Error; err != nil {
		utils.Error("自动修复更新记录失败",
			utils.String("path", relPath),
			utils.Err(err))
	} else {
		utils.Warn("自动修复: 更新文件属性",
			utils.String("path", relPath),
			utils.String("root", rootName))
	}
}
