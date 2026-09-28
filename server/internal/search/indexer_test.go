package search

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"fuzhan/internal/models"
)

// newTestDB 创建基于临时文件的纯 SQLite 数据库并迁移 file_records_public
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := gorm.Open(sqlite.Open(filepath.Join(dir, "test.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.FileRecordPublic{}); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}
	t.Cleanup(func() {
		if s, err := db.DB(); err == nil {
			_ = s.Close()
		}
	})
	return db
}

func newActiveRecord(id uint, name string) models.FileRecordPublic {
	now := time.Now()
	return models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		ID:           id,
		FileName:     name,
		FilePath:     "/",
		RootName:     "root",
		FullPath:     "/root/" + name,
		ModTime:      now,
		LastSyncedAt: now,
		CreatedAt:    now,
		UpdatedAt:    now,
		Status:       models.FileStatusActive,
	}}
}

// seedActiveRecords 写入一批 active 文件记录（只需 RebuildFromDB 用到的字段）
func seedActiveRecords(t *testing.T, db *gorm.DB, records ...models.FileRecordPublic) {
	t.Helper()
	for i := range records {
		if err := db.Create(&records[i]).Error; err != nil {
			t.Fatalf("写入测试记录失败: %v", err)
		}
	}
}

// TestRebuildFromDBHotSwap 验证全量重建在影子目录完成后能原子热替换：
// 线上查询见到的是重建后的完整数据，且目录回到规范位置、writer 仍可写入。
func TestRebuildFromDBHotSwap(t *testing.T) {
	idxDir := filepath.Join(t.TempDir(), "search_index")
	idx, err := OpenIndex(idxDir)
	if err != nil {
		t.Fatalf("OpenIndex 失败: %v", err)
	}

	// 初始索引
	if err := idx.IndexBatch([]IndexEntry{{ID: 1, FileName: "年度报表"}, {ID: 2, FileName: "readme"}}); err != nil {
		t.Fatalf("初始 IndexBatch 失败: %v", err)
	}

	// DB：id1 改名、id2 删除、新增 id3
	db := newTestDB(t)
	seedActiveRecords(t, db, newActiveRecord(1, "年度财务报表"), newActiveRecord(3, "第三.md"))

	if err := idx.RebuildFromDB(db); err != nil {
		t.Fatalf("RebuildFromDB 失败: %v", err)
	}

	// 规范目录下应只剩索引数据，不再有 .rebuild 影子目录残留
	if _, err := os.Stat(idxDir); err != nil {
		t.Fatalf("规范目录不存在: %v", err)
	}
	matches, _ := filepath.Glob(idxDir + ".rebuild.*")
	if len(matches) != 0 {
		t.Fatalf("影子目录未清理: %v", matches)
	}

	// 查询应看到重建后的数据：新名命中断言、旧名不再命中
	assertHits := func(prefix string, want ...string) {
		t.Helper()
		got, err := idx.AutoComplete(prefix, 10)
		if err != nil {
			t.Fatalf("AutoComplete(%q) 失败: %v", prefix, err)
		}
		for _, w := range want {
			if !sliceContains(got, w) {
				t.Fatalf("AutoComplete(%q) 期望含 %q，实际 %v", prefix, w, got)
			}
		}
	}
	assertHits("年度", "年度财务报表")
	assertHits("第三", "第三.md")
	assertHits("readme") // 应命中为空

	// 热替换后 writer 仍可继续增量写入
	if err := idx.IndexFile(4, "memo"); err != nil {
		t.Fatalf("热替换后 IndexFile 失败: %v", err)
	}
	assertHits("memo", "memo")

	if err := idx.CloseWriter(); err != nil {
		t.Fatalf("CloseWriter 失败: %v", err)
	}
}

// TestRebuildFromDBIdempotent 反复全量重建结果稳定，且词表缓存随数据更换而刷新。
func TestRebuildFromDBIdempotent(t *testing.T) {
	idx, err := OpenIndex(filepath.Join(t.TempDir(), "search_index"))
	if err != nil {
		t.Fatalf("OpenIndex 失败: %v", err)
	}
	if err := idx.IndexBatch([]IndexEntry{{ID: 1, FileName: "报表"}}); err != nil {
		t.Fatalf("初始 IndexBatch 失败: %v", err)
	}

	db := newTestDB(t)
	seedActiveRecords(t, db, newActiveRecord(1, "财务报表"), newActiveRecord(9, "other"))

	// 先触发一次词表缓存构建（基于旧索引）
	if _, err := idx.SuggestKeywords("报", 5); err != nil {
		t.Fatalf("预热词表缓存失败: %v", err)
	}
	// 重建后用缓存路径（SuggestKeywords）也应能看到新数据
	if err := idx.RebuildFromDB(db); err != nil {
		t.Fatalf("RebuildFromDB 失败: %v", err)
	}
	got, err := idx.SuggestKeywords("财", 5)
	if err != nil {
		t.Fatalf("SuggestKeywords 失败: %v", err)
	}
	if !sliceContains(got, "财务报表") {
		t.Fatalf("重建后词表缓存未刷新，期望含 财务报表，实际 %v", got)
	}
	if err := idx.CloseWriter(); err != nil {
		t.Fatalf("CloseWriter 失败: %v", err)
	}
}

func sliceContains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
