package services

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

// SearchService 搜索服务（基于数据库索引表）
type SearchService struct {
	db *gorm.DB
}

// NewSearchService 创建搜索服务实例
func NewSearchService(db *gorm.DB) *SearchService {
	return &SearchService{db: db}
}

// GetPublicFileIDByPath 按路径反查公共文件索引记录 ID（active），用于下载记录
// 建立与文件的身份关联（file_record_id），移动/重命名后依然有效。
func (s *SearchService) GetPublicFileIDByPath(rootName, fullPath string) (uint, error) {
	if s.db == nil || rootName == "" || fullPath == "" {
		return 0, nil
	}
	var candidates []string
	if strings.HasPrefix(fullPath, "/") {
		candidates = []string{fullPath, strings.TrimPrefix(fullPath, "/")}
	} else {
		candidates = []string{fullPath, "/" + fullPath}
	}
	var ids []uint
	err := s.db.Model(&models.FileRecordPublic{}).
		Where("root_name = ? AND status = ? AND deleted_at IS NULL", rootName, models.FileStatusActive).
		Where("full_path IN ?", candidates).
		Order("id ASC").Limit(1).Pluck("id", &ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}

// SearchFiles 同步搜索文件，一次性返回所有结果（不再使用 SSE 流式）。
// 支持使用空格分隔多个关键词（AND 逻辑），最后一个关键词可使用 .xxx 指定扩展名。
func (s *SearchService) SearchFiles(query string, rootDirs []appconfig.DirectoryConfig, timeout time.Duration) ([]appconfig.FileInfo, error) {
	if query == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// 获取 rootNames
	rootNameSet := make(map[string]string, len(rootDirs))
	for _, dir := range rootDirs {
		name := dir.Path
		if idx := strings.LastIndexAny(name, "/\\"); idx >= 0 {
			name = name[idx+1:]
		}
		rootNameSet[name] = dir.Path
	}

	// 多关键词 AND 搜索，空格分隔
	keywords := strings.Fields(query)
	if len(keywords) == 0 {
		return nil, nil
	}

	// 提取扩展名过滤条件（最后一个关键词以 . 开头或包含 .）
	var extFilter string
	lastIdx := len(keywords) - 1
	lastKw := keywords[lastIdx]
	if strings.HasPrefix(lastKw, ".") {
		// 纯扩展名过滤，如 ".pdf"
		extFilter = strings.ToLower(lastKw[1:])
		keywords = keywords[:lastIdx]
	} else if strings.Contains(lastKw, ".") {
		// 关键词包含扩展名，如 "file.txt"
		parts := strings.SplitN(lastKw, ".", 2)
		keywords[lastIdx] = parts[0]
		extFilter = strings.ToLower(parts[1])
	}

	utils.Info("开始数据库索引搜索",
		utils.String("query", query),
		utils.Int("keywords", len(keywords)),
		utils.Int("roots", len(rootNameSet)),
		utils.String("extFilter", extFilter))

	startTime := utils.Now()
	const maxResults = 1000

	// 合并所有根目录为单次查询，避免为每个 root 各自深分页反复全表扫描（原实现
	// 多 goroutine + OFFSET 在 SQLite 下单写锁下反而加剧锁竞争）。
	roots := make([]string, 0, len(rootNameSet))
	for rn := range rootNameSet {
		roots = append(roots, rn)
	}

	tx := s.db.WithContext(ctx).Model(&models.FileRecordPublic{}).
		Where("status = 'active'")
	if len(roots) > 0 {
		tx = tx.Where("root_name IN ?", roots)
	}
	for _, kw := range keywords {
		// 同时匹配文件名与备注，便于用备注内容检索文件
		tx = tx.Where("(file_name LIKE ? OR notes LIKE ?)", "%"+kw+"%", "%"+kw+"%")
	}
	// 添加扩展名过滤条件
	if extFilter != "" {
		tx = tx.Where("file_name LIKE ?", "%."+extFilter)
	}

	var records []models.FileRecordPublic
	if err := tx.Order("id DESC").Limit(maxResults).Find(&records).Error; err != nil {
		utils.Warn("数据库搜索查询失败", utils.Err(err))
		return nil, fmt.Errorf("数据库搜索查询失败: %w", err)
	}

	results := make([]appconfig.FileInfo, 0, len(records))
	for _, record := range records {
		fileType := "file"
		if record.IsDir {
			fileType = "directory"
		}
		results = append(results, appconfig.FileInfo{
			Name:          record.FileName,
			Type:          fileType,
			Path:          record.FullPath,
			ModifiedTime:  record.ModTime.Format("2006-01-02T15:04:05"),
			Size:          record.FileSize,
			RootName:      record.RootName,
			Xxh3Hash:      record.Xxh3Hash,
			Notes:         record.Notes,
			DownloadCount: record.DownloadCount,
			RecordID:      record.ID,
		})
	}

	utils.Info("数据库索引搜索完成",
		utils.String("query", query),
		utils.Int("results", len(results)),
		utils.String("duration", time.Since(startTime).String()))

	return results, nil
}

// Close 关闭搜索服务（释放资源）
func (s *SearchService) Close() error {
	return nil
}

// GetFileHashByPath 根据文件路径查询 xxh3 哈希值
func (s *SearchService) GetFileHashByPath(fullPath string) (string, error) {
	var record models.FileRecordPublic
	err := s.db.Model(&models.FileRecordPublic{}).
		Where("full_path = ? AND status = 'active'", fullPath).
		Select("xxh3_hash").
		First(&record).Error
	if err != nil {
		return "", err
	}
	return record.Xxh3Hash, nil
}

// LoadHashMap 加载指定根目录下所有文件的 xxh3 哈希映射（fullPath -> hash）
func (s *SearchService) LoadHashMap(rootName string) (map[string]string, error) {
	var records []struct {
		FullPath string
		Xxh3Hash string
	}
	err := s.db.Model(&models.FileRecordPublic{}).
		Where("root_name = ? AND status = 'active' AND xxh3_hash != ''", rootName).
		Select("full_path, xxh3_hash").
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	hashMap := make(map[string]string, len(records))
	for _, r := range records {
		hashMap[filepath.ToSlash(r.FullPath)] = r.Xxh3Hash
	}
	return hashMap, nil
}

// LoadDirectoryNotes 返回指定根目录下、某子目录的直接子项的备注映射。
// prefix 为根内相对路径（不含前导斜杠），空表示根级；返回的键为归一化的
// URL 相对路径（"/"+file_path，file_path 根级可能不带前导 /）。
func (s *SearchService) LoadDirectoryNotes(rootName, prefix string) map[string]string {
	if s.db == nil || rootName == "" {
		return nil
	}
	tx := s.db.Model(&models.FileRecordPublic{}).
		Where("root_name = ? AND status = ?", rootName, models.FileStatusActive)
	if prefix == "" {
		// 根级：文件路径不含额外 "/"（只有前导 / 或没有）
		tx = tx.Where("file_path NOT LIKE ?", "/%/%")
	} else {
		cleanPrefix := strings.TrimRight(prefix, "/")
		tx = tx.Where("file_path LIKE ? AND file_path NOT LIKE ?",
			"/"+cleanPrefix+"/%", "/"+cleanPrefix+"/%/%")
	}
	var rows []struct {
		FilePath string
		Notes    string
	}
	if err := tx.Select("file_path, notes").Find(&rows).Error; err != nil {
		utils.Warn("加载目录备注失败", utils.String("root", rootName), utils.Err(err))
		return nil
	}
	m := make(map[string]string, len(rows))
	for _, r := range rows {
		key := "/" + strings.TrimLeft(r.FilePath, "/")
		m[key] = r.Notes
	}
	return m
}

// IncrementPublicDownloadCount 公共文件下载次数 +1（按完整路径定位，非阻塞返回错误）
func (s *SearchService) IncrementPublicDownloadCount(fullPath string) error {
	if fullPath == "" {
		return nil
	}
	return s.db.Model(&models.FileRecordPublic{}).
		Where("full_path = ? AND status = ? AND is_dir = ?", fullPath, models.FileStatusActive, false).
		UpdateColumn("download_count", gorm.Expr("download_count + 1")).Error
}

// FindFilePathByHash 根据 xxh3 哈希值查找文件路径
func (s *SearchService) FindFilePathByHash(hash string) (string, string, error) {
	var record models.FileRecordPublic
	err := s.db.Model(&models.FileRecordPublic{}).
		Where("xxh3_hash = ? AND status = 'active' AND is_dir = ?", hash, false).
		Select("full_path, root_name").
		First(&record).Error
	if err != nil {
		return "", "", err
	}
	return record.FullPath, record.RootName, nil
}

// Validate 验证搜索服务配置
func (s *SearchService) Validate() error {
	if s.db == nil {
		return fmt.Errorf("search service: database is nil")
	}
	return nil
}
