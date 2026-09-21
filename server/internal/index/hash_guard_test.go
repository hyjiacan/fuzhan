package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fuzhan/internal/models"
)

func hashTestRecord(t *testing.T, db interface {
	Create(interface{}) interface{ GetError() error }
}, root, name string, size int64) *models.FileRecordPublic {
	t.Helper()
	_ = db
	_ = root
	rec := &models.FileRecordPublic{
		FileRecordBase: models.FileRecordBase{
			FileName:   name,
			FilePath:   "/" + name,
			RootName:   "docs",
			FullPath:   "/docs/" + name,
			FileSize:   size,
			Status:     models.FileStatusActive,
			HashStatus: "pending",
		},
	}
	return rec
}

// 稳定文件：hash 写前后 size 一致 → 正常落库为 done 并写入 hash。
func TestHashWorker_StableFile_MarksDone(t *testing.T) {
	db := setupTestDB(t)
	root := t.TempDir()
	hw := NewHashWorker(db, map[string]string{"docs": root})

	content := "hello world"
	writeFile(t, filepath.Join(root, "f.bin"), content)
	rec := &models.FileRecordPublic{
		FileRecordBase: models.FileRecordBase{
			FileName:   "f.bin",
			FilePath:   "/f.bin",
			RootName:   "docs",
			FullPath:   "/docs/f.bin",
			FileSize:   int64(len(content)),
			Status:     models.FileStatusActive,
			HashStatus: "pending",
		},
	}
	if err := db.Create(rec).Error; err != nil {
		t.Fatal(err)
	}

	done, failed, skipped := hw.processHashBatch(context.Background(), "file_records_public",
		[]hashRecord{{ID: rec.ID, FilePath: "/f.bin", RootName: "docs"}})
	if done != 1 || failed != 0 || len(skipped) != 0 {
		t.Fatalf("期望 done=1 failed=0 skipped=0, 实际 done=%d failed=%d skipped=%d", done, failed, len(skipped))
	}

	var got models.FileRecordPublic
	if err := db.Where("id = ?", rec.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.HashStatus != "done" || got.Xxh3Hash == "" {
		t.Fatalf("稳定文件应标记 done 并写 hash, 实际 status=%q hash=%q", got.HashStatus, got.Xxh3Hash)
	}
}

// 计算期间文件被改动（模拟写入中读取）→ 本轮跳过、保留 pending，不固化半截 hash。
// 用 statSize 桩注入变更：首次返回旧 size，随后计算完成后返回新 size。
func TestHashWorker_ChangedDuringCompute_Skips(t *testing.T) {
	db := setupTestDB(t)
	root := t.TempDir()
	hw := NewHashWorker(db, map[string]string{"docs": root})

	content := "hello world changed"
	path := filepath.Join(root, "changing.bin")
	writeFile(t, path, content)

	// 桩替换 statSize：第一次(算前)返回旧大小，之后返回增大的大小，模拟写入中 size 变化
	realStat := statSize
	called := 0
	injected := func(p string) (int64, error) {
		s, err := realStat(p)
		if err != nil {
			return s, err
		}
		called++
		if called <= 1 {
			return s, nil // 算前：当前(较小的)真实大小
		}
		return s + 1000, nil // 算后：认为文件已增写 1000 字节
	}
	statSize = injected
	defer func() { statSize = realStat }()

	rec := &models.FileRecordPublic{
		FileRecordBase: models.FileRecordBase{
			FileName:   "changing.bin",
			FilePath:   "/changing.bin",
			RootName:   "docs",
			FullPath:   "/docs/changing.bin",
			FileSize:   int64(len(content)),
			Status:     models.FileStatusActive,
			HashStatus: "pending",
		},
	}
	if err := db.Create(rec).Error; err != nil {
		t.Fatal(err)
	}

	done, failed, skipped := hw.processHashBatch(context.Background(), "file_records_public",
		[]hashRecord{{ID: rec.ID, FilePath: "/changing.bin", RootName: "docs"}})
	if failed != 0 {
		t.Fatalf("写入中文件不应计为 failed, 实际 failed=%d", failed)
	}
	if done != 0 || len(skipped) != 1 {
		t.Fatalf("写入中被改动文件应被跳过(done=0 skipped=1), 实际 done=%d skipped=%d", done, len(skipped))
	}

	var got models.FileRecordPublic
	if err := db.Where("id = ?", rec.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.HashStatus != "pending" {
		t.Fatalf("写入中文件应保留 pending, 实际 status=%q hash=%q", got.HashStatus, got.Xxh3Hash)
	}
}

// MarkHashPending：把已完成 hash 的文件标记为待重算（供 onFileDirty 使用）。
func TestSyncer_MarkHashPending(t *testing.T) {
	syncer, root, _ := newSyncerEnv(t)
	writeFile(t, filepath.Join(root, "f.bin"), "data")
	if err := syncer.SyncFile("docs", "/f.bin"); err != nil {
		t.Fatal(err)
	}
	db := syncer.db
	if err := db.Model(&models.FileRecordPublic{}).
		Where("file_path = ?", "/f.bin").
		Update("hash_status", "done").Error; err != nil {
		t.Fatal(err)
	}
	var before models.FileRecordPublic
	if err := db.Where("file_path = ?", "/f.bin").First(&before).Error; err != nil {
		t.Fatal(err)
	}
	if before.HashStatus != "done" {
		t.Fatalf("前置条件失败: 应已标记 done, 实际 %q", before.HashStatus)
	}

	syncer.MarkHashPending("docs", "/f.bin")

	var after models.FileRecordPublic
	if err := db.Where("file_path = ?", "/f.bin").First(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after.HashStatus != "pending" {
		t.Fatalf("MarkHashPending 应置回 pending, 实际 %q", after.HashStatus)
	}
}

var _ = os.Getwd // keep os import if unused (removed below)

// 残差补算：主扫描中被判为"正在写入"而跳过的文件，在后续补算轮写完后再命中并固化。
// 注入 statSize：第一次 processHashBatch 内两次调用返回不一致 size → 跳过；
// 第二次（残差轮）两次调用一致 → 正常固化。
func TestHashWorker_ResidualPass_FillsDebt(t *testing.T) {
	db := setupTestDB(t)
	root := t.TempDir()
	hw := NewHashWorker(db, map[string]string{"docs": root})
	hw.ResidualDelay = 2 * time.Millisecond
	hw.MaxResidualBatches = 5

	content := "hello residual"
	path := filepath.Join(root, "growing.bin")
	writeFile(t, path, content)

	realStat := statSize
	called := 0
	injected := func(p string) (int64, error) {
		s, err := realStat(p)
		if err != nil {
			return s, err
		}
		called++
		if called == 2 { // 主扫描"算后"返回更大的 size → 判定仍在写入，跳过
			return s + 1000, nil
		}
		return s, nil
	}
	statSize = injected
	defer func() { statSize = realStat }()

	rec := &models.FileRecordPublic{
		FileRecordBase: models.FileRecordBase{
			FileName: "growing.bin", FilePath: "/growing.bin",
			RootName: "docs", FullPath: "/docs/growing.bin",
			FileSize: int64(len(content)), Status: models.FileStatusActive,
			HashStatus: "pending",
		},
	}
	if err := db.Create(rec).Error; err != nil {
		t.Fatal(err)
	}

	done, failed := hw.processTable(context.Background(), "file_records_public", 1)
	if done != 1 || failed != 0 {
		t.Fatalf("期望残差轮补齐 done=1 failed=0, 实际 done=%d failed=%d (called=%d)", done, failed, called)
	}

	var got models.FileRecordPublic
	if err := db.Where("id = ?", rec.ID).First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.HashStatus != "done" || got.Xxh3Hash == "" {
		t.Fatalf("残差补算后应固化 hash, 实际 status=%q hash=%q", got.HashStatus, got.Xxh3Hash)
	}
}