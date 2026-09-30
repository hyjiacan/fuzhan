package integration

import (
	"testing"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/services"
)

// TestExpandRecordActions 验证"下载"记录类型展开为 download + download-by-hash，
// 其余类型原样返回（列表、删除、清空、计数重算共用同一口径）。
func TestExpandRecordActions(t *testing.T) {
	got := repositories.ExpandRecordActions("download")
	if len(got) != 2 || got[0] != "download" || got[1] != "download-by-hash" {
		t.Fatalf("download 应展开为 [download download-by-hash]，实际 %v", got)
	}
	for _, action := range []string{"upload", "search", "preview"} {
		if got := repositories.ExpandRecordActions(action); len(got) != 1 || got[0] != action {
			t.Errorf("%s 应原样返回，实际 %v", action, got)
		}
	}
}

// TestCollectRecountTargets_NonDownloadAction 验证非下载类型不产生重算目标：
// 上传/搜索/预览记录都不对应计数列，收集阶段直接返回空。
func TestCollectRecountTargets_NonDownloadAction(t *testing.T) {
	db := getTestDB(t)
	repo := repositories.NewRecordRepository(db)
	now := time.Now()

	rec := models.OperationRecord{
		Action: "upload", RootName: "rootA", FullPath: "/rootA/a.txt", FileName: "a.txt",
		SourceType: models.SourceTypeTemp, SourceID: 7, CreatedAt: now,
	}
	if err := repo.Create(&rec); err != nil {
		t.Fatalf("创建记录失败: %v", err)
	}

	for _, action := range []string{"upload", "search", "preview"} {
		paths, tempIDs, err := repo.CollectRecountTargets(action, 0)
		if err != nil {
			t.Fatalf("%s 收集失败: %v", action, err)
		}
		if len(paths) != 0 || len(tempIDs) != 0 {
			t.Errorf("%s 不应产生重算目标，实际 paths=%v tempIDs=%v", action, paths, tempIDs)
		}
	}
}

// TestClearDownloadRecords_RecountPublicCounts 验证清空下载记录后，公共文件下载次数按剩余记录回退。
// 覆盖：download 与 download-by-hash 同属"下载"；未计入计数（dl_counted=0）的记录不影响重算。
func TestClearDownloadRecords_RecountPublicCounts(t *testing.T) {
	db := getTestDB(t)
	repo := repositories.NewRecordRepository(db)
	now := time.Now()

	a := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		RootName: "rootA", FilePath: "/a.txt", FileName: "a.txt", FullPath: "/rootA/a.txt",
		FileSize: 1, ModTime: now, LastSyncedAt: now, DownloadCount: 2,
	}}
	b := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		RootName: "rootA", FilePath: "/b.txt", FileName: "b.txt", FullPath: "/rootA/b.txt",
		FileSize: 1, ModTime: now, LastSyncedAt: now, DownloadCount: 1,
	}}
	if err := db.Create(&a).Error; err != nil {
		t.Fatalf("创建索引失败: %v", err)
	}
	if err := db.Create(&b).Error; err != nil {
		t.Fatalf("创建索引失败: %v", err)
	}

	records := []models.OperationRecord{
		{Action: "download", RootName: "rootA", FullPath: "/rootA/a.txt", FileName: "a.txt", CreatedAt: now, DlCounted: 1},
		{Action: "download", RootName: "rootA", FullPath: "/rootA/a.txt", FileName: "a.txt", CreatedAt: now.Add(-time.Minute), DlCounted: 1},
		// 断点续传/分段延续：落记录但不计入计数
		{Action: "download", RootName: "rootA", FullPath: "/rootA/a.txt", FileName: "a.txt", CreatedAt: now.Add(-2 * time.Minute)},
		// 按哈希下载：与 download 同属"下载"，同样计入 /rootA/b.txt
		{Action: "download-by-hash", RootName: "rootA", FullPath: "/rootA/b.txt", FileName: "b.txt", CreatedAt: now, DlCounted: 1},
		// 预览不属于下载记录，清空下载时不应被删除
		{Action: "preview", RootName: "rootA", FullPath: "/rootA/a.txt", FileName: "a.txt", CreatedAt: now},
	}
	for i := range records {
		if err := repo.Create(&records[i]); err != nil {
			t.Fatalf("创建记录失败: %v", err)
		}
	}

	paths, tempIDs, err := repo.CollectRecountTargets("download", 0)
	if err != nil {
		t.Fatalf("收集重算目标失败: %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("应收集到 2 个公共路径，实际 %v", paths)
	}
	if len(tempIDs) != 0 {
		t.Errorf("不应收集到临时文件 ID，实际 %v", tempIDs)
	}

	count, err := repo.DeleteByAction("download")
	if err != nil {
		t.Fatalf("清空下载记录失败: %v", err)
	}
	if count != 4 {
		t.Errorf("应删除 4 条下载类记录（含 download-by-hash），实际 %d", count)
	}

	services.RecountPublicDownloadCounts(db, paths)
	services.RecountTempDownloadCounts(db, tempIDs)

	var gotA, gotB models.FileRecordPublic
	if err := db.First(&gotA, a.ID).Error; err != nil {
		t.Fatalf("查询 a.txt 失败: %v", err)
	}
	if err := db.First(&gotB, b.ID).Error; err != nil {
		t.Fatalf("查询 b.txt 失败: %v", err)
	}
	if gotA.DownloadCount != 0 {
		t.Errorf("a.txt 下载次数应回退为 0，实际 %d", gotA.DownloadCount)
	}
	if gotB.DownloadCount != 0 {
		t.Errorf("b.txt 下载次数应回退为 0，实际 %d", gotB.DownloadCount)
	}

	// 预览记录不受影响
	var previews int64
	if err := db.Model(&models.OperationRecord{}).Where("action = ?", "preview").Count(&previews).Error; err != nil {
		t.Fatalf("统计预览记录失败: %v", err)
	}
	if previews != 1 {
		t.Errorf("预览记录应保留 1 条，实际 %d", previews)
	}
}

