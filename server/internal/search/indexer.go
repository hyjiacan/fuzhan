package search

import (
	"fmt"

	"gorm.io/gorm"

	"fuzhan/internal/models"
)

// ReconcileResult 对齐结果统计
type ReconcileResult struct {
	IndexedTotal   int64 `json:"indexedTotal"`   // DB 中 active 记录总数（补缺失的参考基数）
	MissingIndexed int64 `json:"missingIndexed"` // DB 有而索引无、已补齐的条数
	OrphansTotal   int64 `json:"orphansTotal"`   // 索引中应被清理的孤儿总数
	OrphansRemoved int64 `json:"orphansRemoved"` // 索引有而 DB 无、已清理的条数
}

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

// ReconcileIndex 将检索索引与索引表对齐（本质是两集合求差，而非全量重建）：
//   - 补齐缺失：索引表有、索引无的 fileID → IndexFile（幂等覆盖）
//   - 清理孤儿：索引有、索引表无的 fileID → Delete
//
// progress 可选，用于上报任务进度（done/total）。返回差异统计。
func (s *SearchIndex) ReconcileIndex(db *gorm.DB, progress func(done, total int64)) (*ReconcileResult, error) {
	if s.writer == nil {
		return nil, fmt.Errorf("索引未打开")
	}
	if db == nil {
		return nil, fmt.Errorf("数据库未初始化")
	}

	// 1. 读取索引表全部 active 记录（id + 文件名）
	const pageSize = 500
	dbFiles := make(map[int64]string)
	var lastID uint
	for {
		var records []models.FileRecordPublic
		if err := db.Model(&models.FileRecordPublic{}).
			Where("status = ? AND id > ?", models.FileStatusActive, lastID).
			Order("id").
			Limit(pageSize).
			Find(&records).Error; err != nil {
			return nil, fmt.Errorf("读取文件记录失败: %w", err)
		}
		if len(records) == 0 {
			break
		}
		for _, r := range records {
			dbFiles[int64(r.ID)] = r.FileName
		}
		lastID = records[len(records)-1].ID
		if len(records) < pageSize {
			break
		}
	}

	// 2. 枚举检索索引中全部 fileID
	indexIDs, err := s.AllFileIDs()
	if err != nil {
		return nil, fmt.Errorf("枚举检索索引失败: %w", err)
	}
	indexSet := make(map[int64]struct{}, len(indexIDs))
	for _, id := range indexIDs {
		indexSet[id] = struct{}{}
	}

	// 3. 求差
	result := &ReconcileResult{
		IndexedTotal: int64(len(dbFiles)),
		OrphansTotal: int64(len(indexIDs) - countIntersection(dbFiles, indexSet)),
	}
	missing := make([]IndexEntry, 0)
	orphans := make([]int64, 0)
	for id, name := range dbFiles {
		if _, ok := indexSet[id]; !ok {
			missing = append(missing, IndexEntry{ID: id, FileName: name})
		}
	}
	for id := range indexSet {
		if _, ok := dbFiles[id]; !ok {
			orphans = append(orphans, id)
		}
	}
	result.MissingIndexed = int64(len(missing))
	result.OrphansRemoved = int64(len(orphans))

	totalSteps := int64(len(missing)) + int64(len(orphans))
	doneSteps := int64(0)
	if progress == nil {
		progress = func(done, total int64) {}
	}

	// 4. 分批补齐缺失 / 清理孤儿
	for i := 0; i < len(missing); i += pageSize {
		end := i + pageSize
		if end > len(missing) {
			end = len(missing)
		}
		if err := s.IndexBatch(missing[i:end]); err != nil {
			return result, fmt.Errorf("补齐缺失索引失败: %w", err)
		}
		doneSteps += int64(end - i)
		progress(doneSteps, totalSteps)
	}
	for i := 0; i < len(orphans); i += pageSize {
		end := i + pageSize
		if end > len(orphans) {
			end = len(orphans)
		}
		if err := s.DeleteBatch(orphans[i:end]); err != nil {
			return result, fmt.Errorf("清理孤儿索引失败: %w", err)
		}
		doneSteps += int64(end - i)
		progress(doneSteps, totalSteps)
	}

	if totalSteps > 0 {
		progress(totalSteps, totalSteps)
	}
	return result, nil
}

// countIntersection 统计两集合交集元素个数
func countIntersection(m map[int64]string, s map[int64]struct{}) int {
	n := 0
	for k := range m {
		if _, ok := s[k]; ok {
			n++
		}
	}
	return n
}
