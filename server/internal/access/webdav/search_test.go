package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"fuzhan/internal/models"
)

// newSearchTestDB 构造一个带公开/私有索引表的内存库并写入测试数据。
func newSearchTestDB(t *testing.T, publicFiles, privateFiles int) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.FileRecordPublic{}, &models.FileRecordPrivate{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now()
	for i := 0; i < publicFiles; i++ {
		rec := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
			FileName: "report-2026.xlsx",
			FilePath: "/pub/report-2026.xlsx",
			RootName: "pub",
			FullPath: "/pub/report-2026.xlsx",
			IsDir:    false,
			Status:   models.FileStatusActive,
			ModTime:  now,
		}}
		if err := db.Create(&rec).Error; err != nil {
			t.Fatalf("create public: %v", err)
		}
	}
	for i := 0; i < privateFiles; i++ {
		rec := models.FileRecordPrivate{
			ShareCode: "codeshare00" + string(rune('0'+i)),
			FileRecordBase: models.FileRecordBase{
				FileName: "report-2026.xlsx",
				FilePath: "/2026/report-2026.xlsx",
				RootName: "private",
				FullPath: "/private/2026/report-2026.xlsx",
				IsDir:    false,
				Status:   models.FileStatusActive,
				OwnerID:  "user-uuid-1",
				ModTime:  now,
			},
		}
		if err := db.Create(&rec).Error; err != nil {
			t.Fatalf("create private: %v", err)
		}
	}
	return db
}

// authedContext 模拟 OptionalBasicAuthMiddleware 向请求上下文注入用户 UUID。
func authedContext(userUUID string) context.Context {
	return context.WithValue(context.Background(), ContextKeyUserUUID, userUUID)
}

func TestSearchableHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newSearchTestDB(t, 1, 1)

	h := &SearchableHandler{Standard: http.NotFoundHandler(), DB: db, Prefix: "/api/v1/webdav"}

	req := httptest.NewRequest("SEARCH", "/api/v1/webdav", strings.NewReader(
		`<?xml version="1.0"?><D:searchrequest xmlns:D="DAV:"><D:basicsearch><D:select><D:prop><D:displayname/></D:prop></D:select><D:where><D:like><D:prop><D:displayname/></D:prop><D:literal>2026</D:literal></D:like></D:where></D:basicsearch></D:searchrequest>`))
	req = req.WithContext(authedContext("user-uuid-1"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMultiStatus {
		t.Fatalf("SEARCH status = %d, want 207", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "/api/v1/webdav/public/pub/report-2026.xlsx") {
		t.Errorf("missing public href in SEARCH body: %s", body)
	}
	if !strings.Contains(body, "/api/v1/webdav/private/2026/report-2026.xlsx") {
		t.Errorf("missing private href in SEARCH body: %s", body)
	}
	if strings.Contains(body, "<D:collection/>") {
		t.Errorf("file should not be collection: %s", body)
	}
}

func TestSearchableHandlerAnonymous(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newSearchTestDB(t, 1, 1)

	h := &SearchableHandler{Standard: http.NotFoundHandler(), DB: db, Prefix: "/api/v1/webdav"}

	// 匿名：不应返回任何私有文件结果
	req := httptest.NewRequest("SEARCH", "/api/v1/webdav", strings.NewReader(
		`<?xml version="1.0"?><D:searchrequest xmlns:D="DAV:"><D:literal>2026</D:literal></D:searchrequest>`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "/api/v1/webdav/public/pub/report-2026.xlsx") {
		t.Errorf("missing public href in anonymous SEARCH: %s", body)
	}
	if strings.Contains(body, "/api/v1/webdav/private/") {
		t.Errorf("anonymous SEARCH leaked private href: %s", body)
	}
}

func TestSearchableHandlerEmptyKeyword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newSearchTestDB(t, 1, 1)

	h := &SearchableHandler{Standard: http.NotFoundHandler(), DB: db, Prefix: "/api/v1/webdav"}
	req := httptest.NewRequest("SEARCH", "/api/v1/webdav", strings.NewReader(`<D:searchrequest xmlns:D="DAV:"/>`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMultiStatus {
		t.Fatalf("status = %d, want 207", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "response") {
		t.Errorf("empty keyword should return empty multistatus: %s", rr.Body.String())
	}
}

func TestReportHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := newSearchTestDB(t, 1, 0)

	h := &SearchableHandler{Standard: http.NotFoundHandler(), DB: db, Prefix: "/api/v1/webdav"}

	// ownCloud 风格 REPORT：<oc:search-files> 内含 <oc:pattern>
	req := httptest.NewRequest("REPORT", "/api/v1/webdav", strings.NewReader(
		`<?xml version="1.0"?><oc:search-files xmlns:oc="http://owncloud.org/ns" xmlns:D="DAV:"><oc:pattern>2026</oc:pattern></oc:search-files>`))
	req = req.WithContext(authedContext("user-uuid-1"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMultiStatus {
		t.Fatalf("REPORT status = %d, want 207", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "report-2026.xlsx") {
		t.Errorf("REPORT should find file: %s", rr.Body.String())
	}
}

func TestIsReadOnlyMethodAllowsSearch(t *testing.T) {
	if !isReadOnlyMethod("SEARCH") || !isReadOnlyMethod("REPORT") {
		t.Fatal("SEARCH/REPORT should be read-only for readOnlyMiddleware")
	}
}
