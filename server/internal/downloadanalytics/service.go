package downloadanalytics

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"gorm.io/gorm"
)

// 下载行为分析：基于 operation_records 中公开文件下载记录（action=download/download-by-hash）聚合。
// 私有/临时下载虽在入口落 operation_records（source_type=private/temp），但分析层一律只统计公共来源。
//
// 提升一：写时物化的「下载统计签名」列 DlEvent/DlOk 在 OperationRecord.BeforeCreate 写入时计算，
// 所有查询退化为 dl_event/dl_ok 等值过滤，命中 (dl_event, created_at, dl_ok) 覆盖索引，
// 不再对 action/source_type/status 逐行做 OR/IN 表达式过滤。跨 SQLite/MySQL/PostgreSQL 语义一致。
//
// 提升二：cached() 采用「水位 + 兜底 TTL」的内存缓存：结果携带计算时的公开下载事件最大 id 作为水位，
// 命中时只要自水位以来无新增公开下载事件即无限期复用，避免固定 TTL 到期后无条件重扫；兜底有效期收敛
// 这些字段（如 file_records_public 的备注/上传时间、软删除等未进入水位追踪的变化）的陈旧性。

// dl_event=1 时 dl_ok 的成功/失败标志
const (
	dlOkSuccess = 1 // 成功下载
	dlOkFailed  = 0 // 失败/异常
)

// publicDownloadCond 公开下载事件且未软删除。deleted_at IS NULL 亦会被 GORM Scope 自动并入，
// 显式声明保持语义自明（下载记录确会被 DeleteByFile 等软删除）。
const publicDownloadCond = "dl_event = 1 AND deleted_at IS NULL"

// SummaryResult 区间总览
type SummaryResult struct {
	TotalDownloads int64 `json:"totalDownloads"` // 成功下载次数
	UniqueFiles    int64 `json:"uniqueFiles"`    // 去重文件数（成功）
	UniqueIPs      int64 `json:"uniqueIPs"`      // 去重客户端 IP（成功）
	UniqueUsers    int64 `json:"uniqueUsers"`    // 去重用户数（成功，匿名视为空不计）
	TotalBytes     int64 `json:"totalBytes"`     // 下载体积（成功，按一次下载记录文件大小）
	Failed         int64 `json:"failed"`         // 失败下载次数
}

// TrendPoint 时序点
type TrendPoint struct {
	Time   string `json:"time"` // 按粒度格式化：2006-01-02 / 2006年第WW周 / 2006-01
	Count  int64  `json:"count"`
	Failed int64  `json:"failed"`
}

// TopFile 热门下载文件
type TopFile struct {
	SourceID     uint      `json:"sourceId"`
	FileRecordID uint      `json:"fileRecordId"`
	FileName     string    `json:"fileName"`
	FilePath     string    `json:"path"`
	FullPath     string    `json:"fullPath"`
	RootName     string    `json:"rootName"`
	FileSize     int64     `json:"fileSize"`
	Count        int64     `json:"count"`
	Spread       int64     `json:"spread"` // 覆盖的不同客户端 IP 数（扩散）
	UploadTime   time.Time `json:"uploadTime,omitempty"`
	Notes        string    `json:"notes"`
}

// SourceStat 来源分布（IP/用户）
type SourceStat struct {
	Key       string `json:"key"`
	Count     int64  `json:"count"`
	FileCount int64  `json:"fileCount"`
}

// FailureStat 失败原因分布
type FailureStat struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
	IPs    int64  `json:"ips"`
}

// HeatCell 时段热度单元（时段热度分布）
type HeatCell struct {
	Weekday int   `json:"weekday"` // 1-7，1=周一
	Hour    int   `json:"hour"`    // 0-23
	Count   int64 `json:"count"`
}

// AggItem 目录/类型聚合条目
type AggItem struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
	Files int64  `json:"files"`
}

