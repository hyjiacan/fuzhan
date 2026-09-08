package services

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(&models.UploadSession{}, &models.UploadedChunk{}, &models.OperationRecord{}); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}
	return db
}

func newTestService(t *testing.T) (*UploadSessionService, string) {
	t.Helper()

	chunkDir, err := os.MkdirTemp("", "upload-session-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	db := newTestDB(t)
	svc := NewUploadSessionService(db, 1024, nil)
	return svc, chunkDir
}

func createTestSession(t *testing.T, svc *UploadSessionService, rootDir, filename string, fileSize int64) *models.UploadSession {
	t.Helper()
	session, err := svc.CreateSession(&CreateSessionReq{
		Filename:   filename,
		FileSize:   fileSize,
		Dir:        "/",
		RootName:   "test-root",
		TargetType: models.TargetTypeRegular,
		UserID:     "test-user",
	})
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	return session
}

func TestCreateSession_Success(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup

	session := createTestSession(t, svc, cleanup, "test.txt", 3000)

	if session.FileName != "test.txt" {
		t.Errorf("expected filename test.txt, got %s", session.FileName)
	}
	if session.FileSize != 3000 {
		t.Errorf("expected file size 3000, got %d", session.FileSize)
	}
	if session.TotalChunks != 3 {
		t.Errorf("expected 3 chunks for 3000 bytes with 1024 chunk size, got %d", session.TotalChunks)
	}
	if session.Status != models.UploadStatusPending {
		t.Errorf("expected status pending, got %v", session.Status)
	}
	if session.ID == 0 {
		t.Errorf("expected non-zero session ID")
	}
	if session.ChunkDir == "" {
		t.Errorf("expected non-empty chunk dir")
	}
}

func TestCreateSession_DiskSpaceInsufficient(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup

	_, err := svc.CreateSession(&CreateSessionReq{
		Filename:   "huge.txt",
		FileSize:   1 << 62,
		Dir:        "/",
		RootName:   "test-root",
		TargetType: models.TargetTypeRegular,
		UserID:     "test-user",
	})
	if err == nil {
		t.Fatal("expected disk space error for huge file, got nil")
	}
}

func TestUploadChunk_Success(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup

	session := createTestSession(t, svc, cleanup, "test.txt", 2048)

	chunkData := strings.NewReader(strings.Repeat("A", 1024))
	err := svc.UploadChunk(&UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: 0,
		ChunkData:  io.NopCloser(chunkData),
		ChunkSize:  1024,
	})
	if err != nil {
		t.Fatalf("UploadChunk failed: %v", err)
	}

	chunkData2 := strings.NewReader(strings.Repeat("B", 1024))
	err = svc.UploadChunk(&UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: 1,
		ChunkData:  io.NopCloser(chunkData2),
		ChunkSize:  1024,
	})
	if err != nil {
		t.Fatalf("UploadChunk second chunk failed: %v", err)
	}

	status, err := svc.GetStatus(session.ID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if len(status.UploadedIndexes) != 2 {
		t.Errorf("expected 2 uploaded chunks, got %d", len(status.UploadedIndexes))
	}
}

func TestUploadChunk_InvalidSession(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	err := svc.UploadChunk(&UploadChunkReq{
		UploadID:   999,
		ChunkIndex: 0,
		ChunkData:  io.NopCloser(strings.NewReader("data")),
	})
	if err == nil {
		t.Fatal("expected error for invalid session, got nil")
	}
}

func TestUploadChunk_ExpiredSession(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "test.txt", 1024)

	repo := repositories.NewSessionRepository(svc.db)
	_ = repo.UpdateStatus(session.ID, models.UploadStatusExpired)

	err := svc.UploadChunk(&UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: 0,
		ChunkData:  io.NopCloser(strings.NewReader("data")),
	})
	if err == nil {
		t.Fatal("expected error for expired session, got nil")
	}
}

func TestUploadChunk_InvalidIndex(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "test.txt", 1024)

	err := svc.UploadChunk(&UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: 99,
		ChunkData:  io.NopCloser(strings.NewReader("data")),
	})
	if err == nil {
		t.Fatal("expected error for invalid chunk index, got nil")
	}
}

func TestUploadChunk_ChunkTooLarge(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "test.txt", 1024)

	chunkData := strings.NewReader(strings.Repeat("X", 2048))
	err := svc.UploadChunk(&UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: 0,
		ChunkData:  io.NopCloser(chunkData),
		ChunkSize:  2048,
	})
	if err == nil {
		t.Fatal("expected error for oversized chunk, got nil")
	}
}

func TestUploadChunk_ChecksumMismatch(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "test.txt", 1024)

	err := svc.UploadChunk(&UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: 0,
		ChunkData:  io.NopCloser(strings.NewReader("hello")),
		Checksum:   "deadbeef",
	})
	if err == nil {
		t.Fatal("expected checksum error, got nil")
	}
}

