package services

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"
	"gorm.io/gorm"
)

// StorageStats 存储统计
type StorageStats struct {
	TotalSpace int64         `json:"totalSpace"`
	UsedSpace  int64         `json:"usedSpace"`
	FreeSpace  int64         `json:"freeSpace"`
	Roots      []RootStorage `json:"roots"`
	Temp       TempUsage     `json:"temp"`
	Private    PrivateUsage  `json:"private"`
}

// RootStorage 根目录存储
type RootStorage struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Total int64  `json:"total"`
	Quota int64  `json:"quota"`
	Used  int64  `json:"used"`
	Free  int64  `json:"free"`
}

// TempUsage 临时文件使用量
type TempUsage struct {
	QuotaPerIP int64            `json:"quotaPerIP"`
	UsedByIP   map[string]int64 `json:"usedByIP"`
}

// PrivateUsage 私有存储使用量
type PrivateUsage struct {
	UsedByUser map[string]int64 `json:"usedByUser"`
}

// AccessStats 访问统计
type AccessStats struct {
	TotalRequests int64 `json:"totalRequests"`
	ActiveUsers   int64 `json:"activeUsers"`
	OnlineIPs     int64 `json:"onlineIPs"`
}

// KeywordStat 关键词统计
type KeywordStat struct {
	Word  string `json:"word"`
	Count int    `json:"count"`
}

// RecentKeyword 最近关键词
type RecentKeyword struct {
	Word string    `json:"word"`
	Time time.Time `json:"time"`
}

// Rankings 排行榜
type Rankings struct {
	RecentUploads   []FileRank `json:"recentUploads"`
	RecentDownloads []FileRank `json:"recentDownloads"`
}

// FileRank 文件排行
type FileRank struct {
	Filename string    `json:"filename"`
	Time     time.Time `json:"time,omitempty"`
	Count    int       `json:"count,omitempty"`
}

// HotDownloadStat 热门下载统计
type HotDownloadStat struct {
	FileName   string    `json:"fileName"`
	FilePath   string    `json:"path"`
	FullPath   string    `json:"fullPath"`
	RootName   string    `json:"rootName"`
	FileSize   int64     `json:"fileSize"`
	UploadTime time.Time `json:"uploadTime"`
	Count      int       `json:"count"`
	Notes      string    `json:"notes"`
}

