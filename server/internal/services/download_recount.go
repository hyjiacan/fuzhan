package services

import (
	"gorm.io/gorm"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

// 下载次数重算：把索引表上的冗余计数列，重新推导为"记录集上的计数"。
//
// 背景：file_records_public.download_count 是为读性能保留的冗余列（列表/搜索逐文件返回并支持排序）。
// 但冗余带来双真源：删除 operation_records 不会回退该列，导致同一文件在"列表下载次数"与
// "热门/行为分析（实时聚合记录）"之间长期不一致。
//
// 解决：把操作记录作为唯一真源，计数列降级为可重算的派生值。
// 计数口径与写入侧完全一致——dl_counted = 1（公开来源、非预览、非断点续传/分段延续）且未软删除。
// 删除记录后调用重算即可精确回退；重算是幂等的（重复执行结果不变），不依赖"删除前记得递减"。

// RecountPublicDownloadCounts 按给定完整路径集合重算公共文件下载次数并回写索引表。
// paths 为空时不做任何事；全量对齐请用 RecountAllPublicDownloadCounts。
func RecountPublicDownloadCounts(db *gorm.DB, paths []string) {
	if db == nil {
		return
	}
	uniq := dedupNonEmptyStrings(paths)
	if len(uniq) == 0 {
		return
	}

	table := models.FileRecordPublic{}.TableName()
	opTable := models.OperationRecord{}.TableName()

	for i := 0; i < len(uniq); i += downloadCountBatchSize {
		end := i + downloadCountBatchSize
		if end > len(uniq) {
			end = len(uniq)
		}
		batch := uniq[i:end]

		sql := "UPDATE " + table + " SET download_count = (" +
			"SELECT COUNT(*) FROM " + opTable +
			" WHERE " + opTable + ".dl_counted = 1" +
			" AND " + opTable + ".deleted_at IS NULL" +
			" AND " + opTable + ".full_path = " + table + ".full_path)" +
			" WHERE " + table + ".status = ? AND " + table + ".is_dir = ? AND " + table + ".full_path IN (?)"
		if err := db.Exec(sql, models.FileStatusActive, false, batch).Error; err != nil {
			utils.Error("重算公共文件下载次数失败", utils.Int("rows", len(batch)), utils.Err(err))
		}
	}
}

// RecountAllPublicDownloadCounts 全量对齐：把所有活跃公共文件的 download_count 重算为记录集推导值。
// 用于下载计数口径变更后的一次性对齐（幂等）。分两步，避免逐文件做相关子查询：
//  1. 记录集已无计数记录的文件清零；
//  2. 单趟按 full_path 分组统计（命中 (dl_counted, full_path) 索引），再分批回写。
func RecountAllPublicDownloadCounts(db *gorm.DB) {
	if db == nil {
		return
	}
	if !db.Migrator().HasTable(&models.OperationRecord{}) || !db.Migrator().HasTable(&models.FileRecordPublic{}) {
		return
	}

	table := models.FileRecordPublic{}.TableName()
	opTable := models.OperationRecord{}.TableName()

	// 1) 清零：无计数记录的文件不应保留历史冗余值（full_path 为空的记录不参与 NOT IN，避免三值逻辑失效）
	zeroSQL := "UPDATE " + table + " SET download_count = 0" +
		" WHERE " + table + ".status = ? AND " + table + ".is_dir = ? AND " + table + ".download_count <> 0" +
		" AND " + table + ".full_path NOT IN (" +
		"SELECT full_path FROM " + opTable +
		" WHERE dl_counted = 1 AND deleted_at IS NULL AND full_path IS NOT NULL AND full_path <> ''" +
		" GROUP BY full_path)"
	if err := db.Exec(zeroSQL, models.FileStatusActive, false).Error; err != nil {
		utils.Warn("清零无下载记录文件的下载次数失败", utils.Err(err))
		return
	}

	// 2) 按路径分组回写
	type pathCount struct {
		FullPath string
		N        int64
	}
	var rows []pathCount
	if err := db.Table(opTable).
		Select("full_path, COUNT(*) AS n").
		Where("dl_counted = 1 AND deleted_at IS NULL AND full_path IS NOT NULL AND full_path <> ''").
		Group("full_path").
		Scan(&rows).Error; err != nil {
		utils.Warn("统计各文件下载次数失败", utils.Err(err))
		return
	}
	if len(rows) == 0 {
		utils.Info("公共文件下载次数全量对齐完成", utils.Int("paths", 0))
		return
	}

	paths := make([]string, 0, len(rows))
	counts := make([]int64, 0, len(rows))
	for _, r := range rows {
		paths = append(paths, r.FullPath)
		counts = append(counts, r.N)
	}

	for i := 0; i < len(paths); i += downloadCountBatchSize {
		end := i + downloadCountBatchSize
		if end > len(paths) {
			end = len(paths)
		}
		batchPaths := paths[i:end]
		batchCounts := counts[i:end]

		sql := "UPDATE " + table + " SET download_count = CASE full_path"
		args := make([]interface{}, 0, len(batchPaths)*2+3)
		for j, p := range batchPaths {
			sql += " WHEN ? THEN ?"
			args = append(args, p, batchCounts[j])
		}
		sql += " END WHERE status = ? AND is_dir = ? AND full_path IN (?)"
		args = append(args, models.FileStatusActive, false, batchPaths)
		if err := db.Exec(sql, args...).Error; err != nil {
			utils.Error("批量回写下载次数失败", utils.Int("rows", len(batchPaths)), utils.Err(err))
		}
	}
	utils.Info("公共文件下载次数全量对齐完成", utils.Int("paths", len(paths)))
}

// RecountTempDownloadCounts 按给定临时文件 ID 集合重算 temp_files.download_count。
// 口径：该临时文件成功下载的记录数（source_type=temp、status=success 且未软删除）。
// 临时下载每成功一次既落一条记录、又累加一次计数，两者一一对应。
func RecountTempDownloadCounts(db *gorm.DB, sourceIDs []uint) {
	if db == nil {
		return
	}
	uniq := dedupNonEmptyUints(sourceIDs)
	if len(uniq) == 0 {
		return
	}

	table := models.TempFile{}.TableName()
	opTable := models.OperationRecord{}.TableName()

	for i := 0; i < len(uniq); i += downloadCountBatchSize {
		end := i + downloadCountBatchSize
		if end > len(uniq) {
			end = len(uniq)
		}
		batch := uniq[i:end]

		sql := "UPDATE " + table + " SET download_count = (" +
			"SELECT COUNT(*) FROM " + opTable +
			" WHERE " + opTable + ".source_type = ?" +
			" AND " + opTable + ".status = ?" +
			" AND " + opTable + ".deleted_at IS NULL" +
			" AND " + opTable + ".source_id = " + table + ".id)" +
			" WHERE " + table + ".id IN (?)"
		if err := db.Exec(sql, models.SourceTypeTemp, models.RecordStatusSuccess, batch).Error; err != nil {
			utils.Error("重算临时文件下载次数失败", utils.Int("rows", len(batch)), utils.Err(err))
		}
	}
}

// dedupNonEmptyStrings 去重并丢弃空串
func dedupNonEmptyStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// dedupNonEmptyUints 去重并丢弃 0
func dedupNonEmptyUints(in []uint) []uint {
	seen := make(map[uint]struct{}, len(in))
	out := make([]uint, 0, len(in))
	for _, v := range in {
		if v == 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
