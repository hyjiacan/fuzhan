package search

import (
    "fmt"

    "gorm.io/gorm"

    "fuzhan/internal/models"
)

// RebuildFromDB 从数据库索引表全量构建检索索引。
// 读取 file_records_public 中 status=active 的全部记录（ID + 文件名），
// 分批调用 IndexBatch（按 ID 覆盖，幂等），可用于启动时的全量回填。
func (s *SearchIndex) RebuildFromDB(db *gorm.DB) error {
    if s.writer == nil {
        return fmt.Errorf("索引未打开")
    }
    if db == nil {
        return fmt.Errorf("数据库未初始化")
    }

    const pageSize = 500
    var lastID uint
    for {
        var records []models.FileRecordPublic
        if err := db.Model(&models.FileRecordPublic{}).
            Where("status = ? AND id > ?", models.FileStatusActive, lastID).
            Order("id").
            Limit(pageSize).
            Find(&records).Error; err != nil {
            return fmt.Errorf("读取文件记录失败: %w", err)
        }
        if len(records) == 0 {
            break
        }

        entries := make([]IndexEntry, 0, len(records))
        for _, r := range records {
            entries = append(entries, IndexEntry{ID: int64(r.ID), FileName: r.FileName})
        }
        if err := s.IndexBatch(entries); err != nil {
            return fmt.Errorf("批量写入检索索引失败: %w", err)
        }

        lastID = records[len(records)-1].ID
        if len(records) < pageSize {
            break
        }
    }
    return nil
}