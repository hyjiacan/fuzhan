package index

import (
	"path/filepath"
	"testing"
	"time"

	"fuzhan/internal/models"
)

// TestDedupePublicFileRecords 验证重复行合并：保留最早行、字段合并、引用重定向、软删与幂等。
func TestDedupePublicFileRecords(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.OperationRecord{}); err != nil {
		t.Fatalf("迁移操作记录表失败: %v", err)
	}
	now := time.Now()

	base := func(filePath string, size int64) models.FileRecordBase {
		return models.FileRecordBase{
			FileName:     filepath.Base(filePath),
			FilePath:     filePath,
			RootName:     "docs",
			FullPath:     "/docs" + filePath,
			FileSize:     size,
			ModTime:      now,
			Status:       models.FileStatusActive,
			LastSyncedAt: now,
		}
	}

	first := models.FileRecordPublic{FileRecordBase: base("/a.bin", 100)}
	first.HashStatus = "pending"
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("写入保留行失败: %v", err)
	}
	second := models.FileRecordPublic{FileRecordBase: base("/a.bin", 100)}
	second.UploaderIP = "10.0.0.1"
	second.Xxh3Hash = "hash-a"
	second.HashStatus = "done"
	second.DownloadCount = 5
	second.LastSyncedAt = now.Add(time.Minute)
	if err := db.Create(&second).Error; err != nil {
		t.Fatalf("写入重复行失败: %v", err)
	}
	other := models.FileRecordPublic{FileRecordBase: base("/b.bin", 200)}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("写入无关行失败: %v", err)
	}
	// 保留行自身带一次下载，重复行 5 次，合并后应为 6
	db.Model(&models.FileRecordPublic{}).Where("id = ?", first.ID).Update("download_count", 1)

	op := models.OperationRecord{
		FileName:     "a.bin",
		RootName:     "docs",
		FilePath:     "/a.bin",
		FullPath:     "/docs/a.bin",
		SourceType:   models.SourceTypePublic,
		SourceID:     second.ID,
		FileRecordID: second.ID,
		Action:       "upload",
		UploadTime:   now,
	}
	if err := db.Create(&op).Error; err != nil {
		t.Fatalf("写入操作记录失败: %v", err)
	}
	dep := models.FileDependency{FileRecordID: second.ID, DependsOnID: other.ID, Relation: "requires"}
	if err := db.Create(&dep).Error; err != nil {
		t.Fatalf("写入依赖失败: %v", err)
	}

	DedupePublicFileRecords(db)

	var active []models.FileRecordPublic
	if err := db.Where("root_name = ? AND file_path = ?", "docs", "/a.bin").Find(&active).Error; err != nil {
		t.Fatalf("查询活跃行失败: %v", err)
	}
	if len(active) != 1 || active[0].ID != first.ID {
		t.Fatalf("应只保留 id=%d 的一条记录，实际: %+v", first.ID, active)
	}
	keep := active[0]
	if keep.UploaderIP != "10.0.0.1" || keep.Xxh3Hash != "hash-a" || keep.HashStatus != "done" {
		t.Fatalf("字段合并异常: ip=%q hash=%q status=%q", keep.UploaderIP, keep.Xxh3Hash, keep.HashStatus)
	}
	if keep.DownloadCount != 6 {
		t.Fatalf("下载次数应合并为 6，实际 %d", keep.DownloadCount)
	}

	var dup models.FileRecordPublic
	if err := db.Unscoped().First(&dup, second.ID).Error; err != nil {
		t.Fatalf("查询被删行失败: %v", err)
	}
	if !dup.DeletedAt.Valid || dup.Status != models.FileStatusDeleted {
		t.Fatalf("重复行应被软删: %+v", dup)
	}

	var reloadedOp models.OperationRecord
	if err := db.First(&reloadedOp, op.ID).Error; err != nil {
		t.Fatalf("查询操作记录失败: %v", err)
	}
	if reloadedOp.FileRecordID != first.ID || reloadedOp.SourceID != first.ID {
		t.Fatalf("操作记录引用未重定向: file=%d source=%d", reloadedOp.FileRecordID, reloadedOp.SourceID)
	}
	var reloadedDep models.FileDependency
	if err := db.First(&reloadedDep, dep.ID).Error; err != nil {
		t.Fatalf("查询依赖失败: %v", err)
	}
	if reloadedDep.FileRecordID != first.ID {
		t.Fatalf("依赖引用未重定向: %d", reloadedDep.FileRecordID)
	}

	// 幂等：再次执行不应产生任何变化
	DedupePublicFileRecords(db)
	var count int64
	db.Model(&models.FileRecordPublic{}).Where("root_name = ? AND file_path = ?", "docs", "/a.bin").Count(&count)
	if count != 1 {
		t.Fatalf("重复执行后活跃行应为 1，实际 %d", count)
	}

	// 唯一性保障：SQLite/PostgreSQL 下"活跃路径"部分唯一索引生效，再插重复行应被拒绝
	models.EnsureFileRecordIndexes(db)
	duplicate := models.FileRecordPublic{FileRecordBase: base("/a.bin", 100)}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatalf("活跃路径唯一索引未生效，重复插入未被拒绝")
	}
}

// TestSyncFileCollapsesDuplicates 验证 SyncFile 写库后会收敛同路径的历史重复行，
// 且检索索引使用保留行的 ID。
func TestSyncFileCollapsesDuplicates(t *testing.T) {
	syncer, root, notifier := newSyncerEnv(t)
	now := time.Now()

	first := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		FileName: "dup.bin", FilePath: "/dup.bin", RootName: "docs", FullPath: "/docs/dup.bin",
		FileSize: 1, ModTime: now, Status: models.FileStatusActive, LastSyncedAt: now,
	}}
	if err := syncer.db.Create(&first).Error; err != nil {
		t.Fatalf("写入保留行失败: %v", err)
	}
	second := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		FileName: "dup.bin", FilePath: "/dup.bin", RootName: "docs", FullPath: "/docs/dup.bin",
		FileSize: 1, ModTime: now, Status: models.FileStatusActive, LastSyncedAt: now,
	}}
	second.UploaderIP = "192.168.1.10"
	if err := syncer.db.Create(&second).Error; err != nil {
		t.Fatalf("写入重复行失败: %v", err)
	}

	writeFile(t, filepath.Join(root, "dup.bin"), "data")
	if err := syncer.SyncFile("docs", "/dup.bin"); err != nil {
		t.Fatalf("SyncFile 失败: %v", err)
	}

	var active []models.FileRecordPublic
	if err := syncer.db.Where("root_name = ? AND file_path = ?", "docs", "/dup.bin").Find(&active).Error; err != nil {
		t.Fatalf("查询活跃行失败: %v", err)
	}
	if len(active) != 1 || active[0].ID != first.ID {
		t.Fatalf("同步后应只保留 id=%d 的一条记录，实际: %+v", first.ID, active)
	}
	if active[0].UploaderIP != "192.168.1.10" {
		t.Fatalf("上传 IP 应合并到保留行，实际 %q", active[0].UploaderIP)
	}

	adds, _ := notifier.snapshot()
	if _, ok := adds[int64(first.ID)]; !ok {
		t.Fatalf("检索索引应使用保留行 ID=%d，实际 %v", first.ID, adds)
	}
}
