package index

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/storage"
	"fuzhan/internal/utils"
	"gorm.io/gorm"
)

// ConsistencyChecker 一致性校验器
// 定期比较 FileRecord 索引与文件系统的实际状态,
// 发现不一致时自动修复并记录报告。
type ConsistencyChecker struct {
	db        *gorm.DB
	rootNames map[string]string
	storages  map[string]storage.Storage // rootName → Storage（远端后端注入）
}

// NewConsistencyChecker 创建一致性校验器
func NewConsistencyChecker(db *gorm.DB, rootNames map[string]string) *ConsistencyChecker {
	return &ConsistencyChecker{
		db:        db,
		rootNames: rootNames,
	}
}

// SetStorages 注入公开多根目录的存储后端映射（rootName → Storage）。
func (c *ConsistencyChecker) SetStorages(storages map[string]storage.Storage) {
	c.storages = storages
}

// storageFor 返回指定根目录的存储后端。远端后端（非 local）时 remote=true，
// 校验将通过 Storage 接口遍历；本地后端（含未注入）保持 rootNames 物理路径语义。
// 与 Scanner.storageFor 语义一致。
func (c *ConsistencyChecker) storageFor(rootName string) (storage.Storage, bool) {
	st, ok := c.storages[rootName]
	if !ok || st == nil || st.Kind() == "local" {
		return nil, false
	}
	return st, true
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
	st, remote := c.storageFor(rootName)
	if !remote {
		// 护栏：根不可遍历（junction/符号链接/挂载不可用）时跳过, 否则会把
		// 有效索引经 fixMissingInFS 批量标记为 deleted（数据丢失）。
		// 远端后端根为虚拟前缀（无真实磁盘路径），跳过 Lstat 护栏。
		if ok, reason := validateScanRoot(rootPath); !ok {
			utils.Warn("一致性校验跳过根目录: 根不可遍历",
				utils.String("root_name", rootName),
				utils.String("root_path", rootPath),
				utils.String("reason", reason))
			return nil
		}
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

	now := utils.Now()
	// 文件系统存在但索引缺失的记录 → 收集后批量新增，避免大目录首次校验时逐条写库
	var pendingCreate []models.FileRecordPublic
	// 索引存在但文件系统已删除的记录 ID → 收集后批量标记删除
	var missingInFSIDs []uint

	// 2. 遍历文件系统
	fsSeen := make(map[string]bool)

	// handleEntry 处理一个遍历到的目录/文件项：对比 DB 记录、收集差异或修复。
	// 本地（filepath.Walk）与远端（walkStorage）两条遍历路径共用此处理逻辑。
	handleEntry := func(relPath string, info os.FileInfo) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
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
			pendingCreate = append(pendingCreate, models.FileRecordPublic{
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
			})
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
	}

	var walkErr error
	if remote {
		// 远端后端：无真实磁盘，用 Storage.List 递归遍历（替代 filepath.Walk）。
		entryCh := make(chan walkEntry, 256)
		walkDone := make(chan error, 1)
		go func() {
			walkDone <- walkStorage(ctx, st, "/", entryCh)
		}()
		for e := range entryCh {
			herr := handleEntry(e.relPath, e.info)
			if herr != nil {
				walkErr = herr
				// 取消后 walkStorage 会关闭 entryCh, 消费端退出
			}
		}
		if werr := <-walkDone; walkErr == nil {
			walkErr = werr
		}
	} else {
		walkErr = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				utils.Warn("校验跳过无法访问的文件", utils.String("path", path), utils.Err(err))
				return nil
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
			return handleEntry(relPath, info)
		})
	}

	// 3. 批量新增文件系统存在但索引缺失的记录
	if len(pendingCreate) > 0 {
		release := lockWrite()
		if err := c.db.CreateInBatches(pendingCreate, 200).Error; err != nil {
			utils.Error("自动修复批量新增索引记录失败",
				utils.String("root", rootName),
				utils.Int("count", len(pendingCreate)),
				utils.Err(err))
		}
		release()
	}

	// 4. 处理 DB 中有但文件系统上不存在的记录
	for path, rec := range dbMap {
		report.MissingInFS = append(report.MissingInFS, ConsistencyDiff{
			RootName:    rootName,
			FilePath:    path,
			FullPath:    rootName + path,
			FileName:    rec.FileName,
			Description: "索引存在但文件系统已删除",
		})
		missingInFSIDs = append(missingInFSIDs, rec.ID)
		report.AutoFixed++
	}

	// 5. 批量标记索引存在但文件系统已删除的记录
	if len(missingInFSIDs) > 0 {
		release := lockWrite()
		if err := c.db.Model(&models.FileRecordPublic{}).
			Where("id IN ?", missingInFSIDs).
			Updates(map[string]interface{}{
				"status":     models.FileStatusDeleted,
				"updated_at": now,
			}).Error; err != nil {
			utils.Error("自动修复批量标记删除失败",
				utils.String("root", rootName),
				utils.Int("count", len(missingInFSIDs)),
				utils.Err(err))
		}
		release()
	}

	return walkErr
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
