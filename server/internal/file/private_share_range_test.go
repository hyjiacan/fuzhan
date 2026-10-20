package file

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	configPkg "fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// setupPrivateShareDL 初始化私有分享下载测试环境：内存库 + 临时目录物理文件 + 分享码记录。
func setupPrivateShareDL(t *testing.T) (engine *gin.Engine, code string, content []byte, db *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:private_share_range_test?mode=memory&cache=shared"), &gorm.Config{
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.FileRecordPrivate{}, &models.OperationRecord{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}

	prevDB := configPkg.GetDB()
	prevCfg := configPkg.GlobalConfig
	configPkg.SetDB(db)
	basePath := t.TempDir()
	configPkg.GlobalConfig = configPkg.Config{}
	configPkg.GlobalConfig.Storage.Private.Path = basePath

	t.Cleanup(func() {
		configPkg.SetDB(prevDB)
		configPkg.GlobalConfig = prevCfg
		sqlDB.Close()
	})

	content = make([]byte, 100)
	for i := range content {
		content[i] = byte('a' + i%26)
	}
	userDir := filepath.Join(basePath, "users", "u1")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatalf("创建用户目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "test.bin"), content, 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	code = "0123456789ABCDEF0123456789ABCDEF"
	now := time.Now()
	rec := &models.FileRecordPrivate{ShareCode: code}
	rec.FileName = "test.bin"
	rec.FilePath = "/test.bin"
	rec.RootName = privateRootName
	rec.FullPath = "/" + privateRootName + "/test.bin"
	rec.FileSize = int64(len(content))
	rec.Status = models.FileStatusActive
	rec.OwnerID = "u1"
	rec.ModTime = now
	rec.LastSyncedAt = now
	rec.CreatedAt = now
	rec.UpdatedAt = now
	if err := db.Create(rec).Error; err != nil {
		t.Fatalf("创建私有记录失败: %v", err)
	}

	gin.SetMode(gin.TestMode)
	engine = gin.New()
	h := NewPrivateStorageHandlers()
	engine.GET("/share/:code", h.Download)
	engine.HEAD("/share/:code", h.Download)
	return
}

// countShareDownloadRecords 统计私有分享成功下载记录数（action=download 且来源为 private）。
func countShareDownloadRecords(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&models.OperationRecord{}).
		Where("action = ? AND source_type = ? AND status = ?",
			"download", models.SourceTypePrivate, models.RecordStatusSuccess).
		Count(&n).Error; err != nil {
		t.Fatalf("统计下载记录失败: %v", err)
	}
	return n
}

func doShareReq(engine *gin.Engine, path string, headers map[string]string) *httptest.ResponseRecorder {
	return doShareReqMethod(engine, http.MethodGet, path, headers)
}

func doShareReqMethod(engine *gin.Engine, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// TestPrivateShareDownloadRangeSupport 验证私有分享下载支持 Range：
// 完整请求返回 200 + Accept-Ranges: bytes；分段请求返回 206 + Content-Range；
// 非法区间返回 416；预览分支同样支持 Range。
func TestPrivateShareDownloadRangeSupport(t *testing.T) {
	engine, code, content, _ := setupPrivateShareDL(t)

	// 完整下载：200 + Accept-Ranges + Content-Length
	w := doShareReq(engine, "/share/"+code, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("完整下载应返回 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("缺少 Accept-Ranges: bytes, 实际 %q", got)
	}
	if got := w.Header().Get("Content-Length"); got != "100" {
		t.Errorf("Content-Length 应为 100, 实际 %q", got)
	}
	if !bytes.Equal(w.Body.Bytes(), content) {
		t.Error("完整下载内容不一致")
	}

	// 分段下载：206 + Content-Range
	w = doShareReq(engine, "/share/"+code, map[string]string{"Range": "bytes=10-19"})
	if w.Code != http.StatusPartialContent {
		t.Fatalf("Range 请求应返回 206, 实际 %d", w.Code)
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 10-19/100" {
		t.Errorf("Content-Range 错误: %q", got)
	}
	if !bytes.Equal(w.Body.Bytes(), content[10:20]) {
		t.Error("206 分段内容不一致")
	}

	// 非法区间：416
	w = doShareReq(engine, "/share/"+code, map[string]string{"Range": "bytes=5000-6000"})
	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("非法区间应返回 416, 实际 %d", w.Code)
	}

	// 预览分支：同样支持 Range，且不落下载记录
	w = doShareReq(engine, "/share/"+code+"?preview=true", map[string]string{"Range": "bytes=0-4"})
	if w.Code != http.StatusPartialContent {
		t.Fatalf("预览 Range 请求应返回 206, 实际 %d", w.Code)
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 0-4/100" {
		t.Errorf("预览 Content-Range 错误: %q", got)
	}
	if !bytes.Equal(w.Body.Bytes(), content[0:5]) {
		t.Error("预览分段内容不一致")
	}
	if cd := w.Header().Get("Content-Disposition"); cd != "" {
		t.Errorf("预览不应带 Content-Disposition, 实际 %q", cd)
	}
}

// TestPrivateShareDownloadRecordFilter 验证下载记录按续传语义过滤：
// 仅新下载（无 Range 或 Range 起始为 0 的 GET）写入成功记录；
// 续传分段、尾部区间、非法区间（416）、HEAD 探活与预览均不重复记录。
func TestPrivateShareDownloadRecordFilter(t *testing.T) {
	engine, code, _, db := setupPrivateShareDL(t)

	// 续传/分段/异常请求：全部不应产生成功下载记录
	for _, tc := range []struct {
		name    string
		method  string
		path    string
		headers map[string]string
	}{
		{"续传分段", http.MethodGet, "/share/" + code, map[string]string{"Range": "bytes=10-19"}},
		{"尾部区间", http.MethodGet, "/share/" + code, map[string]string{"Range": "bytes=-50"}},
		{"非法区间416", http.MethodGet, "/share/" + code, map[string]string{"Range": "bytes=5000-6000"}},
		{"HEAD探活", http.MethodHead, "/share/" + code, nil},
		{"预览", http.MethodGet, "/share/" + code + "?preview=true", nil},
	} {
		w := doShareReqMethod(engine, tc.method, tc.path, tc.headers)
		if w.Code >= 500 {
			t.Fatalf("%s 请求异常: %d", tc.name, w.Code)
		}
		if n := countShareDownloadRecords(t, db); n != 0 {
			t.Fatalf("%s 后不应有成功下载记录, 实际 %d 条", tc.name, n)
		}
	}

	// Range 起始为 0 视为新下载（多连接下载器首段），应记录一次
	w := doShareReq(engine, "/share/"+code, map[string]string{"Range": "bytes=0-49"})
	if w.Code != http.StatusPartialContent {
		t.Fatalf("bytes=0-49 应返回 206, 实际 %d", w.Code)
	}
	if n := countShareDownloadRecords(t, db); n != 1 {
		t.Fatalf("Range 起始为 0 应记录 1 条下载, 实际 %d 条", n)
	}

	// 完整下载（无 Range）：再记录一条
	w = doShareReq(engine, "/share/"+code, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("完整下载应返回 200, 实际 %d", w.Code)
	}
	if n := countShareDownloadRecords(t, db); n != 2 {
		t.Fatalf("完整下载应累计 2 条记录, 实际 %d 条", n)
	}
}
