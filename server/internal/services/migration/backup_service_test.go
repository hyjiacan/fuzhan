package migration

import (
    "os"
    "path/filepath"
    "testing"
    "time"
)

// TestBackupService_ListBackups_Empty tests listing empty backup directory
func TestBackupService_ListBackups_Empty(t *testing.T) {
    tmpDir, err := os.MkdirTemp("", "backup_test_*")
    if err != nil {
        t.Fatalf("创建临时目录失败: %v", err)
    }
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    service := NewBackupService(backupDir, "sqlite")

    backups, err := service.ListBackups()
    if err != nil {
        t.Fatalf("ListBackups 失败: %v", err)
    }

    if len(backups) != 0 {
        t.Errorf("预期 0 个备份，实际有 %d 个", len(backups))
    }
}

// TestBackupService_GetBackup_NotFound tests error case for non-existent backup
func TestBackupService_GetBackup_NotFound(t *testing.T) {
    tmpDir, err := os.MkdirTemp("", "backup_test_*")
    if err != nil {
        t.Fatalf("创建临时目录失败: %v", err)
    }
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    service := NewBackupService(backupDir, "sqlite")

    _, err = service.GetBackup("non-existent-backup")
    if err == nil {
        t.Error("预期返回错误，但得到了 nil")
    }
}

// TestBackupService_DeleteBackup_NotFound tests deletion of non-existent backup
func TestBackupService_DeleteBackup_NotFound(t *testing.T) {
    tmpDir, err := os.MkdirTemp("", "backup_test_*")
    if err != nil {
        t.Fatalf("创建临时目录失败: %v", err)
    }
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    service := NewBackupService(backupDir, "sqlite")

    err = service.DeleteBackup("non-existent")
    if err == nil {
        t.Error("预期返回错误，但得到了 nil")
    }
}

// TestBackupService_CheckDiskSpace tests disk space checking
func TestBackupService_CheckDiskSpace(t *testing.T) {
    tmpDir, err := os.MkdirTemp("", "backup_test_*")
    if err != nil {
        t.Fatalf("创建临时目录失败: %v", err)
    }
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    service := NewBackupService(backupDir, "sqlite")

    // Test with reasonable disk space requirement
    err = service.CheckDiskSpace(1024) // 1KB
    if err != nil {
        t.Errorf("CheckDiskSpace 应返回 nil，实际返回: %v", err)
    }
}

// TestBackupService_CheckDiskSpace_InvalidDir tests disk space check with invalid directory
func TestBackupService_CheckDiskSpace_InvalidDir(t *testing.T) {
    invalidDir := "X:/nonexistent/path/that/cannot/be/written"
    invalidService := NewBackupService(invalidDir, "sqlite")
    err := invalidService.CheckDiskSpace(1024)
    if err == nil {
        t.Error("对无效目录应返回错误")
    }
}

// TestBackupService_GetBackupDir tests getting backup directory
func TestBackupService_GetBackupDir(t *testing.T) {
    tmpDir := "/test/backup/dir"
    service := NewBackupService(tmpDir, "sqlite")

    if service.GetBackupDir() != tmpDir {
        t.Errorf("预期备份目录为 %s，实际为 %s", tmpDir, service.GetBackupDir())
    }
}

// TestBackupService_SetMaxBackups tests setting max backups
func TestBackupService_SetMaxBackups(t *testing.T) {
    tmpDir, _ := os.MkdirTemp("", "backup_test_*")
    defer os.RemoveAll(tmpDir)

    service := NewBackupService(tmpDir, "sqlite")

    testCases := []int{0, 1, 5, 100}
    for _, max := range testCases {
        service.SetMaxBackups(max)
    }
}

// TestBackupService_EmptyBackupDir tests listing empty backup directory
func TestBackupService_EmptyBackupDir(t *testing.T) {
    tmpDir, err := os.MkdirTemp("", "backup_test_*")
    if err != nil {
        t.Fatalf("创建临时目录失败: %v", err)
    }
    defer os.RemoveAll(tmpDir)

    backupDir := filepath.Join(tmpDir, "backups")
    os.MkdirAll(backupDir, 0755)

    service := NewBackupService(backupDir, "sqlite")

    backups, err := service.ListBackups()
    if err != nil {
        t.Fatalf("ListBackups 失败: %v", err)
    }

    if len(backups) != 0 {
        t.Errorf("预期 0 个备份，实际有 %d 个", len(backups))
    }
}

// TestBackupInfo tests BackupInfo struct
func TestBackupInfo(t *testing.T) {
    now := time.Now()
    info := BackupInfo{
        ID:          "backup_20260612",
        FileName:    "backup_20260612.db",
        FilePath:    "/backups/backup_20260612.db",
        Description: "Test backup",
        Size:        1024,
        CreatedAt:   now,
        Status:      "completed",
        Driver:      "sqlite",
    }

    if info.ID != "backup_20260612" {
        t.Errorf("ID 不正确: %s", info.ID)
    }

    if info.Status != "completed" {
        t.Errorf("Status 不正确: %s", info.Status)
    }

    if info.Size != 1024 {
        t.Errorf("Size 不正确: %d", info.Size)
    }
}