func TestGetStatus_Success(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "test.txt", 3000)

	status, err := svc.GetStatus(session.ID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}

	if status.ID != session.ID {
		t.Errorf("expected ID %d, got %d", session.ID, status.ID)
	}
	if status.FileName != "test.txt" {
		t.Errorf("expected test.txt, got %s", status.FileName)
	}
	if status.TotalChunks != 3 {
		t.Errorf("expected 3 chunks, got %d", status.TotalChunks)
	}
}

func TestGetStatus_Nonexistent(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	_, err := svc.GetStatus(999)
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}
}

func TestFinalize_Success(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "finalize_test.txt", 1024)

	chunkData := strings.NewReader(strings.Repeat("B", 1024))
	err := svc.UploadChunk(&UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: 0,
		ChunkData:  io.NopCloser(chunkData),
		ChunkSize:  1024,
	})
	if err != nil {
		t.Fatalf("UploadChunk failed: %v", err)
	}

	result, err := svc.Finalize(session.ID, false)
	if err != nil {
		t.Fatalf("Finalize failed: %v", err)
	}

	if result.FileName != "finalize_test.txt" {
		t.Errorf("expected finalize_test.txt, got %s", result.FileName)
	}

	targetPath := filepath.Join(cleanup, "finalize_test.txt")
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		t.Errorf("target file does not exist: %s", targetPath)
	}
}

func TestFinalize_MissingChunks(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "missing.txt", 2048)

	chunkData := strings.NewReader(strings.Repeat("C", 1024))
	_ = svc.UploadChunk(&UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: 0,
		ChunkData:  io.NopCloser(chunkData),
		ChunkSize:  1024,
	})

	_, err := svc.Finalize(session.ID, false)
	if err == nil {
		t.Fatal("expected error for missing chunks, got nil")
	}
}

func TestResume_Success(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "resume_test.txt", 1024)

	repo := repositories.NewSessionRepository(svc.db)
	_ = repo.UpdateStatus(session.ID, models.UploadStatusExpired)

	status, err := svc.Resume(session.ID)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}

	if status.Expired {
		t.Errorf("expected expired=false after resume")
	}
	if status.Status != models.UploadStatusInProgress {
		t.Errorf("expected status in-progress after resume, got %v", status.Status)
	}
}

func TestResume_CompletedSession(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "resume_fail.txt", 1024)

	repo := repositories.NewSessionRepository(svc.db)
	_ = repo.UpdateStatus(session.ID, models.UploadStatusCompleted)

	_, err := svc.Resume(session.ID)
	if err == nil {
		t.Fatal("expected error for completed session, got nil")
	}
}

func TestCancel_Success(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "cancel_test.txt", 1024)

	chunkData := strings.NewReader(strings.Repeat("D", 1024))
	_ = svc.UploadChunk(&UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: 0,
		ChunkData:  io.NopCloser(chunkData),
		ChunkSize:  1024,
	})

	err := svc.Cancel(session.ID)
	if err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	repo := repositories.NewSessionRepository(svc.db)
	_, err = repo.GetByID(session.ID)
	if err == nil {
		t.Errorf("expected session to be deleted")
	}
}

func TestCleanupExpired(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "cleanup_test.txt", 1024)

	svc.db.Model(&models.UploadSession{}).Where("id = ?", session.ID).Updates(map[string]interface{}{
		"expired_at": time.Now().UTC().Add(-time.Hour),
		"status":     models.UploadStatusInProgress,
	})

	_, err := svc.CleanupExpired()
	if err != nil {
		t.Fatalf("CleanupExpired failed: %v", err)
	}

	repo := repositories.NewSessionRepository(svc.db)
	updated, err := repo.GetByID(session.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if updated.Status != models.UploadStatusExpired {
		t.Errorf("expected status expired, got %v", updated.Status)
	}
}

func TestFinalize_NoUploadingFile(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "nofile.txt", 1024)

	_, err := svc.Finalize(session.ID, false)
	if err == nil {
		t.Fatal("expected error for missing chunks, got nil")
	}
}

func TestUploadChunk_ExpiredByTime(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "expired_by_time.txt", 1024)

	svc.db.Model(&models.UploadSession{}).Where("id = ?", session.ID).Update("expired_at", time.Now().UTC().Add(-time.Hour))

	err := svc.UploadChunk(&UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: 0,
		ChunkData:  io.NopCloser(strings.NewReader("data")),
	})
	if err == nil {
		t.Fatal("expected error for expired session, got nil")
	}
}

func TestUploadChunk_CompletedSession(t *testing.T) {
	svc, cleanup := newTestService(t)
	defer os.RemoveAll(cleanup)

	appconfig.RootNames["test-root"] = cleanup
	session := createTestSession(t, svc, cleanup, "completed_test.txt", 1024)

	repo := repositories.NewSessionRepository(svc.db)
	_ = repo.UpdateStatus(session.ID, models.UploadStatusCompleted)

	err := svc.UploadChunk(&UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: 0,
		ChunkData:  io.NopCloser(strings.NewReader("data")),
	})
	if err == nil {
		t.Fatal("expected error for completed session, got nil")
	}
}
