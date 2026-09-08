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
