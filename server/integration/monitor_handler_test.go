package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/monitor"
	"fuzhan/internal/repositories"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// getMonitorTestDB 创建测试数据库（复用 recent_handler_test 的逻辑）
func getMonitorTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.OperationRecord{}, &models.FileRecordPublic{}); err != nil {
		t.Fatalf("迁移数据库失败: %v", err)
	}
	return db
}

// TestMonitorHandler_Storage 测试存储统计
func TestMonitorHandler_Storage(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/web/monitor/storage", handler.Storage)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web/monitor/storage", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Error("期望 success 为 true")
	}

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data 应为对象")
	}

	if data["total_space"] == nil {
		t.Error("total_space 字段应存在")
	}
	if data["used_space"] == nil {
		t.Error("used_space 字段应存在")
	}
	if data["roots"] == nil {
		t.Error("roots 字段应存在")
	}
}

// TestMonitorHandler_Access 测试访问统计
func TestMonitorHandler_Access(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/web/monitor/access", handler.Access)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web/monitor/access", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Error("期望 success 为 true")
	}

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data 应为对象")
	}

	if data["total_requests"] == nil {
		t.Error("total_requests 字段应存在")
	}
	if data["active_users"] == nil {
		t.Error("active_users 字段应存在")
	}
}

// TestMonitorHandler_Access_WithRecords 测试有记录时的访问统计
func TestMonitorHandler_Access_WithRecords(t *testing.T) {
	db := getMonitorTestDB(t)

	// 添加测试记录
	records := []models.OperationRecord{
		{Action: "upload", FileName: "test1.txt", UserID: "user1"},
		{Action: "download", FileName: "test2.txt", UserID: "user1"},
		{Action: "upload", FileName: "test3.txt", UserID: "user2"},
	}
	for i := range records {
		db.Create(&records[i])
	}

	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/web/monitor/access", handler.Access)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web/monitor/access", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data 应为对象")
	}

	// 验证统计数据
	totalRequests, ok := data["total_requests"].(float64)
	if !ok {
		t.Error("total_requests 应为数字")
	}
	if totalRequests != 3 {
		t.Errorf("期望 total_requests 为 3，实际 %v", totalRequests)
	}
}

// TestMonitorHandler_Keywords 测试关键词统计
func TestMonitorHandler_Keywords(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/web/monitor/keywords", handler.Keywords)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web/monitor/keywords", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Error("期望 success 为 true")
	}
}

// TestMonitorHandler_Keywords_WithSearchRecords 测试有搜索记录时的关键词统计
func TestMonitorHandler_Keywords_WithSearchRecords(t *testing.T) {
	db := getMonitorTestDB(t)

	// 添加搜索记录
	records := []models.OperationRecord{
		{Action: "search", SearchQuery: "report", FileName: ""},
		{Action: "search", SearchQuery: "report", FileName: ""},
		{Action: "search", SearchQuery: "invoice", FileName: ""},
	}
	for i := range records {
		db.Create(&records[i])
	}

	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/web/monitor/keywords", handler.Keywords)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web/monitor/keywords", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	data, ok := response["data"].([]interface{})
	if !ok {
		t.Fatal("data 应为数组")
	}

	if len(data) != 2 {
		t.Errorf("期望 2 个关键词，实际 %d", len(data))
	}

	// 验证排序（report 出现 2 次应在首位）
	if len(data) > 0 {
		first, ok := data[0].(map[string]interface{})
		if ok && first["word"] != "report" {
			t.Errorf("期望第一个关键词为 report，实际 %v", first["word"])
		}
	}
}

// TestMonitorHandler_RecentKeywords 测试最近关键词
func TestMonitorHandler_RecentKeywords(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/web/monitor/recent", handler.RecentKeywords)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web/monitor/recent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Error("期望 success 为 true")
	}
}

