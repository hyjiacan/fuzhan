package integration

import (
	"testing"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
)

// TestListRecentAllPaginated_SearchDedup 验证最近搜索记录按关键词去重：
// 相同关键词只保留最新一次，不同关键词各自展示（此前所有搜索记录会合并为一条）。
func TestListRecentAllPaginated_SearchDedup(t *testing.T) {
	db := getTestDB(t)
	repo := repositories.NewRecordRepository(db)
	now := time.Now()

	ops := []models.OperationRecord{
		{Action: "search", SearchQuery: "golang", CreatedAt: now.Add(-time.Hour), ClientIP: "1.1.1.1"},
		{Action: "search", SearchQuery: "golang", CreatedAt: now, ClientIP: "2.2.2.2"},
		{Action: "search", SearchQuery: "python", CreatedAt: now.Add(-2 * time.Hour), ClientIP: "1.1.1.1"},
		{Action: "search", SearchQuery: "爬虫", CreatedAt: now.Add(-3 * time.Hour), ClientIP: "3.3.3.3"},
	}
	for _, rec := range ops {
		if err := repo.Create(&rec); err != nil {
			t.Fatalf("创建记录失败: %v", err)
		}
	}

	got, total, err := repo.ListRecentAllPaginated(1, 10, "search")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	// golang/python/爬虫 三个不同关键词各留一条
	if total != 3 {
		t.Errorf("expected total=3 (按关键词去重), got %d", total)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 records, got %d", len(got))
	}
	if len(got) > 0 && got[0].SearchQuery != "golang" {
		t.Errorf("最新一条应为 golang，实际 %s", got[0].SearchQuery)
	}
	// 相同关键词只保留最新一条（ClientIP 应为 2.2.2.2）
	for _, r := range got {
		if r.SearchQuery == "golang" && r.ClientIP != "2.2.2.2" {
			t.Errorf("golang 应保留最新一条 (ClientIP=2.2.2.2)，实际 %s", r.ClientIP)
		}
	}
}

// TestDeleteByIDAndAction 验证按 ID + 类型删除单条操作记录，类型不匹配时不删除。
func TestDeleteByIDAndAction(t *testing.T) {
	db := getTestDB(t)
	repo := repositories.NewRecordRepository(db)
	now := time.Now()

	rec := models.OperationRecord{Action: "upload", RootName: "rootA", FilePath: "a.txt", FileName: "a.txt", CreatedAt: now}
	if err := repo.Create(&rec); err != nil {
		t.Fatalf("创建记录失败: %v", err)
	}

	// 类型不匹配：不删除
	if cnt, err := repo.DeleteByIDAndAction(rec.ID, "download"); err != nil || cnt != 0 {
		t.Fatalf("类型不匹配应删除0条, cnt=%d err=%v", cnt, err)
	}
	// 类型匹配：删除
	if cnt, err := repo.DeleteByIDAndAction(rec.ID, "upload"); err != nil || cnt != 1 {
		t.Fatalf("匹配类型应删除1条, cnt=%d err=%v", cnt, err)
	}
}

// TestDeleteByFile 验证文件删除时级联清理上传/下载记录：
// 按 file_id 身份关联的记录（含移动后路径快照）与路径快照一致的历史记录都会被清理。
func TestDeleteByFile(t *testing.T) {
	db := getTestDB(t)
	repo := repositories.NewRecordRepository(db)
	now := time.Now()

	// 创建索引记录作为 file_id 身份
	idx := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		RootName: "rootB", FilePath: "/docs/a.pdf", FileName: "a.pdf", FileSize: 1, ModTime: now, LastSyncedAt: now}}
	if err := db.Create(&idx).Error; err != nil {
		t.Fatalf("创建索引失败: %v", err)
	}

	ops := []models.OperationRecord{
		// 已关联身份（移动过，路径快照是旧路径），应被 file_id 命中
		{Action: "upload", RootName: "rootB", FilePath: "old/a.pdf", FileName: "a.pdf", FileRecordID: idx.ID, FullPath: "/rootB/old/a.pdf", CreatedAt: now},
		// 未关联身份的历史上传记录，路径匹配当前文件，应被路径命中
		{Action: "upload", RootName: "rootB", FilePath: "docs/a.pdf", FileName: "a.pdf", FullPath: "/rootB/docs/a.pdf", CreatedAt: now.Add(-time.Hour)},
		// 同目录下的下载记录，应被路径命中
		{Action: "download", RootName: "rootB", FilePath: "docs/a.pdf", FileName: "a.pdf", FullPath: "/rootB/docs/a.pdf", CreatedAt: now.Add(-2 * time.Hour)},
		// 其他文件记录，不应被删除
		{Action: "upload", RootName: "rootB", FilePath: "docs/b.txt", FileName: "b.txt", FullPath: "/rootB/docs/b.txt", CreatedAt: now},
		// 搜索记录，不应被删除
		{Action: "search", SearchQuery: "a.pdf", CreatedAt: now},
		// 不同根目录的同名记录，不应被删除
		{Action: "upload", RootName: "rootC", FilePath: "docs/a.pdf", FileName: "a.pdf", FullPath: "/rootC/docs/a.pdf", CreatedAt: now},
	}
	for i := range ops {
		r := ops[i]
		if err := repo.Create(&r); err != nil {
			t.Fatalf("创建记录失败: %v", err)
		}
	}

	cnt, err := repo.DeleteByFile("rootB", "/docs/a.pdf", false, []uint{idx.ID})
	if err != nil {
		t.Fatalf("级联删除失败: %v", err)
	}
	// 应删除：2 条 path 记录（含下载）+ 1 条 file_id 关联 = 3；保留 4 条
	if cnt != 3 {
		t.Errorf("expected delete 3, got %d", cnt)
	}

	var total int64
	if err := db.Model(&models.OperationRecord{}).Count(&total).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if total != 3 {
		t.Errorf("expected remaining 3 records, got %d", total)
	}
}
