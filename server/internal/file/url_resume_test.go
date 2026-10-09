package file

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// srvRange / srvIfRange 记录测试服务器收到的续传请求头。
var srvRange, srvIfRange string

// noopAudio 实现 AuditStore 的 no-op 桩，供 downloadFromURL 集成测试使用。
type noopAudit struct{}

func (noopAudit) Create(_ *models.OperationRecord) error          { return nil }
func (noopAudit) CreateBatch(_ []*models.OperationRecord) error   { return nil }
func (noopAudit) GetByID(_ uint) (*models.OperationRecord, error) { return nil, nil }
func (noopAudit) ListByUserID(_ string, _, _ int) ([]models.OperationRecord, int64, error) {
	return nil, 0, nil
}
func (noopAudit) ListByIP(_ string, _, _ int) ([]models.OperationRecord, int64, error) {
	return nil, 0, nil
}
func (noopAudit) ListRecent(_ string, _ int) ([]models.OperationRecord, error)    { return nil, nil }
func (noopAudit) ListRecentAll(_ int, _ string) ([]models.OperationRecord, error) { return nil, nil }
func (noopAudit) ListRecentAllPaginated(_, _ int, _ string) ([]models.OperationRecord, int64, error) {
	return nil, 0, nil
}
func (noopAudit) ResolvePublicFileID(_, _ string) (uint, error) { return 0, nil }
func (noopAudit) UpdateFileRecordID(_, _ uint) error            { return nil }
func (noopAudit) AttachCurrentPaths(r []models.OperationRecord) []models.OperationRecord {
	return r
}
func (noopAudit) Delete(_ uint) error                                       { return nil }
func (noopAudit) DeleteByIDAndAction(_ uint, _ string) (int64, error)       { return 0, nil }
func (noopAudit) DeleteByFile(_, _ string, _ bool, _ []uint) (int64, error) { return 0, nil }
func (noopAudit) GetAll() ([]models.OperationRecord, error)                 { return nil, nil }
func (noopAudit) GetRecent(_ int) ([]models.OperationRecord, error)         { return nil, nil }
func (noopAudit) GetRecentSearches(_ int) ([]models.OperationRecord, error) { return nil, nil }
func (noopAudit) DeleteByAction(_ string) (int64, error)                    { return 0, nil }

// 集成测试：预置部分文件后调用 downloadFromURL，应发 Range 请求从断点续传，
// 最终目标文件字节与远端完全一致，任务完成。
func TestDownloadFromURL_Resume(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 16*1024) // 256KB
	offset := int64(len(payload) / 2)
	etag := `"resume-etag-1"`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srvRange = r.Header.Get("Range")
		srvIfRange = r.Header.Get("If-Range")
		w.Header().Set("ETag", etag)
		http.ServeContent(w, r, "f.bin", time.Time{}, bytes.NewReader(payload))
	}))
	defer srv.Close()

	db := resumeTestDB(t)
	targetPath := filepath.Join(t.TempDir(), "f.bin")
	partial := filepath.Join(filepath.Dir(targetPath), ".f.bin.t-1.uploading")
	// 预置前一半为部分文件，模拟下载中断
	if err := os.WriteFile(partial, payload[:offset], 0644); err != nil {
		t.Fatal(err)
	}

	urlTask := &models.URLDownloadTask{
		ID: "t-1", URL: srv.URL + "/f", FileName: "f.bin",
		FileSize: int64(len(payload)), StorageType: models.URLDownloadStorageRegular,
		Status: models.URLDownloadStatusDownloading, TargetPath: targetPath,
		TempFilePath: partial, ResumeSupported: true, ETag: etag,
	}
	if err := db.Create(urlTask).Error; err != nil {
		t.Fatal(err)
	}

	// 允许本机回环，避免 SSRF 防护拦截测试服务器
	oldCfg := appconfig.GlobalConfig.Upload.URLUpload
	appconfig.GlobalConfig.Upload.URLUpload.Enabled = true
	appconfig.GlobalConfig.Upload.URLUpload.AllowedIPRanges = []string{"127.0.0.1/32"}
	defer func() { appconfig.GlobalConfig.Upload.URLUpload = oldCfg }()

	h := &UploadSessionHandler{
		db: db, urlTaskRepo: repositories.NewURLDownloadTaskRepository(db),
		recordRepo: noopAudit{},
	}
	h.downloadFromURL("t-1", urlTask.URL, urlTask.FileSize, "f.bin", "", partial, targetPath, "", "", models.TargetTypeRegular)

	if srvRange != "bytes="+strconv.FormatInt(offset, 10)+"-" {
		t.Fatalf("应发送 Range 续传请求, got %q", srvRange)
	}
	if srvIfRange != etag {
		t.Fatalf("应携带 If-Range(=ETag) 校验资源未变, got %q", srvIfRange)
	}

	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("目标文件未生成: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("续传后目标文件内容错误: got %d bytes, want %d", len(got), len(payload))
	}

	finalTask, _ := repositories.NewURLDownloadTaskRepository(db).GetByID("t-1")
	if finalTask == nil || finalTask.Status != models.URLDownloadStatusCompleted {
		t.Fatalf("任务应完成, got %v", finalTask)
	}
	// 部分文件应已被原子重命名，不再残留
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("部分文件应已清理, stat err=%v", err)
	}
}

// 服务器资源已变（忽略 Range 返回 200）时，应从头全量重下。
func TestDownloadFromURL_Resume_ServerChanged(t *testing.T) {
	payload := bytes.Repeat([]byte("xyzw"), 8*1024) // 32KB

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srvRange = r.Header.Get("Range")
		// 忽略 Range，始终返回完整 200
		w.Write(payload)
	}))
	defer srv.Close()

	db := resumeTestDB(t)
	targetPath := filepath.Join(t.TempDir(), "g.bin")
	partial := filepath.Join(filepath.Dir(targetPath), ".g.bin.t-2.uploading")
	if err := os.WriteFile(partial, []byte("stale-partial"), 0644); err != nil {
		t.Fatal(err)
	}

	urlTask := &models.URLDownloadTask{
		ID: "t-2", URL: srv.URL + "/g", FileName: "g.bin",
		FileSize: int64(len(payload)), StorageType: models.URLDownloadStorageRegular,
		Status: models.URLDownloadStatusDownloading, TargetPath: targetPath,
		TempFilePath: partial, ResumeSupported: true, ETag: `"old"`,
	}
	if err := db.Create(urlTask).Error; err != nil {
		t.Fatal(err)
	}

	oldCfg := appconfig.GlobalConfig.Upload.URLUpload
	appconfig.GlobalConfig.Upload.URLUpload.Enabled = true
	appconfig.GlobalConfig.Upload.URLUpload.AllowedIPRanges = []string{"127.0.0.1/32"}
	defer func() { appconfig.GlobalConfig.Upload.URLUpload = oldCfg }()

	h := &UploadSessionHandler{
		db: db, urlTaskRepo: repositories.NewURLDownloadTaskRepository(db),
		recordRepo: noopAudit{},
	}
	h.downloadFromURL("t-2", urlTask.URL, urlTask.FileSize, "g.bin", "", partial, targetPath, "", "", models.TargetTypeRegular)

	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("目标文件未生成: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("资源变化后应全量重下: got %d bytes, want %d", len(got), len(payload))
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Fatalf("全量重下后部分文件应被替换清理, stat err=%v", err)
	}
}

func resumeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.URLDownloadTask{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return db
}