// TestMonitorHandler_Rankings 测试排行榜
func TestMonitorHandler_Rankings(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/web/monitor/rankings", handler.Rankings)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web/monitor/rankings", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["success"] != true {
		t.Error("期望 success 为 true")
	}

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data 应为对象")
	}

	if data["recent_uploads"] == nil {
		t.Error("recent_uploads 字段应存在")
	}
	if data["recent_downloads"] == nil {
		t.Error("recent_downloads 字段应存在")
	}
}

// TestMonitorHandler_Rankings_WithRecords 测试有记录时的排行榜
func TestMonitorHandler_Rankings_WithRecords(t *testing.T) {
	db := getMonitorTestDB(t)

	// 添加上传和下载记录（RootName/FilePath 需与下方索引匹配，避免被"文件存在"过滤排除）
	records := []models.OperationRecord{
		{Action: "upload", RootName: "root", FilePath: "test1.txt", FileName: "test1.txt"},
		{Action: "download", RootName: "root", FilePath: "test2.txt", FileName: "test2.txt"},
		{Action: "upload", RootName: "root", FilePath: "test3.txt", FileName: "test3.txt"},
	}
	for i := range records {
		db.Create(&records[i])
	}

	// 对应用户文件索引需存在，否则会被"文件存在"过滤排除
	indexFiles := []models.FileRecordPublic{
		{FileRecordBase: models.FileRecordBase{RootName: "root", FilePath: "/test1.txt", FileName: "test1.txt", FileSize: 1, LastSyncedAt: time.Now()}},
		{FileRecordBase: models.FileRecordBase{RootName: "root", FilePath: "/test2.txt", FileName: "test2.txt", FileSize: 1, LastSyncedAt: time.Now()}},
		{FileRecordBase: models.FileRecordBase{RootName: "root", FilePath: "/test3.txt", FileName: "test3.txt", FileSize: 1, LastSyncedAt: time.Now()}},
	}
	for _, f := range indexFiles {
		db.Create(&f)
	}

	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/web/monitor/rankings", handler.Rankings)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/web/monitor/rankings", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("期望状态码 200，实际 %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)

	data, ok := response["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data 应为对象")
	}

	uploads, ok := data["recent_uploads"].([]interface{})
	if !ok {
		t.Fatal("recent_uploads 应为数组")
	}
	if len(uploads) != 2 {
		t.Errorf("期望 2 条上传记录，实际 %d", len(uploads))
	}

	downloads, ok := data["recent_downloads"].([]interface{})
	if !ok {
		t.Fatal("recent_downloads 应为数组")
	}
	if len(downloads) != 1 {
		t.Errorf("期望 1 条下载记录，实际 %d", len(downloads))
	}
}

