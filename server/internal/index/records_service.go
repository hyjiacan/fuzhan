package index

import (
	"fmt"
	"os"
	"path/filepath"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"gorm.io/gorm"
)

// ListRecords 分页查询文件记录
func (s *Service) ListRecords(query ListRecordsQuery) ([]models.FileRecordPublic, int64, error) {
	query.Normalize()

	// 根据 table 参数选择查询的表
	tableName := "file_records_public"
	switch query.Table {
	case "file_records_public":
		tableName = "file_records_public"
	case "file_records_temp":
		tableName = "file_records_temp"
	case "file_records_private":
		tableName = "file_records_private"
	}
	dbQuery := s.db.Table(tableName)

	// 根目录过滤
	if query.RootName != "" {
		dbQuery = dbQuery.Where("root_name = ?", query.RootName)
	}

	// 所有者过滤
	if query.OwnerID != "" {
		dbQuery = dbQuery.Where("owner_id = ?", query.OwnerID)
	}

	// 状态过滤
	switch query.Status {
	case "active":
		dbQuery = dbQuery.Where("status = ?", models.FileStatusActive)
	case "deleted":
		dbQuery = dbQuery.Where("status = ?", models.FileStatusDeleted)
		// "all" 不添加状态过滤
	}

	// 文件名搜索
	if query.Query != "" {
		dbQuery = dbQuery.Where("file_name LIKE ?", "%"+query.Query+"%")
	}

	// 总数
	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("查询总数失败: %w", err)
	}

	// 排序
	sortField := map[string]string{
		"name": "file_name",
		"size": "file_size",
		"time": "mod_time",
	}
	orderField := "file_name"
	if f, ok := sortField[query.Sort]; ok {
		orderField = f
	}
	orderDir := "ASC"
	if query.Order == "desc" {
		orderDir = "DESC"
	}
	orderClause := fmt.Sprintf("%s %s", orderField, orderDir)

	// 分页
	offset := (query.Page - 1) * query.PageSize
	var records []models.FileRecordPublic
	if err := dbQuery.Order(orderClause).
		Offset(offset).Limit(query.PageSize).
		Find(&records).Error; err != nil {
		return nil, 0, fmt.Errorf("查询记录失败: %w", err)
	}

	if records == nil {
		records = make([]models.FileRecordPublic, 0)
	}

	return records, total, nil
}

// GetStats 获取文件统计
func (s *Service) GetStats() (*RecordStatsResponse, error) {
	// 一次分组查询同时统计总数、目录数、文件大小总和，避免多次全表扫描
	type statRow struct {
		IsDir bool
		Cnt   int64
		Size  int64
	}
	var rows []statRow
	if err := s.db.Model(&models.FileRecordPublic{}).
		Select("is_dir, COUNT(*) AS cnt, COALESCE(SUM(file_size), 0) AS size").
		Where("status = ?", models.FileStatusActive).
		Group("is_dir").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	var activeCount, dirCount, sizeSum int64
	for _, r := range rows {
		activeCount += r.Cnt
		if r.IsDir {
			dirCount = r.Cnt
		} else {
			sizeSum = r.Size
		}
	}

	var deletedCount int64
	if err := s.db.Model(&models.FileRecordPublic{}).
		Where("status = ?", models.FileStatusDeleted).
		Count(&deletedCount).Error; err != nil {
		return nil, err
	}

	// 重复文件组数（有重复哈希值的组）
	var dupGroups int64
	if err := s.db.Raw(`
        SELECT COUNT(*) FROM (
            SELECT xxh3_hash FROM file_records_public
            WHERE status = ? AND xxh3_hash != '' AND file_size > 0
            GROUP BY xxh3_hash HAVING COUNT(*) > 1
        ) dups
    `, string(models.FileStatusActive)).Scan(&dupGroups).Error; err != nil {
		return nil, err
	}

	return &RecordStatsResponse{
		TotalFiles:      activeCount,
		TotalSize:       sizeSum,
		TotalDirs:       dirCount,
		DeletedFiles:    deletedCount,
		ActiveFiles:     activeCount,
		DuplicateGroups: dupGroups,
		TotalRootDirs:   len(s.rootNames),
	}, nil
}

