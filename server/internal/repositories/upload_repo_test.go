package repositories

import (
	"testing"
	"time"

	"fuzhan/internal/models"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func existingFileTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.OperationRecord{}, &models.FileRecordPublic{}); err != nil {
		t.Fatalf("自动迁移失败: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

func seedFile(t *testing.T, db *gorm.DB, name, rootName, filePath, fullPath string, status models.FileStatus) models.FileRecordPublic {
	t.Helper()
	f := models.FileRecordPublic{FileRecordBase: models.FileRecordBase{
		FileName:     name,
		FilePath:     filePath,
		RootName:     rootName,
		FullPath:     fullPath,
		Status:       status,
		ModTime:      time.Now(),
		LastSyncedAt: time.Now(),
	}}
	if err := db.Create(&f).Error; err != nil {
		t.Fatalf("创建文件索引失败: %v", err)
	}
	return f
}

func seedRecord(t *testing.T, db *gorm.DB, name, action string, uploadType models.TargetType, rootName, filePath, fullPath string, fileRecordID uint) {
	t.Helper()
	r := models.OperationRecord{
		FileName:     name,
		Action:       action,
		UploadType:   uploadType,
		RootName:     rootName,
		FilePath:     filePath,
		FullPath:     fullPath,
		FileRecordID: fileRecordID,
		CreatedAt:    time.Now(),
	}
	if err := db.Create(&r).Error; err != nil {
		t.Fatalf("创建操作记录失败: %v", err)
	}
}

// 校验"文件仍然存在"过滤的语义：仅当索引表仍存在对应 active 记录时才返回，
// 身份关联优先，历史记录回退到路径匹配；search 与 temp/private 记录不过滤。
func TestApplyExistingFileFilter_Semantics(t *testing.T) {
	db := existingFileTestDB(t)
	repo := NewRecordRepository(db)

	keep := seedFile(t, db, "keep.txt", "public", "/docs/keep.txt", "/public/docs/keep.txt", models.FileStatusActive)
	delStatus := seedFile(t, db, "delstatus.txt", "public", "/docs/delstatus.txt", "/public/docs/delstatus.txt", models.FileStatusDeleted)
	softDel := seedFile(t, db, "softdel.txt", "public", "/docs/softdel.txt", "/public/docs/softdel.txt", models.FileStatusActive)
	if err := db.Delete(&models.FileRecordPublic{}, softDel.ID).Error; err != nil {
		t.Fatalf("软删除文件索引失败: %v", err)
	}
	// 供路径回退分支匹配的活动文件
	seedFile(t, db, "exist.txt", "public", "/docs/exist.txt", "/public/docs/exist.txt", models.FileStatusActive)
	seedFile(t, db, "emptyfp.txt", "public", "/docs/emptyfp.txt", "", models.FileStatusActive)

	// 身份关联分支
	seedRecord(t, db, "keep.txt", "download", models.TargetTypeRegular, "public", "/docs/keep.txt", "/public/docs/keep.txt", keep.ID)
	seedRecord(t, db, "delstatus.txt", "download", models.TargetTypeRegular, "public", "/docs/delstatus.txt", "/public/docs/delstatus.txt", delStatus.ID)
	seedRecord(t, db, "softdel.txt", "download", models.TargetTypeRegular, "public", "/docs/softdel.txt", "/public/docs/softdel.txt", softDel.ID)
	// 路径回退分支（file_record_id = 0）
	seedRecord(t, db, "exist.txt", "download", models.TargetTypeRegular, "public", "/docs/exist.txt", "/public/docs/exist.txt", 0)
	seedRecord(t, db, "missing.txt", "download", models.TargetTypeRegular, "public", "/docs/missing.txt", "/public/docs/missing.txt", 0)
	seedRecord(t, db, "emptyfp.txt", "download", models.TargetTypeRegular, "public", "docs/emptyfp.txt", "", 0)
	// 非公共来源与非下载动作，均不应被过滤
	seedRecord(t, db, "temp-gone.bin", "download", models.TargetTypeTemp, "temp", "/gone.bin", "/temp/gone.bin", 0)
	seedRecord(t, db, "search-kw", "search", models.TargetTypeRegular, "", "", "", 0)

	downloads, err := repo.ListRecentAll(100, "download")
	if err != nil {
		t.Fatalf("查询下载记录失败: %v", err)
	}
	got := map[string]bool{}
	for _, r := range downloads {
		got[r.FileName] = true
	}

	want := []string{"keep.txt", "exist.txt", "emptyfp.txt", "temp-gone.bin"}
	notWant := []string{"delstatus.txt", "softdel.txt", "missing.txt"}

	for _, n := range want {
		if !got[n] {
			t.Errorf("期望返回 %q，实际未返回（保留集合: %v）", n, keys(got))
		}
	}
	for _, n := range notWant {
		if got[n] {
			t.Errorf("期望过滤掉 %q，实际仍返回", n)
		}
	}

	searches, err := repo.ListRecentAll(100, "search")
	if err != nil {
		t.Fatalf("查询搜索记录失败: %v", err)
	}
	if len(searches) != 1 || searches[0].FileName != "search-kw" {
		t.Errorf("search 记录不应被存在性过滤，期望 1 条 search-kw，实际 %d 条", len(searches))
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}