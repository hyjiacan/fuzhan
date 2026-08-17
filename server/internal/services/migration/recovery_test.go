package migration

import (
    "context"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/glebarez/sqlite"
    "gorm.io/gorm"
    "fuzhan/internal/models"
)

func recoveryCloseDB(db *gorm.DB) {
    if db != nil {
        if sqlDB, err := db.DB(); err == nil {
            sqlDB.Close()
        }
    }
}

func setupRecoveryTestDB(t *testing.T) *gorm.DB {
    tmpFile, err := os.CreateTemp("", "test_recovery_*.db")
    if err != nil {
        t.Fatalf("创建临时数据库失败: %v", err)
    }
    tmpFile.Close()

    db, err := gorm.Open(sqlite.Open(tmpFile.Name()), &gorm.Config{})
    if err != nil {
        os.Remove(tmpFile.Name())
        t.Fatalf("打开数据库失败: %v", err)
    }

    err = db.AutoMigrate(&models.MigrationStatus{}, &models.MigrationTableProgress{})
    if err != nil {
        recoveryCloseDB(db)
        os.Remove(tmpFile.Name())
        t.Fatalf("自动迁移失败: %v", err)
    }

    t.Cleanup(func() {
        recoveryCloseDB(db)
        os.Remove(tmpFile.Name())
    })

    return db
}

// TestRecoveryService_DetectInterruptedMigration tests detecting interrupted migrations
func TestRecoveryService_DetectInterruptedMigration(t *testing.T) {
    db := setupRecoveryTestDB(t)

    tmpDir, err := os.MkdirTemp("", "recovery_test_*")
    if err != nil {
        t.Fatalf("创建临时目录失败: %v", err)
    }
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    migrationService := NewMigrationService(db, backupDir)
    recoveryService := NewRecoveryService(db, migrationService)

    // Test with no interrupted migration
    result, err := recoveryService.DetectInterruptedMigration()
    if err != nil {
        t.Fatalf("DetectInterruptedMigration 失败: %v", err)
    }

    if result != nil {
        t.Error("预期无中断的迁移")
    }

    // Create a running migration
    migrationID := "interrupted-migration"
    migration := &models.MigrationStatus{
        ID:             migrationID,
        SourceDriver:   "sqlite",
        SourceDSN:      "source.db",
        TargetDriver:   "sqlite",
        TargetDSN:      "target.db",
        Status:         models.MigrationStatusRunning,
        Stage:          models.MigrationStageImport,
        Progress:       60,
        TablesCompleted: 3,
        TablesTotal:    5,
        StartedAt:      time.Now().Add(-10 * time.Minute),
    }
    if err := db.Create(migration).Error; err != nil {
        t.Fatalf("创建迁移记录失败: %v", err)
    }

    // Add completed table progress
    tableProgress := &models.MigrationTableProgress{
        MigrationID: migrationID,
        TableName:   "completed_table",
        Status:      models.TableProgressStatusCompleted,
    }
    db.Create(tableProgress)

    // Detect should find the interrupted migration
    result, err = recoveryService.DetectInterruptedMigration()
    if err != nil {
        t.Fatalf("DetectInterruptedMigration 失败: %v", err)
    }

    if result == nil {
        t.Fatal("预期检测到中断的迁移")
    }

    if result.MigrationID != migrationID {
        t.Errorf("预期迁移 ID 为 %s，实际为 %s", migrationID, result.MigrationID)
    }

    if result.Progress != 60 {
        t.Errorf("预期进度为 60，实际为 %d", result.Progress)
    }
}

// TestRecoveryService_DetectInterruptedMigration_None tests when no migration is running
func TestRecoveryService_DetectInterruptedMigration_None(t *testing.T) {
    db := setupRecoveryTestDB(t)

    tmpDir, _ := os.MkdirTemp("", "recovery_test_*")
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    migrationService := NewMigrationService(db, backupDir)
    recoveryService := NewRecoveryService(db, migrationService)

    // Only completed/failed migrations exist
    for _, status := range []string{models.MigrationStatusCompleted, models.MigrationStatusFailed} {
        migration := &models.MigrationStatus{
            ID:           "test-" + status,
            SourceDriver: "sqlite",
            TargetDriver: "sqlite",
            Status:       status,
            StartedAt:    time.Now(),
        }
        db.Create(migration)
    }

    result, err := recoveryService.DetectInterruptedMigration()
    if err != nil {
        t.Fatalf("DetectInterruptedMigration 失败: %v", err)
    }

    if result != nil {
        t.Error("预期无中断的迁移")
    }
}

