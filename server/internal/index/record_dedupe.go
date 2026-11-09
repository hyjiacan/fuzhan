package index

import (
	"time"

	"gorm.io/gorm"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

// DedupePublicFileRecords 收敛公共文件索引表中同一 (root_name, file_path) 的多条活跃记录。
//
// 背景：SyncFile/扫描/一致性修复等多个写入方并发时，可能给同一路径各插一条；
// 重复行会让文件列表出现多条相同记录，也会让覆盖判定命中不属于自己的那条。
//
// 幂等：启动时与每日一致性校验后调用；只处理存在重复的分组，无重复时零写入。
// 规则：保留 id 最小的一条（与 SyncFile 的 First() 选取顺序一致），把其余行的
// uploader_ip/xxh3_hash/备注/下载次数/哈希状态合并过来，重定向下游引用后软删多余行。
func DedupePublicFileRecords(db *gorm.DB) {
	if db == nil || !db.Migrator().HasTable(&models.FileRecordPublic{}) {
		return
	}

	type dupGroup struct {
		RootName string
		FilePath string
	}
	var groups []dupGroup
	err := db.Model(&models.FileRecordPublic{}).
		Select("root_name, file_path").
		Where("status = ? AND deleted_at IS NULL", models.FileStatusActive).
		Group("root_name, file_path").
		Having("COUNT(*) > 1").
		Scan(&groups).Error
	if err != nil {
		utils.Warn("查询公共文件索引重复记录失败", utils.Err(err))
		return
	}
	if len(groups) == 0 {
		return
	}

	var dedupedGroups, removedRows int
	for _, g := range groups {
		release := lockWrite()
		_, removed, cerr := collapsePublicRecordDuplicates(db, g.RootName, g.FilePath)
		release()
		if cerr != nil {
			utils.Warn("清理公共文件索引重复记录失败",
				utils.String("root", g.RootName),
				utils.String("path", g.FilePath),
				utils.Err(cerr))
			continue
		}
		dedupedGroups++
		removedRows += removed
	}
	utils.Info("公共文件索引重复记录清理完成",
		utils.Int("groups", dedupedGroups),
		utils.Int("removed", removedRows))
}

// collapsePublicRecordDuplicates 合并同一 (rootName, filePath) 的多条活跃记录，
// 返回保留行 ID 与被软删的行数。调用方需自行持有 lockWrite（本函数不重复加锁，
// 避免与 SyncFile 内的同一把锁形成自死锁）。
func collapsePublicRecordDuplicates(db *gorm.DB, rootName, filePath string) (uint, int, error) {
	var recs []models.FileRecordPublic
	if err := db.Where("root_name = ? AND file_path = ?", rootName, filePath).
		Order("id ASC").Find(&recs).Error; err != nil {
		return 0, 0, err
	}
	if len(recs) == 0 {
		return 0, 0, nil
	}
	if len(recs) == 1 {
		return recs[0].ID, 0, nil
	}

	keeper := recs[0]
	dupIDs := make([]uint, 0, len(recs)-1)
	updates := map[string]interface{}{}

	var downloadCount int64
	var lastSyncedAt time.Time
	mergedHashStatus := keeper.HashStatus

	for _, r := range recs[1:] {
		dupIDs = append(dupIDs, r.ID)
		downloadCount += r.DownloadCount
		if r.LastSyncedAt.After(lastSyncedAt) {
			lastSyncedAt = r.LastSyncedAt
		}
		if r.HashStatus == "done" {
			mergedHashStatus = "done"
		}
		// 空字段用重复行补齐：上传 IP、哈希、备注都可能只落在其中一条上
		if keeper.UploaderIP == "" && r.UploaderIP != "" {
			updates["uploader_ip"] = r.UploaderIP
			keeper.UploaderIP = r.UploaderIP
		}
		if keeper.Xxh3Hash == "" && r.Xxh3Hash != "" {
			updates["xxh3_hash"] = r.Xxh3Hash
			keeper.Xxh3Hash = r.Xxh3Hash
		}
		if keeper.Notes == "" && r.Notes != "" {
			updates["notes"] = r.Notes
			keeper.Notes = r.Notes
		}
	}

	if total := keeper.DownloadCount + downloadCount; total != keeper.DownloadCount {
		updates["download_count"] = total
	}
	if lastSyncedAt.After(keeper.LastSyncedAt) {
		updates["last_synced_at"] = lastSyncedAt
	}
	if mergedHashStatus != keeper.HashStatus {
		updates["hash_status"] = mergedHashStatus
	}
	// 合并后已是 done，清理旧的失败信息，避免哈希任务误判
	if mergedHashStatus == "done" && (keeper.HashError != "" || keeper.HashRetries != 0) {
		updates["hash_error"] = ""
		updates["hash_retries"] = 0
	}

	// 下游引用表可能不存在（精简部署/测试库）：仅重定向存在的表
	hasOperationTable := db.Migrator().HasTable(&models.OperationRecord{})
	hasDependencyTable := db.Migrator().HasTable(&models.FileDependency{})

	err := db.Transaction(func(tx *gorm.DB) error {
		if len(updates) > 0 {
			if err := tx.Model(&models.FileRecordPublic{}).
				Where("id = ?", keeper.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		// 历史记录引用一并指向保留行，避免上传记录/依赖关系断链
		if hasOperationTable {
			if err := tx.Model(&models.OperationRecord{}).
				Where("file_record_id IN ?", dupIDs).
				UpdateColumn("file_record_id", keeper.ID).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.OperationRecord{}).
				Where("source_type = ? AND source_id IN ?", models.SourceTypePublic, dupIDs).
				UpdateColumn("source_id", keeper.ID).Error; err != nil {
				return err
			}
		}
		if hasDependencyTable {
			if err := tx.Model(&models.FileDependency{}).
				Where("file_record_id IN ?", dupIDs).
				UpdateColumn("file_record_id", keeper.ID).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.FileDependency{}).
				Where("depends_on_id IN ?", dupIDs).
				UpdateColumn("depends_on_id", keeper.ID).Error; err != nil {
				return err
			}
		}
		// 软删：必须写 deleted_at，SyncFile 查询不按 status 过滤，只置 status 会被同步"复活"
		now := time.Now()
		return tx.Model(&models.FileRecordPublic{}).
			Where("id IN ?", dupIDs).
			UpdateColumns(map[string]interface{}{
				"status":     string(models.FileStatusDeleted),
				"deleted_at": now,
				"updated_at": now,
			}).Error
	})
	if err != nil {
		return keeper.ID, 0, err
	}
	return keeper.ID, len(dupIDs), nil
}