// HotDownloadResult 热门下载结果
type HotDownloadResult struct {
	Records  []HotDownloadStat `json:"records"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
}

// MonitorService 监控服务
type MonitorService struct {
	recordRepo repositories.AuditStore
	db         *gorm.DB

	// 存储统计缓存：GetStorageStats 会对全部根目录做整盘 filepath.Walk，
	// 加短 TTL 缓存避免管理面板高频刷新时反复触发磁盘 IO（也规避无节制的重查询）。
	storageMu    sync.Mutex
	storageCache *StorageStats
	storageAt    time.Time
	storageTTL   time.Duration
}

// NewMonitorService 创建监控服务
func NewMonitorService(recordRepo repositories.AuditStore, db *gorm.DB) *MonitorService {
	return &MonitorService{
		recordRepo: recordRepo,
		db:         db,
		storageTTL: 60 * time.Second,
	}
}

// GetHotDownloads 获取热门下载文件
//
// 统计口径与「文件下载次数」「行为分析」保持一致，全部基于写时物化的下载签名列：
//   - dl_event=1：仅公开来源的下载事件（download / download-by-hash），私有、临时下载一律不计；
//   - dl_ok=1：仅成功下载，失败/异常记录不计入热度；
//   - dl_counted=1：仅计一次新下载（排除断点续传/分段延续），与 file_records_public.download_count 同口径。
//
// 分组按文件身份（已关联的用 file_record_id，历史无关联记录回退完整路径），
// 避免覆盖上传导致同一文件按大小拆成多行；展示信息（名称/路径/备注/上传时间）
// 以索引表为权威来源按身份回填，保证移动/重命名后仍指向当前路径。
func (s *MonitorService) GetHotDownloads(page, pageSize int) (*HotDownloadResult, error) {
	type hotRow struct {
		// 聚合列 MAX() 在 MySQL/PostgreSQL 下返回 int64，统一用 int64 接收，
		// 避免各驱动把无符号列扫进 uint 时的差异。
		FileRecordID int64
		FileName     string
		FilePath     string
		FullPath     string
		RootName     string
		FileSize     int64
		Count        int64
	}

	const hotCond = "dl_event = 1 AND dl_ok = 1 AND dl_counted = 1"
	// 分组键：拼接统一用 CONCAT（SQLite / MySQL / PostgreSQL 通用），
	// 不用 `||`（MySQL 会按逻辑 OR 解析）。
	groupKey := `(CASE WHEN file_record_id > 0
		THEN CONCAT('f', file_record_id)
		ELSE CONCAT('p', COALESCE(full_path, ''))
		END)`

	base := func() *gorm.DB {
		return repositories.ApplyExistingFileFilter(
			s.db.Model(&models.OperationRecord{}).Where(hotCond),
		)
	}

	// 统计总分组数
	var total int64
	if err := s.db.Table("(?) AS grouped", base().Select("1").Group(groupKey)).Count(&total).Error; err != nil {
		return nil, err
	}

	// 分页查询；count 相同则按身份排序，保证 LIMIT/OFFSET 跨页稳定（不重复、不漏项）
	var rows []hotRow
	offset := (page - 1) * pageSize
	if err := base().
		Select("MAX(file_record_id) AS file_record_id, MAX(file_name) AS file_name, " +
			"MAX(file_path) AS file_path, MAX(full_path) AS full_path, MAX(root_name) AS root_name, " +
			"MAX(file_size) AS file_size, COUNT(*) AS count").
		Group(groupKey).
		Order("COUNT(*) DESC").
		Order(groupKey + " ASC").
		Limit(pageSize).Offset(offset).
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := make([]HotDownloadStat, 0, len(rows))
	byID := make(map[uint]int, len(rows))
	byPath := make(map[string]int, len(rows))
	for _, r := range rows {
		fullPath := r.FullPath
		if fullPath == "" && r.RootName != "" {
			fullPath = "/" + r.RootName + "/" + strings.TrimPrefix(r.FilePath, "/")
		}
		idx := len(result)
		result = append(result, HotDownloadStat{
			FileName: r.FileName,
			FilePath: r.FilePath,
			FullPath: fullPath,
			RootName: r.RootName,
			FileSize: r.FileSize,
			Count:    int(r.Count),
		})
		if r.FileRecordID > 0 {
			byID[uint(r.FileRecordID)] = idx
		} else if fullPath != "" {
			byPath[fullPath] = idx
		}
	}

	// 按索引记录 ID 回填（主路径：移动/重命名后仍指向当前路径）
	if len(byID) > 0 {
		ids := make([]uint, 0, len(byID))
		for id := range byID {
			ids = append(ids, id)
		}
		var fres []models.FileRecordPublic
		if err := s.db.Where("id IN ?", ids).Find(&fres).Error; err == nil {
			for i := range fres {
				if idx, ok := byID[fres[i].ID]; ok {
					result[idx] = hotStatFromRecord(fres[i], result[idx].Count)
				}
			}
		}
	}

	// 历史记录（未建立身份关联）回退按完整路径回填
	if len(byPath) > 0 {
		paths := make([]string, 0, len(byPath))
		for p := range byPath {
			paths = append(paths, p)
		}
		var fres []models.FileRecordPublic
		if err := s.db.Where("full_path IN ? AND status = ?", paths, models.FileStatusActive).Find(&fres).Error; err == nil {
			for i := range fres {
				if idx, ok := byPath[fres[i].FullPath]; ok {
					result[idx] = hotStatFromRecord(fres[i], result[idx].Count)
				}
			}
		}
	}

	return &HotDownloadResult{Records: result, Total: int(total), Page: page, PageSize: pageSize}, nil
}

// hotStatFromRecord 用索引表（当前权威路径/名称/备注/上传时间）覆盖统计行。
func hotStatFromRecord(rec models.FileRecordPublic, count int) HotDownloadStat {
	return HotDownloadStat{
		FileName:   rec.FileName,
		FilePath:   rec.FilePath,
		FullPath:   rec.FullPath,
		RootName:   rec.RootName,
		FileSize:   rec.FileSize,
		UploadTime: rec.CreatedAt,
		Count:      count,
		Notes:      rec.Notes,
	}
}

// GetStorageStats 获取存储统计
func (s *MonitorService) GetStorageStats() (*StorageStats, error) {
	// 命中缓存则直接返回，避免每次请求对全部根目录重新整盘 Walk。
	s.storageMu.Lock()
	if s.storageCache != nil && time.Since(s.storageAt) < s.storageTTL {
		stats := s.storageCache
		s.storageMu.Unlock()
		return stats, nil
	}
	s.storageMu.Unlock()

	stats := &StorageStats{
		Roots:   []RootStorage{},
		Temp:    TempUsage{UsedByIP: make(map[string]int64)},
		Private: PrivateUsage{UsedByUser: make(map[string]int64)},
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	type rootInfo struct {
		name      string
		path      string
		quota     int64
		used      int64
		diskTotal int64
	}

	rootCh := make(chan rootInfo, len(appconfig.GlobalConfig.Storage.Public.RootDirs))

	for _, rootDirConfig := range appconfig.GlobalConfig.Storage.Public.RootDirs {
		wg.Add(1)
		go func(rootPath string, quota int64) {
			defer wg.Done()

			walkCtx, walkCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer walkCancel()

			var used int64
			filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
				select {
				case <-walkCtx.Done():
					return walkCtx.Err()
				default:
				}
				if err != nil {
					utils.Warn("遍历目录时出错", utils.String("path", path), utils.Err(err))
					return nil
				}
				if !info.IsDir() {
					used += info.Size()
				}
				return nil
			})

			diskTotal, _, _ := GetDiskSpace(rootPath)

			dirName := ""
			for name, path := range appconfig.RootNames {
				if path == rootPath {
					dirName = name
					break
				}
			}
			if dirName == "" {
				dirName = filepath.Base(rootPath)
			}

			rootCh <- rootInfo{
				name:      dirName,
				path:      rootPath,
				quota:     quota,
				used:      used,
				diskTotal: diskTotal,
			}
		}(rootDirConfig.Path, rootDirConfig.Quota)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if appconfig.GlobalConfig.Storage.Temp.Enabled {
			type usageByIP struct {
				ClientIP string
				Total    int64
			}
			var results []usageByIP
			s.db.Model(&models.TempFile{}).
				Select("client_ip, COALESCE(SUM(file_size), 0) AS total").
				Group("client_ip").
				Scan(&results)
			mu.Lock()
			for _, r := range results {
				if r.ClientIP != "" {
					stats.Temp.UsedByIP[r.ClientIP] = r.Total
				}
			}
			mu.Unlock()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if appconfig.GlobalConfig.Storage.Private.Enabled && appconfig.GlobalConfig.Storage.Private.Path != "" {
			usersDir := filepath.Join(appconfig.GlobalConfig.Storage.Private.Path, "users")
			userEntries, err := os.ReadDir(usersDir)
			if err == nil {
				for _, userEntry := range userEntries {
					if !userEntry.IsDir() {
						continue
					}
					userID := userEntry.Name()
					userDir := filepath.Join(usersDir, userID)
					filepath.Walk(userDir, func(path string, info os.FileInfo, err error) error {
						if err == nil && !info.IsDir() {
							mu.Lock()
							stats.Private.UsedByUser[userID] += info.Size()
							mu.Unlock()
						}
						return nil
					})
				}
			}
		}
	}()

	wg.Wait()
	close(rootCh)

	for info := range rootCh {
		free := info.diskTotal - info.used
		if info.quota > 0 && info.diskTotal > 0 {
			free = info.quota - info.used
			if free < 0 {
				free = 0
			}
		}

		stats.Roots = append(stats.Roots, RootStorage{
			Name:  info.name,
			Path:  info.path,
			Total: info.diskTotal,
			Quota: info.quota,
			Used:  info.used,
			Free:  free,
		})

		stats.UsedSpace += info.used
		stats.TotalSpace += info.diskTotal
	}

	if appconfig.GlobalConfig.Storage.Temp.Quota.PerIP != "" {
		stats.Temp.QuotaPerIP, _ = appconfig.ParseQuotaString(appconfig.GlobalConfig.Storage.Temp.Quota.PerIP)
	} else {
		stats.Temp.QuotaPerIP = 1 << 30
	}

	stats.FreeSpace = stats.TotalSpace - stats.UsedSpace

	// 计算成功后再写缓存；失败不缓存，避免长时间返回陈旧统计。
	s.storageMu.Lock()
	s.storageCache = stats
	s.storageAt = utils.Now()
	s.storageMu.Unlock()

	return stats, nil
}

// GetAccessStats 获取访问统计
func (s *MonitorService) GetAccessStats() (*AccessStats, error) {
	// 总请求数
	var uploadCount int64
	s.db.Model(&models.OperationRecord{}).Count(&uploadCount)

	sevenDaysAgo := utils.Now().AddDate(0, 0, -7)
	fiveMinutesAgo := utils.Now().Add(-5 * time.Minute)

	// 活跃用户数（过去7天有操作的非管理员用户）
	// 管理员子查询用 GORM 构造（表名取自模型），不硬编码表名。
	var activeUsers int64
	adminIDs := s.db.Model(&models.User{}).Select("uuid").Where("role = ?", "admin")
	s.db.Model(&models.OperationRecord{}).
		Where("created_at > ? AND user_id != ''", sevenDaysAgo).
		Where("user_id NOT IN (?)", adminIDs).
		Distinct("user_id").Count(&activeUsers)

	// 在线 IP 数（过去5分钟有操作的）
	var onlineIPs int64
	s.db.Model(&models.OperationRecord{}).
		Where("created_at > ? AND client_ip != ''", fiveMinutesAgo).
		Distinct("client_ip").Count(&onlineIPs)

	return &AccessStats{
		TotalRequests: uploadCount,
		ActiveUsers:   activeUsers,
		OnlineIPs:     onlineIPs,
	}, nil
}

// 热门搜索关键词的统计窗口与条数上限。
// 搜索记录按请求逐条落库，若不加时间窗口就只能统计"最近 N 条记录"里的词频，
// 热点词一旦滑出窗口就消失，且计数与真实搜索量无关；这里改为按近 N 天聚合。
const (
	keywordWindowDays = 7
	keywordMaxLimit   = 50
)

// GetKeywords 获取热门搜索关键词（近 keywordWindowDays 天，按出现次数降序）
// 计数相同的词按关键词升序排列，保证结果稳定。
func (s *MonitorService) GetKeywords(limit int) ([]KeywordStat, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > keywordMaxLimit {
		limit = keywordMaxLimit
	}

	type keywordRow struct {
		Word  string
		Count int64
	}
	var rows []keywordRow
	// 按小写分组：MySQL 默认排序规则大小写不敏感，而 SQLite/PostgreSQL 敏感，
	// 直接 GROUP BY search_query 会让三库的分组结果不一致。展示词取 MIN 保证确定性。
	err := s.db.Model(&models.OperationRecord{}).
		Select("MIN(LOWER(search_query)) AS word, COUNT(*) AS count").
		Where("action = ? AND search_query IS NOT NULL AND search_query <> ''", "search").
		Where("created_at >= ?", utils.Now().AddDate(0, 0, -keywordWindowDays)).
		Group("LOWER(search_query)").
		Order("COUNT(*) DESC").
		Order("MIN(LOWER(search_query)) ASC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	result := make([]KeywordStat, 0, len(rows))
	for _, r := range rows {
		result = append(result, KeywordStat{Word: r.Word, Count: int(r.Count)})
	}
	return result, nil
}

// GetRecentKeywords 获取最近关键词
func (s *MonitorService) GetRecentKeywords() ([]RecentKeyword, error) {
	recent, err := s.recordRepo.GetRecentSearches(20)
	if err != nil {
		recent = []models.OperationRecord{}
	}

	result := make([]RecentKeyword, 0, len(recent))
	seen := make(map[string]bool)
	for _, r := range recent {
		if r.SearchQuery != "" && !seen[r.SearchQuery] {
			seen[r.SearchQuery] = true
			result = append(result, RecentKeyword{
				Word: r.SearchQuery,
				Time: r.CreatedAt,
			})
		}
	}

	return result, nil
}

// GetRankings 获取排行榜
func (s *MonitorService) GetRankings(limit int) (*Rankings, error) {
	records, err := s.recordRepo.GetRecent(limit * 5)
	if err != nil {
		records = []models.OperationRecord{}
	}

	rankings := &Rankings{
		RecentUploads:   make([]FileRank, 0),
		RecentDownloads: make([]FileRank, 0),
	}

	for _, r := range records {
		if r.Action == "upload" {
			rankings.RecentUploads = append(rankings.RecentUploads, FileRank{
				Filename: r.FileName,
				Time:     r.CreatedAt,
			})
		} else if r.Action == "download" {
			rankings.RecentDownloads = append(rankings.RecentDownloads, FileRank{
				Filename: r.FileName,
				Time:     r.CreatedAt,
				Count:    1,
			})
		}
	}

	sort.Slice(rankings.RecentUploads, func(i, j int) bool {
		return rankings.RecentUploads[i].Time.After(rankings.RecentUploads[j].Time)
	})
	sort.Slice(rankings.RecentDownloads, func(i, j int) bool {
		return rankings.RecentDownloads[i].Time.After(rankings.RecentDownloads[j].Time)
	})

	if len(rankings.RecentUploads) > limit {
		rankings.RecentUploads = rankings.RecentUploads[:limit]
	}
	if len(rankings.RecentDownloads) > limit {
		rankings.RecentDownloads = rankings.RecentDownloads[:limit]
	}

	return rankings, nil
}