// TestHotDownloads_OnlyPublicSuccessCounted 验证热门下载的统计口径：
// 只统计公开来源、成功、且计入下载次数的新下载；私有/临时/失败/续传/预览一律不计；
// 同一文件（按 file_record_id 身份）的 download 与 download-by-hash 合并计数（不按文件大小拆组）；
// 展示信息以索引表当前值为准（改名后仍指向最新路径与备注）；已软删除的文件不再出现。
func TestHotDownloads_OnlyPublicSuccessCounted(t *testing.T) {
	db := getMonitorTestDB(t)
	baseTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	recA := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		FileName: "a.txt", FilePath: "/a.txt", RootName: "docs", FullPath: "/docs/a.txt",
		FileSize: 100, Status: models.FileStatusActive, CreatedAt: baseTime,
	}}
	recB := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		FileName: "b.txt", FilePath: "/b.txt", RootName: "docs", FullPath: "/docs/b.txt",
		FileSize: 300, Status: models.FileStatusActive, CreatedAt: baseTime,
	}}
	recGone := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		FileName: "gone.txt", FilePath: "/gone.txt", RootName: "docs", FullPath: "/docs/gone.txt",
		FileSize: 400, Status: models.FileStatusActive, CreatedAt: baseTime,
	}}
	for _, r := range []*models.FileRecordPublic{&recA, &recB, &recGone} {
		if err := db.Create(r).Error; err != nil {
			t.Fatalf("创建索引记录失败: %v", err)
		}
	}
	// 软删除的文件不应出现在热门列表中
	if err := db.Delete(&recGone).Error; err != nil {
		t.Fatalf("软删除索引记录失败: %v", err)
	}
	// 移动/重命名 + 覆盖上传后，索引表记录的是当前路径、备注与大小
	if err := db.Model(&recA).Updates(map[string]interface{}{
		"file_name": "a-renamed.txt", "file_path": "/a-renamed.txt",
		"full_path": "/docs/a-renamed.txt", "file_size": 200, "notes": "备注A",
	}).Error; err != nil {
		t.Fatalf("更新索引记录失败: %v", err)
	}

	now := utils.Now()
	ops := []models.OperationRecord{
		// A：3 次成功新下载（含 1 次按哈希下载，且 file_size 与索引表不一致，验证不按大小拆组）
		{Action: "download", FileName: "a.txt", FilePath: "/a.txt", FullPath: "/docs/a.txt", RootName: "docs", FileSize: 1, FileRecordID: recA.ID, SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now},
		{Action: "download", FileName: "a.txt", FilePath: "/a.txt", FullPath: "/docs/a.txt", RootName: "docs", FileSize: 1, FileRecordID: recA.ID, SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now},
		{Action: "download-by-hash", FileName: "a.txt", FilePath: "/", FullPath: "/docs/a.txt", RootName: "docs", FileSize: 1, FileRecordID: recA.ID, SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now},
		// A：断点续传/分段延续（不计入下载次数）
		{Action: "download", FileName: "a.txt", FilePath: "/a.txt", FullPath: "/docs/a.txt", RootName: "docs", FileSize: 1, FileRecordID: recA.ID, SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, DlCounted: 0, CreatedAt: now},
		// A：失败下载
		{Action: "download", FileName: "a.txt", FilePath: "/a.txt", FullPath: "/docs/a.txt", RootName: "docs", FileSize: 0, FileRecordID: recA.ID, SourceType: models.SourceTypePublic, Status: models.RecordStatusFailed, FailReason: "文件不存在", DlCounted: 0, CreatedAt: now},
		// A：预览
		{Action: "preview", FileName: "a.txt", FilePath: "/a.txt", FullPath: "/docs/a.txt", RootName: "docs", FileSize: 1, FileRecordID: recA.ID, SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, CreatedAt: now},
		// B：2 次成功新下载
		{Action: "download", FileName: "b.txt", FilePath: "/b.txt", FullPath: "/docs/b.txt", RootName: "docs", FileSize: 300, FileRecordID: recB.ID, SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now},
		{Action: "download", FileName: "b.txt", FilePath: "/b.txt", FullPath: "/docs/b.txt", RootName: "docs", FileSize: 300, FileRecordID: recB.ID, SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now},
		// 私有/临时下载：评分口径只统计公共来源
		{Action: "download", FileName: "p.txt", FilePath: "/p.txt", FullPath: "/private/u1/p.txt", RootName: "private", FileSize: 1, SourceType: models.SourceTypePrivate, UploadType: models.TargetTypePrivate, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now},
		{Action: "download", FileName: "t.txt", FilePath: "/t.txt", FullPath: "/temp/t.txt", RootName: "temp", FileSize: 1, SourceType: models.SourceTypeTemp, UploadType: models.TargetTypeTemp, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now},
		// 已删除文件
		{Action: "download", FileName: "gone.txt", FilePath: "/gone.txt", FullPath: "/docs/gone.txt", RootName: "docs", FileSize: 400, FileRecordID: recGone.ID, SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now},
		// 历史记录（未关联 file_record_id）：回退按完整路径关联索引表
		{Action: "download", FileName: "c.txt", FilePath: "/c.txt", FullPath: "/docs/c.txt", RootName: "docs", FileSize: 500, SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now},
	}
	for i := range ops {
		if err := db.Create(&ops[i]).Error; err != nil {
			t.Fatalf("创建操作记录失败: %v", err)
		}
	}
	recC := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		FileName: "c.txt", FilePath: "/c.txt", RootName: "docs", FullPath: "/docs/c.txt",
		FileSize: 500, Status: models.FileStatusActive, CreatedAt: baseTime,
	}}
	if err := db.Create(&recC).Error; err != nil {
		t.Fatalf("创建索引记录失败: %v", err)
	}

	// D：覆盖老版本升级后的历史记录——file_record_id 列由 AutoMigrate 追加，存量行取值为 NULL。
	// 这类记录既不属于 >0 分支也不属于 =0 分支，必须与索引记录的其他下载记录合并为同一条。
	recD := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		FileName: "d.txt", FilePath: "/d.txt", RootName: "docs", FullPath: "/docs/d.txt",
		FileSize: 600, Status: models.FileStatusActive, CreatedAt: baseTime,
	}}
	if err := db.Create(&recD).Error; err != nil {
		t.Fatalf("创建索引记录失败: %v", err)
	}
	dOps := []*models.OperationRecord{
		{Action: "download", FileName: "d.txt", FilePath: "/d.txt", FullPath: "/docs/d.txt", RootName: "docs", FileSize: 600, FileRecordID: recD.ID, SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now},
		{Action: "download", FileName: "d.txt", FilePath: "/d.txt", FullPath: "/docs/d.txt", RootName: "docs", FileSize: 600, SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now},
	}
	for i := range dOps {
		if err := db.Create(dOps[i]).Error; err != nil {
			t.Fatalf("创建操作记录失败: %v", err)
		}
	}
	if err := db.Model(&models.OperationRecord{}).Where("id = ?", dOps[1].ID).
		UpdateColumn("file_record_id", nil).Error; err != nil {
		t.Fatalf("置空历史记录的身份失败: %v", err)
	}
	// 启动流程会回填 NULL/0 的身份（bootstrap 中调用），热门统计依赖回填后的身份归并
	services.BackfillFileRecordIDs(db)

	svc := services.NewMonitorService(repositories.NewRecordRepository(db), db)
	res, err := svc.GetHotDownloads(1, 20)
	if err != nil {
		t.Fatalf("获取热门下载失败: %v", err)
	}
	if res.Total != 4 {
		t.Errorf("期望 4 个热门文件，实际 %d", res.Total)
	}
	if len(res.Records) != 4 {
		t.Fatalf("期望 4 条记录，实际 %d", len(res.Records))
	}

	a := res.Records[0]
	if a.FullPath != "/docs/a-renamed.txt" || a.FileName != "a-renamed.txt" {
		t.Errorf("应按索引表回填当前路径，实际 %s / %s", a.FullPath, a.FileName)
	}
	if a.Count != 3 {
		t.Errorf("A 期望 3 次下载（含 download-by-hash），实际 %d", a.Count)
	}
	if a.Notes != "备注A" {
		t.Errorf("A 期望备注 备注A，实际 %q", a.Notes)
	}
	if a.FileSize != 200 {
		t.Errorf("A 期望按索引表回填大小 200，实际 %d", a.FileSize)
	}
	if !a.UploadTime.Equal(baseTime) {
		t.Errorf("A 期望上传时间 %v，实际 %v", baseTime, a.UploadTime)
	}
	if res.Records[1].FullPath != "/docs/b.txt" || res.Records[1].Count != 2 {
		t.Errorf("B 期望 /docs/b.txt 2 次，实际 %s %d", res.Records[1].FullPath, res.Records[1].Count)
	}
	if res.Records[2].FullPath != "/docs/d.txt" || res.Records[2].Count != 2 {
		t.Errorf("D 期望 /docs/d.txt 2 次（含 file_record_id 为 NULL 的历史记录），实际 %s %d", res.Records[2].FullPath, res.Records[2].Count)
	}
	if res.Records[3].FullPath != "/docs/c.txt" || res.Records[3].Count != 1 {
		t.Errorf("C 期望 /docs/c.txt 1 次，实际 %s %d", res.Records[3].FullPath, res.Records[3].Count)
	}

	// 分页按 count 降序稳定推进：不重复、不漏项
	seen := make(map[string]bool)
	for page := 1; page <= 4; page++ {
		pr, err := svc.GetHotDownloads(page, 1)
		if err != nil {
			t.Fatalf("分页查询失败: %v", err)
		}
		if pr.Total != 4 {
			t.Errorf("第 %d 页总数期望 4，实际 %d", page, pr.Total)
		}
		if len(pr.Records) != 1 {
			t.Fatalf("第 %d 页期望 1 条，实际 %d", page, len(pr.Records))
		}
		if seen[pr.Records[0].FullPath] {
			t.Errorf("第 %d 页重复返回 %s", page, pr.Records[0].FullPath)
		}
		seen[pr.Records[0].FullPath] = true
	}
	if len(seen) != 4 {
		t.Errorf("分页应覆盖 4 个文件，实际 %d", len(seen))
	}
}