// TestDeleteSingleDownloadRecord_RecountPublicCounts 验证删除单条下载记录后，
// 该文件下载次数按剩余记录精确回退（而不是整体清零）。
func TestDeleteSingleDownloadRecord_RecountPublicCounts(t *testing.T) {
	db := getTestDB(t)
	repo := repositories.NewRecordRepository(db)
	now := time.Now()

	file := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		RootName: "rootA", FilePath: "/a.txt", FileName: "a.txt", FullPath: "/rootA/a.txt",
		FileSize: 1, ModTime: now, LastSyncedAt: now, DownloadCount: 2,
	}}
	if err := db.Create(&file).Error; err != nil {
		t.Fatalf("创建索引失败: %v", err)
	}

	records := []models.OperationRecord{
		{Action: "download", RootName: "rootA", FullPath: "/rootA/a.txt", FileName: "a.txt", CreatedAt: now, DlCounted: 1},
		{Action: "download-by-hash", RootName: "rootA", FullPath: "/rootA/a.txt", FileName: "a.txt", CreatedAt: now.Add(-time.Minute), DlCounted: 1},
	}
	for i := range records {
		if err := repo.Create(&records[i]); err != nil {
			t.Fatalf("创建记录失败: %v", err)
		}
	}

	// 界面在"下载"页签下删除，传入的是 download；记录本身是 download-by-hash 也应被删除
	paths, tempIDs, err := repo.CollectRecountTargets("download", records[1].ID)
	if err != nil {
		t.Fatalf("收集重算目标失败: %v", err)
	}
	count, err := repo.DeleteByIDAndAction(records[1].ID, "download")
	if err != nil || count != 1 {
		t.Fatalf("应删除 1 条记录, count=%d err=%v", count, err)
	}

	services.RecountPublicDownloadCounts(db, paths)
	services.RecountTempDownloadCounts(db, tempIDs)

	var got models.FileRecordPublic
	if err := db.First(&got, file.ID).Error; err != nil {
		t.Fatalf("查询文件失败: %v", err)
	}
	if got.DownloadCount != 1 {
		t.Errorf("下载次数应回退为 1，实际 %d", got.DownloadCount)
	}
}

