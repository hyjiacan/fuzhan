package index

import (
	"encoding/json"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

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