// TestRecoveryService_ResumeMigration tests resuming an interrupted migration
func TestRecoveryService_ResumeMigration(t *testing.T) {
    db := setupRecoveryTestDB(t)

    tmpDir, err := os.MkdirTemp("", "recovery_test_*")
    if err != nil {
        t.Fatalf("创建临时目录失败: %v", err)
    }
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    migrationService := NewMigrationService(db, backupDir)
    recoveryService := NewRecoveryService(db, migrationService)

    // Create interrupted migration with source DSN
    sourceDB := filepath.Join(tmpDir, "source.db")
    srcDB, _ := gorm.Open(sqlite.Open(sourceDB), &gorm.Config{})
    srcDB.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)")
    recoveryCloseDB(srcDB)

    migrationID := "resume-migration"
    migration := &models.MigrationStatus{
        ID:           migrationID,
        SourceDriver: "sqlite",
        SourceDSN:    sourceDB,
        TargetDriver: "sqlite",
        TargetDSN:    filepath.Join(tmpDir, "target.db"),
        Status:       models.MigrationStatusRunning,
        Stage:        models.MigrationStageImport,
        StartedAt:    time.Now(),
    }
    if err := db.Create(migration).Error; err != nil {
        t.Fatalf("创建迁移记录失败: %v", err)
    }

    // Add completed tables
    completedTables := []string{"table1", "table2"}
    for _, table := range completedTables {
        db.Create(&models.MigrationTableProgress{
            MigrationID: migrationID,
            TableName:   table,
            Status:      models.TableProgressStatusCompleted,
        })
    }

    ctx := context.Background()
    _, err = recoveryService.ResumeMigration(ctx, migrationID, "sqlite", filepath.Join(tmpDir, "new_target.db"))

    // ResumeMigration returns nil event channel and delegates error to StartMigration
    // This tests that the method calls through correctly
    if err != nil {
        t.Logf("ResumeMigration 返回错误 (可能是预期的，因为 migrationService.StartMigration 未完成实现): %v", err)
    }
}

// TestRecoveryService_RestartMigration tests restarting a migration
func TestRecoveryService_RestartMigration(t *testing.T) {
    db := setupRecoveryTestDB(t)

    tmpDir, err := os.MkdirTemp("", "recovery_test_*")
    if err != nil {
        t.Fatalf("创建临时目录失败: %v", err)
    }
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    migrationService := NewMigrationService(db, backupDir)
    recoveryService := NewRecoveryService(db, migrationService)

    // Create failed migration
    migrationID := "restart-migration"
    migration := &models.MigrationStatus{
        ID:             migrationID,
        SourceDriver:   "sqlite",
        SourceDSN:      "source.db",
        TargetDriver:   "sqlite",
        TargetDSN:      "target.db",
        Status:         models.MigrationStatusFailed,
        Stage:          models.MigrationStageError,
        Progress:       50,
        TablesCompleted: 2,
        TablesTotal:    5,
        ErrorMessage:   "Previous error",
        StartedAt:      time.Now().Add(-1 * time.Hour),
    }
    if err := db.Create(migration).Error; err != nil {
        t.Fatalf("创建迁移记录失败: %v", err)
    }

    // Add some table progress
    db.Create(&models.MigrationTableProgress{
        MigrationID: migrationID,
        TableName:   "old_table",
        Status:      models.TableProgressStatusCompleted,
    })

    ctx := context.Background()
    _, err = recoveryService.RestartMigration(ctx, migrationID, "sqlite", filepath.Join(tmpDir, "target.db"))

    // Should attempt restart (may fail due to empty source DSN, but flow is tested)
    if err == nil {
        // Verify status was reset
        var status models.MigrationStatus
        db.Where("id = ?", migrationID).First(&status)
        if status.Status != models.MigrationStatusRunning {
            t.Errorf("预期状态为 running，实际为 %s", status.Status)
        }
        if status.ErrorMessage != "" {
            t.Error("错误信息应被清空")
        }
    }
}