// TestHotDownloads_EqualCountStableOrder 验证下载次数相同时的分页稳定性：
// 次序由文件身份决定，跨页不会重复或漏项。
func TestHotDownloads_EqualCountStableOrder(t *testing.T) {
	db := getMonitorTestDB(t)

	recs := []models.FileRecordPublic{
		{FileRecordBase: models.FileRecordBase{FileName: "x.txt", FilePath: "/x.txt", RootName: "r", FullPath: "/r/x.txt", FileSize: 1, Status: models.FileStatusActive}},
		{FileRecordBase: models.FileRecordBase{FileName: "y.txt", FilePath: "/y.txt", RootName: "r", FullPath: "/r/y.txt", FileSize: 1, Status: models.FileStatusActive}},
	}
	for i := range recs {
		if err := db.Create(&recs[i]).Error; err != nil {
			t.Fatalf("创建索引记录失败: %v", err)
		}
	}
	now := utils.Now()
	for _, r := range recs {
		op := models.OperationRecord{
			Action: "download", FileName: r.FileName, FilePath: r.FilePath, FullPath: r.FullPath,
			RootName: r.RootName, FileSize: r.FileSize, FileRecordID: r.ID,
			SourceType: models.SourceTypePublic, Status: models.RecordStatusSuccess, DlCounted: 1, CreatedAt: now,
		}
		if err := db.Create(&op).Error; err != nil {
			t.Fatalf("创建操作记录失败: %v", err)
		}
	}

	svc := services.NewMonitorService(repositories.NewRecordRepository(db), db)
	seen := make(map[string]bool)
	for page := 1; page <= 2; page++ {
		res, err := svc.GetHotDownloads(page, 1)
		if err != nil {
			t.Fatalf("分页查询失败: %v", err)
		}
		if len(res.Records) != 1 {
			t.Fatalf("第 %d 页期望 1 条，实际 %d", page, len(res.Records))
		}
		if seen[res.Records[0].FullPath] {
			t.Errorf("第 %d 页重复返回 %s", page, res.Records[0].FullPath)
		}
		seen[res.Records[0].FullPath] = true
	}
	if len(seen) != 2 {
		t.Errorf("期望覆盖 2 个文件，实际 %d", len(seen))
	}
}

