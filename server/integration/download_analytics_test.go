package integration

import (
	"testing"
	"time"

	"fuzhan/internal/downloadanalytics"
	"fuzhan/internal/models"
)

// TestDownloadAnalytics_PublicOnlyAndSuccessSplit 验证分析服务只统计公共来源的下载事件，
// 并按 dl_ok 区分成功/失败：私有/临时/上传 等非公共下载记录一律不计入。
func TestDownloadAnalytics_PublicOnlyAndSuccessSplit(t *testing.T) {
	db := getTestDB(t)
	svc := downloadanalytics.NewService(db)

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	mk := func(action, source, status, fullPath, ip, user string, size int64) *models.OperationRecord {
		return &models.OperationRecord{
			FileName:   "f.pdf",
			FullPath:   fullPath,
			RootName:   "root",
			FileSize:   size,
			Action:     action,
			SourceType: source,
			Status:     status,
			ClientIP:   ip,
			UserID:     user,
			SourceID:   1,
			CreatedAt:  time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		}
	}
	// 公共来源下载成功×2、失败×1，私有/临时/上传 各 1 条（应被排除）
	records := []*models.OperationRecord{
		mk("download", "public", "success", "/root/doc1.pdf", "1.1.1.1", "u1", 100),
		mk("download-by-hash", "public", "success", "/root/doc2.pdf", "2.2.2.2", "u2", 200),
		mk("download", "public", "failed", "/root/doc3.pdf", "3.3.3.3", "u3", 300),
		mk("download", "private", "success", "/priv/x.txt", "4.4.4.4", "u4", 400),
		mk("download", "temp", "success", "/tmp/y.txt", "5.5.5.5", "", 500),
		mk("upload", "public", "success", "/root/readme.md", "6.6.6.6", "u5", 600),
	}
	for _, r := range records {
		if err := db.Create(r).Error; err != nil {
			t.Fatalf("插入操作记录失败: %v", err)
		}
	}

	sum, err := svc.Summary(from, to)
	if err != nil {
		t.Fatalf("Summary 失败: %v", err)
	}
	if sum.TotalDownloads != 2 {
		t.Errorf("TotalDownloads 期望 2，实际 %d", sum.TotalDownloads)
	}
	if sum.Failed != 1 {
		t.Errorf("Failed 期望 1，实际 %d", sum.Failed)
	}
	if sum.UniqueFiles != 2 {
		t.Errorf("UniqueFiles 期望 2，实际 %d", sum.UniqueFiles)
	}
	if sum.UniqueIPs != 2 {
		t.Errorf("UniqueIPs 期望 2，实际 %d", sum.UniqueIPs)
	}
	if sum.TotalBytes != 300 {
		t.Errorf("TotalBytes 期望 300，实际 %d", sum.TotalBytes)
	}

	trend, err := svc.TrendPoints(from, to, "day", 0)
	if err != nil {
		t.Fatalf("TrendPoints 失败: %v", err)
	}
	var okCount, failCount int64
	for _, p := range trend {
		okCount += p.Count
		failCount += p.Failed
	}
	if okCount != 2 || failCount != 1 {
		t.Errorf("趋势计数期望 成功2/失败1，实际 成功%d/失败%d", okCount, failCount)
	}

	heat, err := svc.Heatmap(from, to)
	if err != nil {
		t.Fatalf("Heatmap 失败: %v", err)
	}
	var heatTotal int64
	for _, c := range heat {
		heatTotal += c.Count
	}
	if heatTotal != 2 {
		t.Errorf("时段热度成功总和期望 2，实际 %d", heatTotal)
	}
}

