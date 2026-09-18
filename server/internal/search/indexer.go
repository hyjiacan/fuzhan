package search

import (
	"fmt"
	"os"
	"time"

	"gorm.io/gorm"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

// ReconcileResult 对齐结果统计
type ReconcileResult struct {
	IndexedTotal   int64 `json:"indexedTotal"`   // DB 中 active 记录总数（补缺失的参考基数）
	MissingIndexed int64 `json:"missingIndexed"` // DB 有而索引无、已补齐的条数
	OrphansTotal   int64 `json:"orphansTotal"`   // 索引中应被清理的孤儿总数
	OrphansRemoved int64 `json:"orphansRemoved"` // 索引有而 DB 无、已清理的条数
}

// listActiveFiles 分页读取 file_records_public 中 status=active 的全部（ID + 文件名）。
func listActiveFiles(db *gorm.DB) ([]IndexEntry, error) {
	const pageSize = 500
	var out []IndexEntry
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
			out = append(out, IndexEntry{ID: int64(r.ID), FileName: r.FileName})
		}
		lastID = records[len(records)-1].ID
		if len(records) < pageSize {
			break
		}
	}
	return out, nil
}

// RebuildFromDB 从数据库索引表全量重建检索索引，并在完成后原子热替换。
//
// 重建全程在影子目录（<dir>.rebuild.<nano>）用独立 writer 进行，不触碰线上索引，
// 因此重建期间的查询始终命中旧索引（完整、一致、零写锁阻塞）。全部写盘成功后，
// 在 rwMu 写锁下关闭旧 writer、删除旧目录、将影子目录重命名为规范目录并重新打开，
// 一次完成切换。
func (s *SearchIndex) RebuildFromDB(db *gorm.DB) error {
	if s == nil || s.writer == nil {
		return fmt.Errorf("索引未打开")
	}
	if db == nil {
		return fmt.Errorf("数据库未初始化")
	}

	entries, err := listActiveFiles(db)
	if err != nil {
		return err
	}

	// 影子目录与规范目录同前缀不同后缀，避免与线上索引竞争文件锁。
	shadowDir := fmt.Sprintf("%s.rebuild.%d", s.dir, time.Now().UnixNano())
	shadowWriter, err := openBlugeWriter(shadowDir, s.seg)
	if err != nil {
		return fmt.Errorf("打开影子重建索引失败: %w", err)
	}

	// 一次性写入全部条目；空库则影子索引为空（等价清空线上索引）。
	if err := writeEntries(shadowWriter, s.seg, entries); err != nil {
		_ = shadowWriter.Close()
		_ = os.RemoveAll(shadowDir)
		return fmt.Errorf("写入影子索引失败: %w", err)
	}
	if err := shadowWriter.Close(); err != nil {
		_ = os.RemoveAll(shadowDir)
		return fmt.Errorf("刷新影子索引失败: %w", err)
	}

	// 原子热替换：写锁持有期仅做近距离换盘，查询全程要么读旧、要么读新。
	s.rwMu.Lock()
	defer s.rwMu.Unlock()

	oldWriter := s.writer
	oldDir := s.dir
	if oldWriter != nil {
		if cerr := oldWriter.Close(); cerr != nil {
			_ = os.RemoveAll(shadowDir)
			return fmt.Errorf("关闭旧索引失败: %w", cerr)
		}
		s.writer = nil
	}
	// 将影子目录提升为规范目录。先移除旧目录内容，再原地改名。
	if err := os.RemoveAll(oldDir); err != nil {
		// 删除失败不致命，继续尝试把影子目录改名为规范目录（若 rename 目标存在会冲突）。
		_ = err
	}
	if err := os.Rename(shadowDir, oldDir); err != nil {
		// 规范目录可能仍存在（删除失败），回退为打开影子目录并以它作规范目录。
		oldDir = shadowDir
		utils.Warn("检索索引热替换目录提升失败，将以影子目录作为索引目录", utils.Err(err))
	}
	nw, oerr := openBlugeWriter(oldDir, s.seg)
	if oerr != nil {
		return fmt.Errorf("热替换后重新打开索引失败: %w", oerr)
	}
	s.writer = nw
	s.dir = oldDir
	// 数据集合已整体更换，词表缓存整表失效，下次读重建。
	s.resetTermCache()
	return nil
}

// resetTermCache 使词表缓存彻底失效（须已持有 rwMu）。
func (s *SearchIndex) resetTermCache() {
	s.termCacheMu.Lock()
	s.lastWriteAt = 0
	s.termCacheLoaded = false
	s.termCacheTerms = nil
	s.termCacheMu.Unlock()
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
