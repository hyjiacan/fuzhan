package services

import (
	"gorm.io/gorm"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

// BackfillPublicDownloadCounts 从下载记录(operation_records)统计各公共文件的下载次数并回写到文件索引表。
// 作为升级/启动时的一次性回填：仅处理 download_count 仍为 0 的记录，幂等且避免覆盖真实增量，
// 也避免每次启动都做全量重算。下载记录会实时递增，因此未回填过的文件其数量会持续准确。
func BackfillPublicDownloadCounts(db *gorm.DB) {
	if db == nil {
		return
	}
	if !db.Migrator().HasTable(&models.OperationRecord{}) || !db.Migrator().HasTable(&models.FileRecordPublic{}) {
		return
	}

	const stmt = `UPDATE file_records_public
        SET download_count = (
            SELECT COUNT(*)
            FROM operation_records
            WHERE operation_records.full_path = file_records_public.full_path
              AND operation_records.action IN (?, ?)
              AND operation_records.deleted_at IS NULL
        )
        WHERE status = ? AND is_dir = ? AND download_count = 0`

	result := db.Exec(stmt, "download", "download-by-hash", models.FileStatusActive, false)
	if result.Error != nil {
		utils.Warn("回填公共文件下载次数失败", utils.Err(result.Error))
		return
	}
	utils.Info("公共文件下载次数回填完成", utils.Int64("affected", result.RowsAffected))
}

// BackfillFileRecordIDs 回填历史操作记录的 file_record_id：按路径反查公共文件索引记录 ID。
// 仅处理未关联（file_record_id=0）且为文件操作（upload/download/download-by-hash）的记录，
// 分批执行避免一次性大事务锁表。移动/重命名前的历史数据由此建立身份关联，
// 使最近/热门列表能在文件移动后仍定位到当前路径。
func BackfillFileRecordIDs(db *gorm.DB) {
	if db == nil {
		return
	}
	if !db.Migrator().HasTable(&models.OperationRecord{}) || !db.Migrator().HasTable(&models.FileRecordPublic{}) {
		return
	}

	const batchSize = 500
	var affected int64
	actions := []string{"upload", "download", "download-by-hash"}
	// SQLite 的 UPDATE 语句不支持 LIMIT，改用先取一批待回填记录 id、再按 id 分批更新的方式
	for {
		var batchIDs []uint
		if err := db.Table("operation_records").
			Where("file_record_id = ? AND action IN ? AND deleted_at IS NULL", 0, actions).
			Limit(batchSize).
			Pluck("id", &batchIDs).Error; err != nil {
			utils.Warn("查询待回填操作记录失败", utils.Err(err))
			return
		}
		if len(batchIDs) == 0 {
			break
		}
		result := db.Exec(`UPDATE operation_records
			SET file_record_id = (
				SELECT MIN(frp.id) FROM file_records_public frp
				WHERE frp.status = ? AND frp.deleted_at IS NULL
				  AND frp.root_name = operation_records.root_name
				  AND (frp.full_path = operation_records.full_path
				       OR frp.full_path = ('/' || operation_records.full_path))
			)
			WHERE id IN ?`,
			models.FileStatusActive, batchIDs)
		if result.Error != nil {
			utils.Warn("回填操作记录 file_record_id 失败", utils.Err(result.Error))
			return
		}
		affected += result.RowsAffected
		if len(batchIDs) < batchSize {
			break
		}
	}
	utils.Info("操作记录 file_record_id 回填完成", utils.Int64("affected", affected))
}