// LifecyclePoint 生命周期/衰减曲线点（自上传第 N 天的下载聚集度）
type LifecyclePoint struct {
	Day       int   `json:"day"` // 自上传算起第 N 天（0=当天）
	Downloads int64 `json:"downloads"`
	Files     int64 `json:"files"` // 当天发生过下载的去重文件数
}

// FileDetailResult 单文件明细
type FileDetailResult struct {
	FileRecordID uint         `json:"fileRecordId"`
	FileName     string       `json:"fileName"`
	FullPath     string       `json:"fullPath"`
	RootName     string       `json:"rootName"`
	Total        int64        `json:"total"`
	Failed       int64        `json:"failed"`
	Trend        []TrendPoint `json:"trend"`
	ByIP         []SourceStat `json:"byIP"`
	ByUser       []SourceStat `json:"byUser"`
}

// Service 下载行为分析服务
type Service struct {
	db    *gorm.DB
	cache *watermarkCache
}

// NewService 创建下载行为分析服务
func NewService(db *gorm.DB) *Service {
	return &Service{db: db, cache: newWatermarkCache()}
}

// cached 以 key 读取/写入缓存：命中且水位内无新增公开下载事件则直接返回，否则执行 compute() 并缓存。
// 写入时记录水位（当前最大公开下载事件 id），供后续命中判定。
func (s *Service) cached(key string, compute func() (interface{}, error)) (interface{}, error) {
	if e, ok := s.cache.get(key); ok && !s.hasNewDl(e.wm) {
		return e.data, nil
	}
	v, err := compute()
	if err != nil {
		return nil, err
	}
	s.cache.set(key, cacheEntry{at: time.Now(), data: v, wm: s.maxDlID()})
	return v, nil
}

// hasNewDl 判定自水位以来是否出现新的公开下载事件。命中 (dl_event, id) 索引，O(log n)。
// 查询异常时保守返回 true 触发重算，避免返回陈旧数据。
func (s *Service) hasNewDl(wm int64) bool {
	var one int
	if err := s.db.Model(&models.OperationRecord{}).
		Select("1").
		Where("dl_event = ? AND id > ?", dlOkSuccess, wm).
		Limit(1).
		Scan(&one).Error; err != nil {
		return true
	}
	return one == 1
}

// maxDlID 取当前最大公开下载事件记录 id 作为水位。命中 (dl_event, id) 索引，取出末尾条目即最大。
func (s *Service) maxDlID() int64 {
	var m int64
	if err := s.db.Model(&models.OperationRecord{}).
		Select("COALESCE(MAX(id), 0)").
		Where("dl_event = ?", dlOkSuccess).
		Scan(&m).Error; err != nil {
		return 0
	}
	return m
}

// cacheKey 构造缓存键：时间范围按分钟截断，使同一分钟内返回近乎相同的邻近请求命中同一缓存。
func cacheKey(method string, from, to time.Time, extra ...string) string {
	return fmt.Sprintf("%s|%d|%d|%s", method, from.Unix()/60, to.Unix()/60, strings.Join(extra, "|"))
}

// normalizeRange 归一化时间范围，缺省为近 30 天，保证前闭后开 [from, to)
func normalizeRange(from, to time.Time) (time.Time, time.Time) {
	now := utils.Now()
	if to.IsZero() {
		to = now
	}
	if from.IsZero() {
		from = to.AddDate(0, 0, -30)
	}
	if !from.Before(to) {
		from = to.AddDate(0, 0, -30)
	}
	return from, to
}

