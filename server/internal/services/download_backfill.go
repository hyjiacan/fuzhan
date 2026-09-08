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