// TestKeywords_WindowOrderingAndLimit 验证热门搜索词的统计口径：
// 仅统计近 keywordWindowDays（7）天内的 search 记录，按出现次数降序、词序升序；
// 窗口外的历史搜索、空关键词、非 search 操作一律不计；limit 超出上限时被收敛。
func TestKeywords_WindowOrderingAndLimit(t *testing.T) {
	db := getMonitorTestDB(t)
	now := utils.Now()

	ops := []models.OperationRecord{
		{Action: "search", SearchQuery: "report", CreatedAt: now},
		{Action: "search", SearchQuery: "report", CreatedAt: now},
		{Action: "search", SearchQuery: "invoice", CreatedAt: now},
		{Action: "search", SearchQuery: "alpha", CreatedAt: now},
		// 窗口外（8 天前）：不应计入
		{Action: "search", SearchQuery: "legacy", CreatedAt: now.AddDate(0, 0, -8)},
		{Action: "search", SearchQuery: "legacy", CreatedAt: now.AddDate(0, 0, -8)},
		// 空关键词与非搜索操作：不应计入
		{Action: "search", SearchQuery: "", CreatedAt: now},
		{Action: "download", SearchQuery: "report", CreatedAt: now},
	}
	for i := range ops {
		if err := db.Create(&ops[i]).Error; err != nil {
			t.Fatalf("创建操作记录失败: %v", err)
		}
	}

	svc := services.NewMonitorService(repositories.NewRecordRepository(db), db)
	got, err := svc.GetKeywords(20)
	if err != nil {
		t.Fatalf("获取关键词统计失败: %v", err)
	}

	want := []services.KeywordStat{
		{Word: "report", Count: 2},
		{Word: "alpha", Count: 1},
		{Word: "invoice", Count: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("期望 %d 个关键词，实际 %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个关键词期望 %+v，实际 %+v", i, want[i], got[i])
		}
	}

	// limit 正常生效
	limited, err := svc.GetKeywords(1)
	if err != nil {
		t.Fatalf("获取关键词统计失败: %v", err)
	}
	if len(limited) != 1 || limited[0].Word != "report" {
		t.Errorf("limit=1 期望仅返回 report，实际 %+v", limited)
	}

	// limit 超出上限/非法值时收敛到合法范围（不返回空）
	for _, l := range []int{0, -1, 10000} {
		res, err := svc.GetKeywords(l)
		if err != nil {
			t.Fatalf("limit=%d 获取关键词统计失败: %v", l, err)
		}
		if len(res) != len(want) {
			t.Errorf("limit=%d 期望 %d 个关键词，实际 %d", l, len(want), len(res))
		}
	}
}