// ListDuplicateGroups 查询重复文件分组
func (s *Service) ListDuplicateGroups(query DuplicateQuery) ([]DuplicateGroup, int64, error) {
	query.Normalize()

	// 分组总数（返回系统中所有重复分组总数，不含 MinSize 过滤；用 GORM 子查询而非 raw SQL，
	// 避免读取到过期快照导致处理重复项后 total 不随 groups 实时减少）
	totalDup := s.db.Model(&models.FileRecordPublic{}).
		Select("1 AS one").
		Where("status = ? AND xxh3_hash != '' AND is_dir = ? AND file_size > 0",
			string(models.FileStatusActive), false).
		Group("xxh3_hash").Having("COUNT(*) > 1")
	var total int64
	if err := s.db.Table("(?) AS t", totalDup).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("查询重复分组总数失败: %w", err)
	}

	// 分组排序
	orderField := "size_sum"
	orderDir := "DESC"
	if query.Sort == "count" {
		orderField = "count"
	} else if query.Sort == "hash" {
		orderField = "xxh3_hash"
	}
	if query.Order == "asc" {
		orderDir = "ASC"
	}

	// 先查询有重复的哈希列表（当 xxh3_hash 不为空且不统计目录）
	var hashCounts []struct {
		Xxh3Hash string
		Count    int64
		SizeSum  int64
	}
	countQuery := s.db.Model(&models.FileRecordPublic{}).
		Select("xxh3_hash, COUNT(*) as count, SUM(file_size) as size_sum").
		Where("status = ? AND xxh3_hash != '' AND is_dir = ? AND file_size > 0",
			string(models.FileStatusActive), false)

	if query.MinSize > 0 {
		countQuery = countQuery.Where("file_size >= ?", query.MinSize)
	}

	offset := (query.Page - 1) * query.PageSize
	if err := countQuery.Group("xxh3_hash").
		Having("COUNT(*) > 1").
		Order(fmt.Sprintf("%s %s", orderField, orderDir)).
		Offset(offset).Limit(query.PageSize).
		Scan(&hashCounts).Error; err != nil {
		return nil, 0, fmt.Errorf("查询重复分组失败: %w", err)
	}

	if len(hashCounts) == 0 {
		return make([]DuplicateGroup, 0), total, nil
	}

	// 一次批量取出本页所有哈希对应的文件详情，避免逐哈希查询（N+1）
	hashes := make([]string, len(hashCounts))
	for i, hc := range hashCounts {
		hashes[i] = hc.Xxh3Hash
	}
	var allFiles []models.FileRecordPublic
	if err := s.db.Model(&models.FileRecordPublic{}).
		Select("id, file_name, file_path, full_path, root_name, file_size, mod_time, xxh3_hash").
		Where("xxh3_hash IN ? AND status = ? AND is_dir = ? AND file_size > 0",
			hashes, models.FileStatusActive, false).
		Order("file_path ASC").
		Find(&allFiles).Error; err != nil {
		return nil, 0, fmt.Errorf("查询重复分组文件详情失败: %w", err)
	}
	filesByHash := make(map[string][]DuplicateFileItem, len(hashCounts))
	for _, f := range allFiles {
		filesByHash[f.Xxh3Hash] = append(filesByHash[f.Xxh3Hash], DuplicateFileItem{
			ID:       f.ID,
			FileName: f.FileName,
			FilePath: f.FilePath,
			FullPath: f.FullPath,
			RootName: f.RootName,
			FileSize: f.FileSize,
			ModTime:  f.ModTime,
		})
	}

	groups := make([]DuplicateGroup, 0, len(hashCounts))
	for _, hc := range hashCounts {
		files := filesByHash[hc.Xxh3Hash]
		if files == nil {
			files = make([]DuplicateFileItem, 0)
		}
		groups = append(groups, DuplicateGroup{
			Xxh3Hash:  hc.Xxh3Hash,
			FileCount: hc.Count,
			TotalSize: hc.SizeSum,
			Files:     files,
		})
	}

	return groups, total, nil
}