// TestClearDownloadRecords_RecountTempCounts 验证清空下载记录后，临时文件下载次数同步回退。
func TestClearDownloadRecords_RecountTempCounts(t *testing.T) {
	db := getTestDB(t)
	repo := repositories.NewRecordRepository(db)
	now := time.Now()

	tempFile := models.TempFile{
		Code: "abcd1234", Filename: "t.txt", FileSize: 1, FilePath: "t.txt",
		DownloadCount: 2, ExpiredAt: now.Add(time.Hour), CreatedAt: now,
	}
	if err := db.Create(&tempFile).Error; err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}

	records := []models.OperationRecord{
		{Action: "download", RootName: "temp", FullPath: "/temp/abcd1234", FileName: "t.txt",
			SourceType: models.SourceTypeTemp, SourceID: tempFile.ID, Status: models.RecordStatusSuccess, CreatedAt: now},
		{Action: "download", RootName: "temp", FullPath: "/temp/abcd1234", FileName: "t.txt",
			SourceType: models.SourceTypeTemp, SourceID: tempFile.ID, Status: models.RecordStatusSuccess, CreatedAt: now.Add(-time.Minute)},
		// 失败下载不计入临时文件下载次数
		{Action: "download", RootName: "temp", FullPath: "/temp/abcd1234", FileName: "t.txt",
			SourceType: models.SourceTypeTemp, SourceID: tempFile.ID, Status: models.RecordStatusFailed, CreatedAt: now.Add(-2 * time.Minute)},
	}
	for i := range records {
		if err := repo.Create(&records[i]); err != nil {
			t.Fatalf("创建记录失败: %v", err)
		}
	}

	paths, tempIDs, err := repo.CollectRecountTargets("download", 0)
	if err != nil {
		t.Fatalf("收集重算目标失败: %v", err)
	}
	// 临时下载不属于公开下载事件，不应进入公共文件重算路径
	if len(paths) != 0 {
		t.Errorf("临时下载不应产生公共路径，实际 %v", paths)
	}
	if len(tempIDs) != 1 || tempIDs[0] != tempFile.ID {
		t.Fatalf("应收集到临时文件 ID %d，实际 %v", tempFile.ID, tempIDs)
	}

	if _, err := repo.DeleteByAction("download"); err != nil {
		t.Fatalf("清空下载记录失败: %v", err)
	}
	services.RecountPublicDownloadCounts(db, paths)
	services.RecountTempDownloadCounts(db, tempIDs)

	var got models.TempFile
	if err := db.First(&got, tempFile.ID).Error; err != nil {
		t.Fatalf("查询临时文件失败: %v", err)
	}
	if got.DownloadCount != 0 {
		t.Errorf("临时文件下载次数应回退为 0，实际 %d", got.DownloadCount)
	}
}

// TestListDownloadRecords_IncludesHashDownload 验证"下载"列表与删除/清空同口径：
// 按哈希下载记录也应出现在下载列表中（否则会删除界面上看不到的记录）。
func TestListDownloadRecords_IncludesHashDownload(t *testing.T) {
	db := getTestDB(t)
	repo := repositories.NewRecordRepository(db)
	now := time.Now()

	file := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		RootName: "rootA", FilePath: "/a.txt", FileName: "a.txt", FullPath: "/rootA/a.txt",
		FileSize: 1, ModTime: now, LastSyncedAt: now,
	}}
	if err := db.Create(&file).Error; err != nil {
		t.Fatalf("创建索引失败: %v", err)
	}
	other := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		RootName: "rootA", FilePath: "/b.txt", FileName: "b.txt", FullPath: "/rootA/b.txt",
		FileSize: 1, ModTime: now, LastSyncedAt: now,
	}}
	if err := db.Create(&other).Error; err != nil {
		t.Fatalf("创建索引失败: %v", err)
	}

	records := []models.OperationRecord{
		{Action: "download", RootName: "rootA", FilePath: "a.txt", FullPath: "/rootA/a.txt", FileName: "a.txt", CreatedAt: now},
		{Action: "download-by-hash", RootName: "rootA", FilePath: "b.txt", FullPath: "/rootA/b.txt", FileName: "b.txt", CreatedAt: now.Add(-time.Minute)},
	}
	for i := range records {
		if err := repo.Create(&records[i]); err != nil {
			t.Fatalf("创建记录失败: %v", err)
		}
	}

	got, total, err := repo.ListRecentAllPaginated(1, 10, "download")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 2 || len(got) != 2 {
		t.Fatalf("下载列表应包含 2 条（download + download-by-hash），实际 total=%d len=%d", total, len(got))
	}
}
