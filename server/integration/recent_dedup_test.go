package integration

import (
	"testing"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
)

// TestListRecentAllPaginated_Dedup 验证最近记录分页在 SQL 层去重：
// 相同文件（根目录+路径+文件名）只保留最近一条，且不影响分页。
func TestListRecentAllPaginated_Dedup(t *testing.T) {
	db := getTestDB(t)
	repo := repositories.NewRecordRepository(db)

	uploadTime := time.Now().Add(-time.Minute)
	now := time.Now()
	records := []models.OperationRecord{
		// 同一文件 b/报告.pdf 上传了 3 次，应只保留最新一条（createdAt=now 那条）
		{Action: "upload", RootName: "rootA", FilePath: "b/报告.pdf", FileName: "报告.pdf", UploadTime: uploadTime, CreatedAt: now},
		{Action: "upload", RootName: "rootA", FilePath: "b/报告.pdf", FileName: "报告.pdf", UploadTime: uploadTime, CreatedAt: now.Add(-2 * time.Hour)},
		{Action: "upload", RootName: "rootA", FilePath: "b/报告.pdf", FileName: "报告.pdf", UploadTime: uploadTime, CreatedAt: now.Add(-3 * time.Hour)},
		// 不同文件正常返回
		{Action: "upload", RootName: "rootA", FilePath: "c/data.csv", FileName: "data.csv", CreatedAt: now.Add(-1 * time.Hour)},
		{Action: "upload", RootName: "rootB", FilePath: "plan.txt", FileName: "plan.txt", CreatedAt: now.Add(-2 * time.Hour)},
		// 下载操作不应与上传混在同一 action 下
		{Action: "download", RootName: "rootA", FilePath: "b/报告.pdf", FileName: "报告.pdf", UploadTime: uploadTime, CreatedAt: now},
	}
	for _, rec := range records {
		if err := repo.Create(&rec); err != nil {
			t.Fatalf("创建记录失败: %v", err)
		}
	}

	// 插入对应的公共文件索引记录（active），使"文件存在"过滤放行
	indexFiles := []models.FileRecordPublic{
		{FileRecordBase: models.FileRecordBase{RootName: "rootA", FilePath: "/b/报告.pdf", FileName: "报告.pdf", FileSize: 1, ModTime: uploadTime, LastSyncedAt: now}},
		{FileRecordBase: models.FileRecordBase{RootName: "rootA", FilePath: "/c/data.csv", FileName: "data.csv", FileSize: 1, ModTime: uploadTime, LastSyncedAt: now}},
		{FileRecordBase: models.FileRecordBase{RootName: "rootB", FilePath: "/plan.txt", FileName: "plan.txt", FileSize: 1, ModTime: uploadTime, LastSyncedAt: now}},
	}
	for i := range indexFiles {
		if err := db.Create(&indexFiles[i]).Error; err != nil {
			t.Fatalf("创建文件索引失败: %v", err)
		}
	}

	// 上传：应去重为 3 条不同文件
	got, total, err := repo.ListRecentAllPaginated(1, 10, "upload")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 3 {
		t.Errorf("expected total=3, got %d", total)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 records, got %d", len(got))
	}
	if got[0].FileName != "报告.pdf" {
		t.Errorf("最新的一条应为 报告.pdf，实际 %s", got[0].FileName)
	}
	if got[0].FilePath != "b/报告.pdf" {
		t.Errorf("保留的应为 b/报告.pdf，实际 %s", got[0].FilePath)
	}

	// 跨页验证：pageSize=2 时第2页补位仍为去重后的文件
	got2, total2, err := repo.ListRecentAllPaginated(2, 2, "upload")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total2 != 3 {
		t.Errorf("expected page2 total=3, got %d", total2)
	}
	if len(got2) != 1 {
		t.Fatalf("expected page2 1 record, got %d", len(got2))
	}
	if got2[0].FileName != "plan.txt" {
		t.Errorf("第2页应为 plan.txt，实际 %s", got2[0].FileName)
	}

	// 下载：仅 1 条，不被上传记录干扰
	gotDl, totalDl, err := repo.ListRecentAllPaginated(1, 10, "download")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if totalDl != 1 || len(gotDl) != 1 {
		t.Errorf("expected download total=1 len=1, got total=%d len=%d", totalDl, len(gotDl))
	}
}