// Summary 获取区间下载总览（仅公共来源成功下载）
func (s *Service) Summary(from, to time.Time) (*SummaryResult, error) {
	from, to = normalizeRange(from, to)
	v, err := s.cached(cacheKey("summary", from, to), func() (interface{}, error) {
		base := publicDownloadCond + " AND created_at >= ? AND created_at < ?"
		ok := " AND dl_ok = ?"
		baseOK := base + ok

		qOK := func() *gorm.DB {
			return s.db.Model(&models.OperationRecord{}).
				Where(baseOK, from, to, dlOkSuccess)
		}

		var total int64
		if err := qOK().Count(&total).Error; err != nil {
			return nil, err
		}

		var uniqueFiles int64
		if err := qOK().Distinct("full_path").Count(&uniqueFiles).Error; err != nil {
			return nil, err
		}

		var uniqueIPs int64
		if err := qOK().Where("client_ip != ''").Distinct("client_ip").Count(&uniqueIPs).Error; err != nil {
			return nil, err
		}

		var uniqueUsers int64
		if err := qOK().Where("user_id != ''").Distinct("user_id").Count(&uniqueUsers).Error; err != nil {
			return nil, err
		}

		var bytes struct {
			Total int64
		}
		if err := qOK().Select("COALESCE(SUM(file_size), 0) AS total").Scan(&bytes).Error; err != nil {
			return nil, err
		}

		var failed int64
		if err := s.db.Model(&models.OperationRecord{}).
			Where(base+" AND dl_ok = ?", from, to, dlOkFailed).
			Count(&failed).Error; err != nil {
			return nil, err
		}

		return &SummaryResult{
			TotalDownloads: total,
			UniqueFiles:    uniqueFiles,
			UniqueIPs:      uniqueIPs,
			UniqueUsers:    uniqueUsers,
			TotalBytes:     bytes.Total,
			Failed:         failed,
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*SummaryResult), nil
}

// dailyRow 供 Go 侧日期聚合的原始行（SQL 仅取列，日期分组在 Go 内完成，避免数据库日期方言）
type dailyRow struct {
	CreatedAt time.Time
	DlOk      int
}

// TrendPoints 创建时序点序列（按粒度聚合，成功与失败两条线）；sourceID>0 时限定某公共文件。
// 日期分组在 Go 内完成：SQL 只负责按时间/来源过滤并取 created_at，兼容 SQLite/MySQL/PostgreSQL。
func (s *Service) TrendPoints(from, to time.Time, granularity string, sourceID uint) ([]TrendPoint, error) {
	from, to = normalizeRange(from, to)
	if granularity != "month" && granularity != "week" {
		granularity = "day"
	}

	v, err := s.cached(cacheKey("trend", from, to, granularity, strconv.FormatUint(uint64(sourceID), 10)),
		func() (interface{}, error) {
			db := s.db.Model(&models.OperationRecord{}).
				Select("created_at, dl_ok").
				Where(publicDownloadCond+" AND created_at >= ? AND created_at < ?", from, to)
			if sourceID > 0 {
				db = db.Where("source_id = ?", sourceID)
			}

			var rows []dailyRow
			if err := db.Order("created_at ASC").Scan(&rows).Error; err != nil {
				return nil, err
			}

			dayMap := make(map[string]*TrendPoint)
			for _, r := range rows {
				key := r.CreatedAt.Format("2006-01-02")
				if _, ok := dayMap[key]; !ok {
					dayMap[key] = &TrendPoint{Time: key}
				}
				if r.DlOk == dlOkFailed {
					dayMap[key].Failed++
				} else {
					dayMap[key].Count++
				}
			}
			// 占满区间内每一天
			for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
				key := d.Format("2006-01-02")
				if _, ok := dayMap[key]; !ok {
					dayMap[key] = &TrendPoint{Time: key}
				}
			}

			points := make([]TrendPoint, 0, len(dayMap))
			for _, p := range dayMap {
				points = append(points, *p)
			}
			sort.Slice(points, func(i, j int) bool { return points[i].Time < points[j].Time })

			if granularity == "day" {
				return points, nil
			}
			return bucketTrend(points, granularity), nil
		})
	if err != nil {
		return nil, err
	}
	return v.([]TrendPoint), nil
}

// bucketTrend 将日粒度序列聚合为周/月粒度
func bucketTrend(dayPoints []TrendPoint, granularity string) []TrendPoint {
	grouped := make(map[string]*TrendPoint)
	var order []string
	for _, p := range dayPoints {
		t, err := time.Parse("2006-01-02", p.Time)
		if err != nil {
			continue
		}
		var key string
		if granularity == "week" {
			// ISO 周：取该周周一作为桶
			year, week := t.ISOWeek()
			key = fmt.Sprintf("%d-W%02d", year, week)
		} else {
			key = t.Format("2006-01")
		}
		if _, ok := grouped[key]; !ok {
			grouped[key] = &TrendPoint{Time: key}
			order = append(order, key)
		}
		grouped[key].Count += p.Count
		grouped[key].Failed += p.Failed
	}
	sort.Strings(order)
	result := make([]TrendPoint, 0, len(order))
	for _, k := range order {
		result = append(result, *grouped[k])
	}
	return result
}

// TopFiles 获取区间内 Top 下载公共文件（按成功下载次数或扩散 IP 数排序）
func (s *Service) TopFiles(from, to time.Time, sortBy string, limit int) ([]TopFile, error) {
	from, to = normalizeRange(from, to)
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	if sortBy != "spread" {
		sortBy = "count"
	}

	v, err := s.cached(cacheKey("top", from, to, sortBy, strconv.Itoa(limit)), func() (interface{}, error) {
		type row struct {
			SourceID uint
			FileName string
			FilePath string
			FullPath string
			RootName string
			FileSize int64
			Count    int64
			Spread   int64
		}
		var rows []row
		// 注意：不在此处聚合 MAX(upload_time) 落盘到 time.Time —— SQLite 对聚合列返回 TEXT，
		// mattn 驱动无法将其 Scan 进 *time.Time（Scan error），且 operation_records 的上传时间非权威来源。
		// 上传时间改由 enrichPublicInfo 从权威索引表 file_records_public 按 full_path 回填。
		err := s.db.Model(&models.OperationRecord{}).
			Select("source_id, file_name, file_path, full_path, root_name, MAX(file_size) AS file_size, COUNT(*) AS count, COUNT(DISTINCT client_ip) AS spread").
			Where(publicDownloadCond+" AND dl_ok = ? AND created_at >= ? AND created_at < ?",
				dlOkSuccess, from, to).
			Group("source_id, full_path, file_name, file_path, root_name").
			Order(sortBy + " DESC").
			Limit(limit).
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}

		result := make([]TopFile, 0, len(rows))
		publicIndex := make(map[string]int, len(rows))
		for i, r := range rows {
			fullPath := r.FullPath
			if fullPath == "" {
				fullPath = "/" + r.RootName + "/" + strings.TrimPrefix(r.FilePath, "/")
			}
			publicIndex[fullPath] = i
			result = append(result, TopFile{
				SourceID:     r.SourceID,
				FileRecordID: r.SourceID,
				FileName:     r.FileName,
				FilePath:     r.FilePath,
				FullPath:     fullPath,
				RootName:     r.RootName,
				FileSize:     r.FileSize,
				Count:        r.Count,
				Spread:       r.Spread,
			})
		}

		s.enrichPublicInfo(publicIndex, result)
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]TopFile), nil
}

