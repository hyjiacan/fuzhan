package file

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fuzhan/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newDepCountTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.FileRecordPublic{}, &models.FileDependency{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, e := db.DB(); e == nil {
			sqlDB.Close()
		}
	})
	return db
}

func newDepCountTestRecord(t *testing.T, db *gorm.DB, name, filePath string) uint {
	t.Helper()
	rec := models.FileRecordPublic{
		FileRecordBase: models.FileRecordBase{
			FileName:     name,
			FilePath:     filePath,
			RootName:     "docs",
			FullPath:     "/docs" + filePath,
			IsDir:        false,
			Status:       models.FileStatusActive,
			ModTime:      time.Now(),
			LastSyncedAt: time.Now(),
		},
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatalf("创建记录失败: %v", err)
	}
	return rec.ID
}

func TestLoadDependencyCounts(t *testing.T) {
	db := newDepCountTestDB(t)
	a := newDepCountTestRecord(t, db, "a.txt", "/a.txt")
	b := newDepCountTestRecord(t, db, "b.txt", "/b.txt")
	c := newDepCountTestRecord(t, db, "c.txt", "/sub/c.txt")

	for _, dep := range []models.FileDependency{
		{FileRecordID: a, DependsOnID: b, Relation: "requires"},
		{FileRecordID: a, DependsOnID: c, Relation: "requires"},
	} {
		if err := db.Create(&dep).Error; err != nil {
			t.Fatalf("创建依赖失败: %v", err)
		}
	}

	fh := &FileHandlers{DB: db}
	counts, err := fh.loadDependencyCounts("docs", "")
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if counts[a] != 2 || counts[b] != 1 || counts[c] != 0 {
		t.Fatalf("根目录计数不符: %+v", counts)
	}

	// 列表返回结构必须带上依赖计数，且为 0 时也要有字段
	items, err := fh.listDirectChildren("docs", "", "", true)
	if err != nil {
		t.Fatalf("列出目录失败: %v", err)
	}
	byRecord := make(map[uint]FileRecordInfo, len(items))
	for _, item := range items {
		byRecord[item.RecordID] = item
	}
	if byRecord[a].DependencyCount != 2 || byRecord[b].DependencyCount != 1 || byRecord[c].DependencyCount != 0 {
		t.Fatalf("列表返回的依赖计数不符: %+v", items)
	}
	raw, err := json.Marshal(byRecord[c])
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(string(raw), `"dependencyCount":0`) {
		t.Fatalf("响应 JSON 缺少 dependencyCount 字段: %s", raw)
	}

	sub, err := fh.loadDependencyCounts("docs", "sub")
	if err != nil {
		t.Fatalf("子目录统计失败: %v", err)
	}
	if sub[c] != 1 || sub[a] != 0 {
		t.Fatalf("子目录计数不符: %+v", sub)
	}

	// 对端文件被删除后不计入，保持与依赖树展示口径一致
	if err := db.Delete(&models.FileRecordPublic{}, c).Error; err != nil {
		t.Fatalf("删除记录失败: %v", err)
	}
	counts, err = fh.loadDependencyCounts("docs", "")
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if counts[a] != 1 || counts[b] != 1 {
		t.Fatalf("对端删除后计数不符: %+v", counts)
	}
}