// TestListRecentAllPaginated_DeletedFileExcluded 验证已删除文件不再出现在最近列表：
// 索引表记录被软删除（status=deleted）或已不存在（硬删除）时，对应操作记录被过滤。
func TestListRecentAllPaginated_DeletedFileExcluded(t *testing.T) {
	db := getTestDB(t)
	repo := repositories.NewRecordRepository(db)
	now := time.Now()

	ops := []models.OperationRecord{
		{Action: "upload", RootName: "rootX", FilePath: "a/exist.txt", FileName: "exist.txt", CreatedAt: now},
		{Action: "upload", RootName: "rootX", FilePath: "a/gone.txt", FileName: "gone.txt", CreatedAt: now.Add(-time.Hour)},
		{Action: "upload", RootName: "rootX", FilePath: "a/missing.txt", FileName: "missing.txt", CreatedAt: now.Add(-2 * time.Hour)},
	}
	for _, rec := range ops {
		if err := repo.Create(&rec); err != nil {
			t.Fatalf("创建记录失败: %v", err)
		}
	}

	indexFiles := []models.FileRecordPublic{
		// exist.txt：active，应显示
		{FileRecordBase: models.FileRecordBase{RootName: "rootX", FilePath: "/a/exist.txt", FileName: "exist.txt", FileSize: 1, ModTime: now, LastSyncedAt: now}},
		// gone.txt：软删除，应过滤
		{FileRecordBase: models.FileRecordBase{RootName: "rootX", FilePath: "/a/gone.txt", FileName: "gone.txt", FileSize: 1, ModTime: now, LastSyncedAt: now, Status: models.FileStatusDeleted}},
		// missing.txt：不再插入索引记录，模拟硬删除
	}
	for i := range indexFiles {
		if err := db.Create(&indexFiles[i]).Error; err != nil {
			t.Fatalf("创建文件索引失败: %v", err)
		}
	}

	got, total, err := repo.ListRecentAllPaginated(1, 10, "upload")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1 (仅保留仍存在的文件), got %d", total)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 record, got %d", len(got))
	}
	if got[0].FileName != "exist.txt" {
		t.Errorf("期望仅返回 exist.txt，实际 %s", got[0].FileName)
	}
}

// TestListRecentAllPaginated_DedupByFileID 验证按 file_id 身份去重：
// 同一文件移动/重命名后，历史操作记录的路径快照不同，但 file_record_id 相同，
// 去重后仅保留最新一条，并经 AttachCurrentPaths 显示文件当前路径。
func TestListRecentAllPaginated_DedupByFileID(t *testing.T) {
	db := getTestDB(t)
	repo := repositories.NewRecordRepository(db)
	now := time.Now()

	// 两个不同的文件索引记录（active），其当前路径分别为新位置
	movedIndex := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		RootName: "rootZ", FilePath: "/moved/report.pdf", FileName: "report.pdf",
		FileSize: 2, ModTime: now, LastSyncedAt: now}}
	otherIndex := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		RootName: "rootZ", FilePath: "/b/other.txt", FileName: "other.txt",
		FileSize: 1, ModTime: now, LastSyncedAt: now}}
	if err := db.Create(&movedIndex).Error; err != nil {
		t.Fatalf("创建 movedIndex 失败: %v", err)
	}
	if err := db.Create(&otherIndex).Error; err != nil {
		t.Fatalf("创建 otherIndex 失败: %v", err)
	}

	ops := []models.OperationRecord{
		// 同一 file 的两条历史记录：旧路径快照 + 新路径快照，file_record_id 相同
		{Action: "upload", RootName: "rootZ", FilePath: "old/a.pdf", FileName: "a.pdf", FileRecordID: movedIndex.ID, CreatedAt: now.Add(-3 * time.Hour)},
		{Action: "upload", RootName: "rootZ", FilePath: "moved/report.pdf", FileName: "report.pdf", FileRecordID: movedIndex.ID, CreatedAt: now.Add(-time.Hour)},
		// 另一个文件的记录
		{Action: "upload", RootName: "rootZ", FilePath: "b/other.txt", FileName: "other.txt", FileRecordID: otherIndex.ID, CreatedAt: now},
	}
	for _, rec := range ops {
		if err := repo.Create(&rec); err != nil {
			t.Fatalf("创建记录失败: %v", err)
		}
	}

	got, total, err := repo.ListRecentAllPaginated(1, 10, "upload")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 2 {
		t.Errorf("expected total=2 (同一 file_id 合并为一条), got %d", total)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 records, got %d", len(got))
	}
	// 最新的一条是 other.txt；moved 文件合并为一条
	foundMoved := false
	for _, r := range got {
		if r.FileRecordID == movedIndex.ID {
			foundMoved = true
			if r.FileName != "report.pdf" {
				t.Errorf("合并后应按当前索引显示 report.pdf，实际 %s", r.FileName)
			}
			if r.FilePath != "/moved/report.pdf" {
				t.Errorf("合并后应显示当前路径 /moved/report.pdf，实际 %s", r.FilePath)
			}
		}
	}
	if !foundMoved {
		t.Error("缺少 file_id=移动到新位置 的记录")
	}
}