// enrichPublicInfo 按完整路径回填公共文件权威索引表的备注与上传时间
func (s *Service) enrichPublicInfo(publicIndex map[string]int, result []TopFile) {
	if len(publicIndex) == 0 {
		return
	}
	paths := make([]string, 0, len(publicIndex))
	for p := range publicIndex {
		paths = append(paths, p)
	}
	var fres []models.FileRecordPublic
	if err := s.db.Where("full_path IN ? AND status = ? AND deleted_at IS NULL",
		paths, models.FileStatusActive).Find(&fres).Error; err != nil {
		return
	}
	for _, f := range fres {
		if idx, ok := publicIndex[f.FullPath]; ok {
			result[idx].Notes = f.Notes
			if result[idx].UploadTime.IsZero() {
				result[idx].UploadTime = f.CreatedAt
			}
		}
	}
}

// Sources 获取来源分布（by=ip/user，仅公共来源成功下载）
func (s *Service) Sources(from, to time.Time, by string, limit int) ([]SourceStat, error) {
	from, to = normalizeRange(from, to)
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	if by != "user" {
		by = "ip"
	}

	col := "client_ip"
	emptyFilter := "client_ip != ''"
	if by == "user" {
		col = "user_id"
		emptyFilter = "user_id != ''"
	}

	v, err := s.cached(cacheKey("sources", from, to, by, strconv.Itoa(limit)), func() (interface{}, error) {
		type row struct {
			Key   string
			Count int64
			Files int64
		}
		var rows []row
		err := s.db.Model(&models.OperationRecord{}).
			Select(fmt.Sprintf("%s AS key, COUNT(*) AS count, COUNT(DISTINCT full_path) AS files", col)).
			Where(publicDownloadCond+" AND dl_ok = ? AND created_at >= ? AND created_at < ?",
				dlOkSuccess, from, to).
			Where(emptyFilter).
			Group(col).
			Order("count DESC").
			Limit(limit).
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		result := make([]SourceStat, 0, len(rows))
		for _, r := range rows {
			result = append(result, SourceStat{Key: r.Key, Count: r.Count, FileCount: r.Files})
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]SourceStat), nil
}

// FailureReasons 获取失败原因分布（仅公共来源 download 行动的失败记录）
func (s *Service) FailureReasons(from, to time.Time, limit int) ([]FailureStat, error) {
	from, to = normalizeRange(from, to)
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	v, err := s.cached(cacheKey("failures", from, to, strconv.Itoa(limit)), func() (interface{}, error) {
		type row struct {
			Reason string
			Count  int64
			IPs    int64
		}
		var rows []row
		err := s.db.Model(&models.OperationRecord{}).
			Select("COALESCE(NULLIF(fail_reason, ''), '未知') AS reason, COUNT(*) AS count, COUNT(DISTINCT client_ip) AS ips").
			Where(publicDownloadCond+" AND dl_ok = ? AND action = ? AND created_at >= ? AND created_at < ?",
				dlOkFailed, "download", from, to).
			Group("reason").
			Order("count DESC").
			Limit(limit).
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		result := make([]FailureStat, 0, len(rows))
		for _, r := range rows {
			result = append(result, FailureStat{Reason: r.Reason, Count: r.Count, IPs: r.IPs})
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]FailureStat), nil
}

// Heatmap 获取时段热度分布（周几 × 24 小时，仅公共来源成功下载）。
// 周几/小时在 Go 内计算（SQL 只取 created_at），避免 strftime 等日期方言函数。
func (s *Service) Heatmap(from, to time.Time) ([]HeatCell, error) {
	from, to = normalizeRange(from, to)
	v, err := s.cached(cacheKey("heatmap", from, to), func() (interface{}, error) {
		var rows []dailyRow
		err := s.db.Model(&models.OperationRecord{}).
			Select("created_at, dl_ok").
			Where(publicDownloadCond+" AND dl_ok = ? AND created_at >= ? AND created_at < ?",
				dlOkSuccess, from, to).
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}

		// 键 = 周几(1..7) * 100 + 小时
		counts := make(map[int]int64)
		for _, r := range rows {
			wd := int(r.CreatedAt.Weekday()) // 0=周日 .. 6=周六
			wd1 := (wd+6)%7 + 1              // 归一为周一=1 .. 周日=7
			counts[wd1*100+r.CreatedAt.Hour()]++
		}

		// 返回完整的 7×24 格子，缺失计 0
		cells := make([]HeatCell, 0, 7*24)
		for wd := 1; wd <= 7; wd++ {
			for h := 0; h < 24; h++ {
				cells = append(cells, HeatCell{Weekday: wd, Hour: h, Count: counts[wd*100+h]})
			}
		}
		return cells, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]HeatCell), nil
}

// Aggregate 获取目录/类型聚合（dimension=dir|type，仅公共来源成功下载）。
// 目录/扩展名推导在 Go 内用标准库完成（strings/filepath），避免 instr/reverse/substr/lower 等方言函数。
func (s *Service) Aggregate(from, to time.Time, dimension string, limit int) ([]AggItem, error) {
	from, to = normalizeRange(from, to)
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	if dimension != "type" {
		dimension = "dir"
	}

	v, err := s.cached(cacheKey("aggregate", from, to, dimension, strconv.Itoa(limit)), func() (interface{}, error) {
		type row struct {
			FullPath string
			FileName string
		}
		var rows []row
		err := s.db.Model(&models.OperationRecord{}).
			Select("full_path, file_name").
			Where(publicDownloadCond+" AND dl_ok = ? AND created_at >= ? AND created_at < ?",
				dlOkSuccess, from, to).
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}

		// key -> {count, files 去重集合}
		type acc struct {
			count int64
			files map[string]struct{}
		}
		grouped := make(map[string]*acc)
		for _, r := range rows {
			key := aggKey(r.FullPath, r.FileName, dimension)
			a, ok := grouped[key]
			if !ok {
				a = &acc{files: make(map[string]struct{})}
				grouped[key] = a
			}
			a.count++
			a.files[r.FullPath] = struct{}{}
		}

		result := make([]AggItem, 0, len(grouped))
		for key, a := range grouped {
			result = append(result, AggItem{Key: key, Count: a.count, Files: int64(len(a.files))})
		}
		sort.Slice(result, func(i, j int) bool {
			if result[i].Count != result[j].Count {
				return result[i].Count > result[j].Count
			}
			return result[i].Key < result[j].Key
		})
		if len(result) > limit {
			result = result[:limit]
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]AggItem), nil
}

// aggKey 计算目录或扩展名聚合键
func aggKey(fullPath, fileName, dimension string) string {
	if dimension == "type" {
		ext := filepath.Ext(fileName)
		if ext == "" {
			return "无扩展名"
		}
		return strings.ToLower(strings.TrimPrefix(ext, "."))
	}
	// dir：取 full_path 中最后一个 '/' 之前的部分；找不到或根级则归为根目录
	idx := strings.LastIndex(fullPath, "/")
	if idx <= 0 {
		return "（根目录）"
	}
	return fullPath[:idx]
}

// Lifecycle 获取生命周期/衰减曲线：自上传以来各天聚集的下载量（仅公共来源成功下载）。
// 上传时间取自权威索引表 file_records_public.CreatedAt，来源身份用 source_id 关联。
func (s *Service) Lifecycle(from, to time.Time, capDays int) ([]LifecyclePoint, error) {
	from, to = normalizeRange(from, to)
	if capDays <= 0 {
		capDays = 30
	}
	if capDays > 90 {
		capDays = 90
	}

	v, err := s.cached(cacheKey("lifecycle", from, to, strconv.Itoa(capDays)), func() (interface{}, error) {
		type row struct {
			OpCreated  time.Time
			FileCreate time.Time
			SourceID   uint
		}
		var rows []row
		err := s.db.Model(&models.OperationRecord{}).
			Select("operation_records.created_at AS op_created, frp.created_at AS file_create, operation_records.source_id").
			Joins("JOIN file_records_public frp ON frp.id = operation_records.source_id AND frp.deleted_at IS NULL AND frp.status = ?", models.FileStatusActive).
			Where("operation_records.dl_event = ? AND operation_records.dl_ok = ? AND operation_records.source_id > 0 AND operation_records.deleted_at IS NULL",
				dlOkSuccess, dlOkSuccess).
			Where("operation_records.created_at >= ? AND operation_records.created_at < ?", from, to).
			Order("operation_records.created_at ASC").
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}

		dlBy := make(map[int]int64)
		fileSet := make(map[int]map[uint]struct{})
		for _, r := range rows {
			opD := truncateDay(r.OpCreated)
			fD := truncateDay(r.FileCreate)
			if opD.IsZero() || fD.IsZero() || !opD.After(fD) {
				// 上传时间异常（如空/晚于下载）则按 0 天归入，避免丢失
				dlBy[0]++
				if fileSet[0] == nil {
					fileSet[0] = make(map[uint]struct{})
				}
				fileSet[0][r.SourceID] = struct{}{}
				continue
			}
			d := int(opD.Sub(fD).Hours() / 24)
			if d < 0 {
				d = 0
			}
			dlBy[d]++
			if fileSet[d] == nil {
				fileSet[d] = make(map[uint]struct{})
			}
			fileSet[d][r.SourceID] = struct{}{}
		}

		points := make([]LifecyclePoint, 0, capDays+1)
		for d := 0; d <= capDays; d++ {
			points = append(points, LifecyclePoint{
				Day:       d,
				Downloads: dlBy[d],
				Files:     int64(len(fileSet[d])),
			})
		}
		return points, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]LifecyclePoint), nil
}

