package services

import (
    "context"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "sync"
    "time"

    "gorm.io/gorm"
    "fuzhan/internal/appconfig"
    "fuzhan/internal/models"
    "fuzhan/internal/repositories"
    "fuzhan/internal/utils"
)

// StorageStats 存储统计
type StorageStats struct {
    TotalSpace int64              `json:"totalSpace"`
    UsedSpace  int64              `json:"usedSpace"`
    FreeSpace  int64              `json:"freeSpace"`
    Roots      []RootStorage      `json:"roots"`
    Temp       TempUsage          `json:"temp"`
    Private    PrivateUsage       `json:"private"`
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
}

// NewMonitorService 创建监控服务
func NewMonitorService(recordRepo repositories.AuditStore, db *gorm.DB) *MonitorService {
    return &MonitorService{
        recordRepo: recordRepo,
        db:         db,
    }
}

// GetHotDownloads 获取热门下载文件
func (s *MonitorService) GetHotDownloads(page, pageSize int) (*HotDownloadResult, error) {
    type fileDownloadCount struct {
        FileName   string
        FilePath   string
        FullPath   string
        RootName   string
        FileSize   int64
        UploadTime string
        Count      int
    }
    var downloadCounts []fileDownloadCount
    if err := s.db.Model(&models.OperationRecord{}).
        Select("file_name, file_path, full_path, root_name, file_size, MAX(created_at) as upload_time, COUNT(*) as count").
        Where("action = ?", "download").
        Group("file_name, file_path, full_path, root_name, file_size").
        Order("count DESC").
        Find(&downloadCounts).Error; err != nil {
        return nil, err
    }

    // 统计总分组数
    var total int64
    subQuery := s.db.Model(&models.OperationRecord{}).
        Select("1").
        Where("action = ?", "download").
        Group("file_name, file_path, full_path, root_name, file_size")
    s.db.Table("(?) AS grouped", subQuery).Count(&total)

    // 分页查询
    offset := (page - 1) * pageSize
    if err := s.db.Model(&models.OperationRecord{}).
        Select("file_name, file_path, full_path, root_name, file_size, MAX(created_at) as upload_time, COUNT(*) as count").
        Where("action = ?", "download").
        Group("file_name, file_path, full_path, root_name, file_size").
        Order("count DESC").
        Limit(pageSize).Offset(offset).
        Find(&downloadCounts).Error; err != nil {
        return nil, err
    }

    result := make([]HotDownloadStat, 0, len(downloadCounts))
    for _, d := range downloadCounts {
        var uploadTime time.Time
        if len(d.UploadTime) >= 19 {
            if t, err := time.Parse("2006-01-02 15:04:05", d.UploadTime[:19]); err == nil {
                uploadTime = t
            }
        }
        fullPath := d.FullPath
        if fullPath == "" {
            fullPath = "/" + d.RootName + "/" + strings.TrimPrefix(d.FilePath, "/")
        }
        result = append(result, HotDownloadStat{
            FileName:   d.FileName,
            FilePath:   d.FilePath,
            FullPath:   fullPath,
            RootName:   d.RootName,
            FileSize:   d.FileSize,
            UploadTime: uploadTime,
            Count:      d.Count,
        })
    }

    return &HotDownloadResult{Records: result, Total: int(total), Page: page, PageSize: pageSize}, nil
}

// GetStorageStats 获取存储统计
func (s *MonitorService) GetStorageStats() (*StorageStats, error) {
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

    return stats, nil
}

// GetAccessStats 获取访问统计
func (s *MonitorService) GetAccessStats() (*AccessStats, error) {
    // 总请求数
    var uploadCount int64
    s.db.Model(&models.OperationRecord{}).Count(&uploadCount)

    sevenDaysAgo := time.Now().AddDate(0, 0, -7)
    fiveMinutesAgo := time.Now().Add(-5 * time.Minute)

    // 活跃用户数（过去7天有操作的非管理员用户）
    var activeUsers int64
    s.db.Model(&models.OperationRecord{}).
        Where("created_at > ? AND user_id != ''", sevenDaysAgo).
        Where("user_id NOT IN (SELECT uuid FROM users WHERE role = 'admin')").
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

// GetKeywords 获取关键词统计
func (s *MonitorService) GetKeywords(limit int) ([]KeywordStat, error) {
    recent, err := s.recordRepo.GetRecentSearches(limit * 5)
    if err != nil {
        recent = []models.OperationRecord{}
    }

    wordCount := make(map[string]int)
    for _, r := range recent {
        if r.SearchQuery != "" {
            wordCount[r.SearchQuery]++
        }
    }

    type wordStat struct {
        Word  string
        Count int
    }
    stats := make([]wordStat, 0, len(wordCount))
    for word, count := range wordCount {
        stats = append(stats, wordStat{Word: word, Count: count})
    }
    sort.Slice(stats, func(i, j int) bool {
        return stats[i].Count > stats[j].Count
    })

    result := make([]KeywordStat, 0, limit)
    for i, s := range stats {
        if i >= limit {
            break
        }
        result = append(result, KeywordStat{Word: s.Word, Count: s.Count})
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