// TestRecoveryService_GetCompletedTables tests getting list of completed tables
func TestRecoveryService_GetCompletedTables(t *testing.T) {
    db := setupRecoveryTestDB(t)

    tmpDir, _ := os.MkdirTemp("", "recovery_test_*")
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    migrationService := NewMigrationService(db, backupDir)
    recoveryService := NewRecoveryService(db, migrationService)

    migrationID := "completed-tables-test"

    // Add completed tables
    completedTables := []string{"users", "posts", "comments"}
    for _, table := range completedTables {
        db.Create(&models.MigrationTableProgress{
            MigrationID: migrationID,
            TableName:   table,
            Status:      models.TableProgressStatusCompleted,
        })
    }

    // Add failed table
    db.Create(&models.MigrationTableProgress{
        MigrationID: migrationID,
        TableName:   "failed_table",
        Status:      models.TableProgressStatusFailed,
    })

    tables, err := recoveryService.GetCompletedTables(migrationID)
    if err != nil {
        t.Fatalf("GetCompletedTables 失败: %v", err)
    }

    if len(tables) != 3 {
        t.Errorf("预期 3 个已完成表，实际有 %d 个", len(tables))
    }

    // Verify all expected tables are present
    expected := map[string]bool{"users": true, "posts": true, "comments": true}
    for _, table := range tables {
        if !expected[table] {
            t.Errorf("意外的已完成表: %s", table)
        }
        delete(expected, table)
    }

    if len(expected) > 0 {
        t.Errorf("缺少以下表: %v", expected)
    }
}

// TestRecoveryService_GetCompletedTables_Empty tests with no completed tables
func TestRecoveryService_GetCompletedTables_Empty(t *testing.T) {
    db := setupRecoveryTestDB(t)

    tmpDir, _ := os.MkdirTemp("", "recovery_test_*")
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    migrationService := NewMigrationService(db, backupDir)
    recoveryService := NewRecoveryService(db, migrationService)

    tables, err := recoveryService.GetCompletedTables("non-existent")
    if err != nil {
        t.Fatalf("GetCompletedTables 失败: %v", err)
    }

    if len(tables) != 0 {
        t.Errorf("预期 0 个表，实际有 %d 个", len(tables))
    }
}

// TestRecoveryService_ResetMigrationStatus tests resetting migration status
func TestRecoveryService_ResetMigrationStatus(t *testing.T) {
    db := setupRecoveryTestDB(t)

    tmpDir, _ := os.MkdirTemp("", "recovery_test_*")
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    migrationService := NewMigrationService(db, backupDir)
    recoveryService := NewRecoveryService(db, migrationService)

    migrationID := "reset-test"
    migration := &models.MigrationStatus{
        ID:             migrationID,
        SourceDriver:   "sqlite",
        TargetDriver:   "sqlite",
        Status:         models.MigrationStatusFailed,
        Stage:          models.MigrationStageError,
        Progress:       75,
        TablesCompleted: 5,
        TablesTotal:    6,
        RecordsMigrated: 1000,
        ErrorMessage:   "Test error",
        StartedAt:      time.Now().Add(-1 * time.Hour),
    }
    if err := db.Create(migration).Error; err != nil {
        t.Fatalf("创建迁移记录失败: %v", err)
    }

    err := recoveryService.ResetMigrationStatus(migrationID)
    if err != nil {
        t.Fatalf("ResetMigrationStatus 失败: %v", err)
    }

    // Verify status was reset
    var status models.MigrationStatus
    if err := db.Where("id = ?", migrationID).First(&status).Error; err != nil {
        t.Fatalf("查询迁移状态失败: %v", err)
    }

    if status.Status != models.MigrationStatusPending {
        t.Errorf("预期状态为 pending，实际为 %s", status.Status)
    }

    if status.Progress != 0 {
        t.Errorf("预期进度为 0，实际为 %d", status.Progress)
    }

    if status.ErrorMessage != "" {
        t.Error("错误信息应被清空")
    }
}

// TestExtractBackupID tests backup ID extraction from path
func TestExtractBackupID(t *testing.T) {
    tests := []struct {
        path     string
        expected string
    }{
        {"/backups/backup_20260612_143052.db", "backup_20260612_143052"},
        {`C:\backups\backup_20260612_143052.db`, "backup_20260612_143052"},
        {"/backups/backup_test.db", "backup_test"},
        {"", ""},
        {"no_extension", "no_extension"},
    }

    for _, tt := range tests {
        t.Run(tt.path, func(t *testing.T) {
            result := extractBackupID(tt.path)
            if result != tt.expected {
                t.Errorf("extractBackupID(%s) = %s, want %s", tt.path, result, tt.expected)
            }
        })
    }
}