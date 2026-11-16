package services

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
)

// 搜索功能已从文件系统遍历迁移至数据库索引查询。
// 数据库搜索的完整测试需要 SQLite 或 MySQL 环境，暂不在单元测试中覆盖。
// 相关功能通过集成环境进行验证。

func TestSearchServicePlaceholder(t *testing.T) {
	t.Log("搜索功能已迁移至数据库索引查询")
}

// newDepCountSearchDB 构建含公开文件记录与依赖表的临时库
func newDepCountSearchDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.FileRecordPublic{}, &models.FileDependency{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	t.Cleanup(func() {
		if s, e := db.DB(); e == nil {
			_ = s.Close()
		}
	})
	return db
}

func newDepCountSearchRecord(t *testing.T, db *gorm.DB, name string) uint {
	t.Helper()
	now := time.Now()
	rec := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		FileName: name, FilePath: "/" + name, RootName: "docs", FullPath: "/docs/" + name,
		Status: models.FileStatusActive, ModTime: now, LastSyncedAt: now,
	}}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("创建记录失败: %v", err)
	}
	return rec.ID
}

// TestLoadDependencyCounts 验证批量统计的计数口径（上游 + 下游、对端失效排除、空入参）。
func TestLoadDependencyCounts(t *testing.T) {
	db := newDepCountSearchDB(t)
	a := newDepCountSearchRecord(t, db, "a.txt")
	b := newDepCountSearchRecord(t, db, "b.txt")
	c := newDepCountSearchRecord(t, db, "c.txt")
	d := newDepCountSearchRecord(t, db, "d.txt")

	deps := []models.FileDependency{
		{FileRecordID: a, DependsOnID: b, Relation: "requires"},
		{FileRecordID: c, DependsOnID: a, Relation: "requires"},
		{FileRecordID: c, DependsOnID: d, Relation: "requires"},
	}
	for _, dep := range deps {
		if err := db.Create(&dep).Error; err != nil {
			t.Fatalf("创建依赖失败: %v", err)
		}
	}

	svc := NewSearchService(db)
	counts, err := svc.LoadDependencyCounts([]uint{a, b, c, d})
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	// a: 上游 1（依赖 b）+ 下游 1（被 c 依赖）= 2
	// b: 下游 1（被 a 依赖）
	// c: 上游 2
	// d: 下游 1
	if counts[a] != 2 || counts[b] != 1 || counts[c] != 2 || counts[d] != 1 {
		t.Fatalf("计数不符: %+v", counts)
	}

	// 只查子集时，不应出现未请求的 ID
	subset, err := svc.LoadDependencyCounts([]uint{b})
	if err != nil {
		t.Fatalf("子集统计失败: %v", err)
	}
	if len(subset) != 1 || subset[b] != 1 {
		t.Fatalf("子集计数不符: %+v", subset)
	}

	// 对端记录被软删除后不计入，保持与依赖树展示口径一致
	if err := db.Delete(&models.FileRecordPublic{}, b).Error; err != nil {
		t.Fatalf("删除记录失败: %v", err)
	}
	counts, err = svc.LoadDependencyCounts([]uint{a, c})
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if counts[a] != 1 || counts[c] != 2 {
		t.Fatalf("对端删除后计数不符: %+v", counts)
	}

	// 对端 status=deleted 但记录仍存在时照常计入（依赖树不看 status，只判断记录是否存在）
	if err := db.Model(&models.FileRecordPublic{}).Where("id = ?", d).
		Update("status", models.FileStatusDeleted).Error; err != nil {
		t.Fatalf("更新状态失败: %v", err)
	}
	counts, err = svc.LoadDependencyCounts([]uint{c})
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if counts[c] != 2 {
		t.Fatalf("对端非 active 时计数不符: %+v", counts)
	}

	// 空入参不产生查询
	empty, err := svc.LoadDependencyCounts(nil)
	if err != nil {
		t.Fatalf("空入参统计失败: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("空入参应返回空映射: %+v", empty)
	}
}

// TestSearchFilesDependencyCount 验证搜索结果确实携带 dependencyCount，
// 且计数为 0 时字段依然出现在 JSON 中（前端靠该字段决定是否显示"依赖 (n)"）。
func TestSearchFilesDependencyCount(t *testing.T) {
	db := newDepCountSearchDB(t)
	a := newDepCountSearchRecord(t, db, "report.txt")
	b := newDepCountSearchRecord(t, db, "other.txt")
	if err := db.Create(&models.FileDependency{FileRecordID: a, DependsOnID: b, Relation: "requires"}).Error; err != nil {
		t.Fatalf("创建依赖失败: %v", err)
	}

	svc := NewSearchService(db)
	roots := []appconfig.DirectoryConfig{{Path: "/data/docs"}}
	items, err := svc.SearchFiles("txt", roots, 5*time.Second)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 2 条结果，实际 %d: %+v", len(items), items)
	}

	byID := make(map[uint]appconfig.FileInfo, len(items))
	for _, item := range items {
		byID[item.RecordID] = item
	}
	for id, want := range map[uint]int64{a: 1, b: 1} {
		if byID[id].DependencyCount != want {
			t.Fatalf("记录 %d 依赖计数不符: %+v", id, byID[id])
		}
	}

	// 再加一条无依赖的记录，验证 0 值也输出字段
	c := newDepCountSearchRecord(t, db, "empty.txt")
	items, err = svc.SearchFiles("empty.txt", roots, 5*time.Second)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	var target appconfig.FileInfo
	for _, item := range items {
		if item.RecordID == c {
			target = item
		}
	}
	if target.RecordID == 0 {
		t.Fatalf("未找到记录 %d: %+v", c, items)
	}
	raw, err := json.Marshal(target)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(string(raw), `"dependency_count":0`) {
		t.Fatalf("搜索结果 JSON 缺少 dependency_count 字段: %s", raw)
	}
}