// truncateDay 将时间截断到 UTC 当天零点，用于按天比较（兼容任意驱动的时区归一）
func truncateDay(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// FileDetail 获取单公共文件下载明细（时序 + IP/用户拆分）
func (s *Service) FileDetail(sourceID uint, from, to time.Time) (*FileDetailResult, error) {
	from, to = normalizeRange(from, to)
	if sourceID == 0 {
		return nil, fmt.Errorf("缺少有效的文件ID")
	}

	var meta struct {
		FileName string
		FullPath string
		RootName string
	}
	if err := s.db.Model(&models.OperationRecord{}).
		Select("file_name, full_path, root_name").
		Where("source_id = ? AND deleted_at IS NULL", sourceID).
		Order("created_at DESC").
		First(&meta).Error; err != nil {
		return nil, err
	}

	trend, err := s.TrendPoints(from, to, "day", sourceID)
	if err != nil {
		return nil, err
	}

	byIP, err := s.sourceBreakdown(from, to, sourceID, "client_ip", "client_ip != ''")
	if err != nil {
		return nil, err
	}
	byUser, err := s.sourceBreakdown(from, to, sourceID, "user_id", "user_id != ''")
	if err != nil {
		return nil, err
	}

	var total, failed int64
	if err := s.db.Model(&models.OperationRecord{}).
		Where(publicDownloadCond+" AND source_id = ? AND created_at >= ? AND created_at < ? AND dl_ok = ?",
			sourceID, from, to, dlOkSuccess).
		Count(&total).Error; err != nil {
		return nil, err
	}
	if err := s.db.Model(&models.OperationRecord{}).
		Where(publicDownloadCond+" AND source_id = ? AND created_at >= ? AND created_at < ? AND dl_ok = ?",
			sourceID, from, to, dlOkFailed).
		Count(&failed).Error; err != nil {
		return nil, err
	}

	return &FileDetailResult{
		FileRecordID: sourceID,
		FileName:     meta.FileName,
		FullPath:     meta.FullPath,
		RootName:     meta.RootName,
		Total:        total,
		Failed:       failed,
		Trend:        trend,
		ByIP:         byIP,
		ByUser:       byUser,
	}, nil
}

// sourceBreakdown 单公共文件的 IP/用户成功下载拆分，emptyFilter 决定忽略空值来源标识
func (s *Service) sourceBreakdown(from, to time.Time, sourceID uint, col, emptyFilter string) ([]SourceStat, error) {
	v, err := s.cached(cacheKey("breakdown", from, to, strconv.FormatUint(uint64(sourceID), 10), col),
		func() (interface{}, error) {
			type row struct {
				Key   string
				Count int64
			}
			var rows []row
			err := s.db.Model(&models.OperationRecord{}).
				Select(fmt.Sprintf("%s AS key, COUNT(*) AS count", col)).
				Where(publicDownloadCond+" AND source_id = ? AND dl_ok = ? AND created_at >= ? AND created_at < ?",
					sourceID, dlOkSuccess, from, to).
				Where(emptyFilter).
				Group(col).
				Order("count DESC").
				Limit(20).
				Scan(&rows).Error
			if err != nil {
				return nil, err
			}
			result := make([]SourceStat, 0, len(rows))
			for _, r := range rows {
				result = append(result, SourceStat{Key: r.Key, Count: r.Count})
			}
			return result, nil
		})
	if err != nil {
		return nil, err
	}
	return v.([]SourceStat), nil
}