// UpdateNotes 更新文件备注
func (s *Service) UpdateNotes(id uint, notes string) error {
	result := s.db.Model(&models.FileRecordPublic{}).Where("id = ?", id).
		Update("notes", notes)
	if result.Error != nil {
		return fmt.Errorf("更新备注失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("文件记录不存在: %d", id)
	}
	return nil
}

// GetRecord 获取单个文件记录
func (s *Service) GetRecord(id uint) (*models.FileRecordPublic, error) {
	var record models.FileRecordPublic
	if err := s.db.First(&record, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("记录不存在: %d", id)
		}
		return nil, fmt.Errorf("查询记录失败: %w", err)
	}
	return &record, nil
}

// FindRecordByPath 根据文件名和路径查找文件记录
func (s *Service) FindRecordByPath(fileName, rootName, filePath string) (*models.FileRecordPublic, error) {
	var record models.FileRecordPublic
	if err := s.db.Where("file_name = ? AND root_name = ? AND file_path = ? AND status = 'active'",
		fileName, rootName, filePath).
		First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil // 未找到不返回错误
		}
		return nil, fmt.Errorf("查询文件记录失败: %w", err)
	}
	return &record, nil
}

// SearchFileRecords 搜索文件记录（用于 autocomplete）
func (s *Service) SearchFileRecords(query string, limit int) ([]models.FileRecordPublic, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var records []models.FileRecordPublic
	if err := s.db.Where("file_name LIKE ?", "%"+query+"%").
		Order("file_size DESC").
		Limit(limit).
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("搜索文件记录失败: %w", err)
	}
	if records == nil {
		records = make([]models.FileRecordPublic, 0)
	}
	return records, nil
}

// DeleteRecord 硬删除索引记录（管理员操作）
func (s *Service) DeleteRecord(id uint) error {
	result := s.db.Delete(&models.FileRecordPublic{}, id)
	if result.Error != nil {
		return fmt.Errorf("删除索引记录失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("索引记录不存在: %d", id)
	}
	return nil
}

// KeptDuplicateResult 保留指定文件记录，删除同哈希的其他重复文件（索引记录 + 磁盘文件）
// 返回被删除的文件列表
type KeptDuplicateResult struct {
	DeletedFiles []DuplicateFileItem `json:"deletedFiles"`
}

func (s *Service) KeepDuplicate(id uint) (*KeptDuplicateResult, error) {
	// 获取要保留的记录
	record, err := s.GetRecord(id)
	if err != nil {
		return nil, fmt.Errorf("获取文件记录失败: %w", err)
	}
	if record.Xxh3Hash == "" {
		return nil, fmt.Errorf("文件记录没有哈希值，无法查找重复文件")
	}

	// 查询同哈希的其他记录
	var duplicates []models.FileRecordPublic
	if err := s.db.Where("xxh3_hash = ? AND status = ? AND is_dir = ? AND id != ?",
		record.Xxh3Hash, models.FileStatusActive, false, record.ID).
		Find(&duplicates).Error; err != nil {
		return nil, fmt.Errorf("查询重复文件失败: %w", err)
	}

	if len(duplicates) == 0 {
		return &KeptDuplicateResult{DeletedFiles: make([]DuplicateFileItem, 0)}, nil
	}

	var deleted []DuplicateFileItem
	for _, dup := range duplicates {
		// 构建并删除磁盘文件
		rootPath, ok := s.rootNames[dup.RootName]
		if !ok {
			utils.Warn("保留重复文件时未找到根目录路径",
				utils.String("root_name", dup.RootName))
			continue
		}
		fullPath := filepath.Join(rootPath, dup.FilePath)
		if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			utils.Warn("删除重复文件磁盘文件失败",
				utils.String("path", fullPath),
				utils.Err(err))
			// 继续删除索引记录，不中断
		}

		// 删除索引记录
		if err := s.db.Delete(&models.FileRecordPublic{}, dup.ID).Error; err != nil {
			utils.Warn("删除重复文件索引记录失败",
				utils.Int("id", int(dup.ID)),
				utils.Err(err))
			continue
		}

		deleted = append(deleted, DuplicateFileItem{
			ID:       dup.ID,
			FileName: dup.FileName,
			FilePath: dup.FilePath,
			FullPath: dup.FullPath,
			RootName: dup.RootName,
			FileSize: dup.FileSize,
			ModTime:  dup.ModTime,
		})
	}

	return &KeptDuplicateResult{DeletedFiles: deleted}, nil
}