// TestDownloadAnalytics_WatermarkCacheRefresh 验证水位缓存：新增公开下载事件会触发缓存失效并重算。
func TestDownloadAnalytics_WatermarkCacheRefresh(t *testing.T) {
	db := getTestDB(t)
	svc := downloadanalytics.NewService(db)

	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	base := &models.OperationRecord{
		FileName:   "f.pdf",
		FullPath:   "/root/doc.pdf",
		RootName:   "root",
		FileSize:   100,
		Action:     "download",
		SourceType: "public",
		Status:     "success",
		ClientIP:   "1.1.1.1",
		UserID:     "u1",
		SourceID:   1,
		CreatedAt:  time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
	}
	if err := db.Create(base).Error; err != nil {
		t.Fatalf("插入首条记录失败: %v", err)
	}

	sum1, err := svc.Summary(from, to)
	if err != nil {
		t.Fatalf("首次 Summary 失败: %v", err)
	}
	if sum1.TotalDownloads != 1 {
		t.Fatalf("首次 TotalDownloads 期望 1，实际 %d", sum1.TotalDownloads)
	}

	// 新增一条公开下载事件（新 id 位于水位之后），应触发缓存失效重算。
	extra := &models.OperationRecord{}
	*extra = *base
	extra.ID = 0
	extra.ClientIP = "2.2.2.2"
	extra.UserID = "u2"
	if err := db.Create(extra).Error; err != nil {
		t.Fatalf("插入新记录失败: %v", err)
	}

	sum2, err := svc.Summary(from, to)
	if err != nil {
		t.Fatalf("二次 Summary 失败: %v", err)
	}
	if sum2.TotalDownloads != 2 {
		t.Errorf("新增事件后 TotalDownloads 期望 2，实际 %d", sum2.TotalDownloads)
	}
}

// TestDownloadAnalytics_TopFilesUploadTimeBackfill 回归验证 TopFiles：
// 不得把 SQL 聚合列 MAX(upload_time) 直接 Scan 进 time.Time（SQLite 对聚合列返回 TEXT，
// 驱动无法转换会报 Scan error），上传时间应由 enrichPublicInfo 从权威索引表 file_records_public 回填。
func TestDownloadAnalytics_TopFilesUploadTimeBackfill(t *testing.T) {
	db := getTestDB(t)
	svc := downloadanalytics.NewService(db)

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC)

	// 权威索引表的创建时间即期望回填的上传时间
	created := time.Date(2026, 1, 20, 8, 30, 0, 0, time.UTC)
	idx := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		FileName:  "report.pdf",
		FilePath:  "/doc/report.pdf",
		RootName:  "root",
		FullPath:  "/root/doc/report.pdf",
		FileSize:  1024,
		Status:    models.FileStatusActive,
		CreatedAt: created,
		ModTime:   created,
	}}
	if err := db.Create(&idx).Error; err != nil {
		t.Fatalf("插入权威索引记录失败: %v", err)
	}

	mk := func(ip string) *models.OperationRecord {
		return &models.OperationRecord{
			FileName:   "report.pdf",
			FilePath:   "/doc/report.pdf",
			FullPath:   "/root/doc/report.pdf",
			RootName:   "root",
			FileSize:   1024,
			Action:     "download",
			SourceType: "public",
			Status:     "success",
			ClientIP:   ip,
			UserID:     "u1",
			SourceID:   idx.ID,
			UploadTime: created,
			CreatedAt:  time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
		}
	}
	for _, ip := range []string{"1.1.1.1", "2.2.2.2"} {
		if err := db.Create(mk(ip)).Error; err != nil {
			t.Fatalf("插入操作记录失败: %v", err)
		}
	}
	// 用遗留数据库的空格偏移时间格式覆盖 upload_time（模拟存量库混用格式，曾触发 Scan error）
	if err := db.Exec("UPDATE operation_records SET upload_time = ?",
		created.Format("2006-01-02 15:04:05.999999999-07:00")).Error; err != nil {
		t.Fatalf("覆盖 upload_time 失败: %v", err)
	}

	top, err := svc.TopFiles(from, to, "count", 10)
	if err != nil {
		t.Fatalf("TopFiles 失败（不应因 upload_time 格式触发 Scan error）: %v", err)
	}
	if len(top) < 1 {
		t.Fatalf("TopFiles 期望至少 1 条，实际 %d", len(top))
	}
	first := top[0]
	if first.SourceID != idx.ID {
		t.Errorf("SourceID 期望 %d，实际 %d", idx.ID, first.SourceID)
	}
	if !first.UploadTime.Equal(created) {
		t.Errorf("UploadTime 期望回填 %v，实际 %v", created, first.UploadTime)
	}
	if first.Count != 2 {
		t.Errorf("Count 期望 2，实际 %d", first.Count)
	}
	if first.Spread != 2 {
		t.Errorf("Spread 期望 2，实际 %d", first.Spread)
	}
}