// TestKeywords_CaseInsensitiveMerge 验证关键词按小写归一化统计：
// MySQL 默认排序规则大小写不敏感，SQLite/PostgreSQL 敏感，归一化后三库结果一致。
func TestKeywords_CaseInsensitiveMerge(t *testing.T) {
	db := getMonitorTestDB(t)
	now := utils.Now()
	for _, q := range []string{"NASA", "nasa"} {
		op := models.OperationRecord{Action: "search", SearchQuery: q, CreatedAt: now}
		if err := db.Create(&op).Error; err != nil {
			t.Fatalf("创建操作记录失败: %v", err)
		}
	}

	svc := services.NewMonitorService(repositories.NewRecordRepository(db), db)
	got, err := svc.GetKeywords(10)
	if err != nil {
		t.Fatalf("获取关键词统计失败: %v", err)
	}
	if len(got) != 1 || got[0].Word != "nasa" || got[0].Count != 2 {
		t.Errorf("期望合并为 {nasa 2}，实际 %+v", got)
	}
}

// TestMonitorHandler_AllEndpoints 测试所有端点同时访问
func TestMonitorHandler_AllEndpoints(t *testing.T) {
	db := getMonitorTestDB(t)
	recordRepo := repositories.NewRecordRepository(db)
	handler := monitor.NewHandler(services.NewMonitorService(recordRepo, db))

	router := gin.New()
	router.GET("/api/v1/web/monitor/storage", handler.Storage)
	router.GET("/api/v1/web/monitor/access", handler.Access)
	router.GET("/api/v1/web/monitor/keywords", handler.Keywords)
	router.GET("/api/v1/web/monitor/recent", handler.RecentKeywords)
	router.GET("/api/v1/web/monitor/rankings", handler.Rankings)

	endpoints := []struct {
		path string
		name string
	}{
		{"/api/v1/web/monitor/storage", "Storage"},
		{"/api/v1/web/monitor/access", "Access"},
		{"/api/v1/web/monitor/keywords", "Keywords"},
		{"/api/v1/web/monitor/recent", "RecentKeywords"},
		{"/api/v1/web/monitor/rankings", "Rankings"},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ep.path, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("%s: 期望状态码 200，实际 %d", ep.name, w.Code)
			}

			var response map[string]interface{}
			json.Unmarshal(w.Body.Bytes(), &response)

			if response["success"] != true {
				t.Errorf("%s: 期望 success 为 true", ep.name)
			}
		})
	}
}
