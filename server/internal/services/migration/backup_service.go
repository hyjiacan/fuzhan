package migration

import (
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "time"
)

// BackupService 备份服务
type BackupService struct {
    backupDir   string
    maxBackups  int
    driver      string
}

// BackupInfo 备份信息
type BackupInfo struct {
    ID          string    `json:"id"`
    FileName    string    `json:"fileName"`
    FilePath    string    `json:"filePath"`
    Description string    `json:"description"`
    Size        int64     `json:"size"`
    CreatedAt   time.Time `json:"createdAt"`
    Status      string    `json:"status"` // pending, completed, failed, restoring
    Driver      string    `json:"driver"` // sqlite, mysql, postgres
}

// NewBackupService 创建备份服务
func NewBackupService(backupDir string, driver string) *BackupService {
    if backupDir == "" {
        backupDir = "./backups"
    }
    os.MkdirAll(backupDir, 0755)

    return &BackupService{
        backupDir:  backupDir,
        maxBackups: 10,
        driver:     driver,
    }
}

// CreateBackup 创建备份
func (s *BackupService) CreateBackup(migrator DatabaseMigrator, description string) (*BackupInfo, error) {
    // 生成备份文件名
    timestamp := time.Now().Format("20060102_150405")
    var ext string
    switch migrator.DriverName() {
    case "sqlite":
        ext = "db"
    case "mysql", "postgres":
        ext = "sql"
    default:
        ext = "sql"
    }

    backupID := fmt.Sprintf("backup_%s", timestamp)
    fileName := fmt.Sprintf("%s.%s", backupID, ext)
    filePath := filepath.Join(s.backupDir, fileName)

    // 创建备份
    if err := migrator.ExportData(nil); err != nil {
        // ExportData 需要 writer，我们使用文件
        if err := s.exportToFile(migrator, filePath); err != nil {
            return nil, fmt.Errorf("备份失败: %w", err)
        }
    }

    // 获取文件大小
    info, err := os.Stat(filePath)
    if err != nil {
        return nil, fmt.Errorf("获取备份文件信息失败: %w", err)
    }

    backup := &BackupInfo{
        ID:          backupID,
        FileName:    fileName,
        FilePath:    filePath,
        Description: description,
        Size:        info.Size(),
        CreatedAt:   time.Now(),
        Status:      "completed",
        Driver:      migrator.DriverName(),
    }

    // 保存元数据
    if err := s.saveMetadata(backup); err != nil {
        // 不影响主流程
    }

    // 清理旧备份
    s.Cleanup()

    return backup, nil
}

// exportToFile 导出到文件
func (s *BackupService) exportToFile(migrator DatabaseMigrator, filePath string) error {
    file, err := os.Create(filePath)
    if err != nil {
        return err
    }
    defer file.Close()

    return migrator.ExportData(file)
}

// ListBackups 列出所有备份
func (s *BackupService) ListBackups() ([]*BackupInfo, error) {
    var backups []*BackupInfo

    entries, err := os.ReadDir(s.backupDir)
    if err != nil {
        if os.IsNotExist(err) {
            return backups, nil
        }
        return nil, err
    }

    for _, entry := range entries {
        if entry.IsDir() {
            continue
        }

        name := entry.Name()
        if !strings.HasPrefix(name, "backup_") || !strings.HasSuffix(name, ".meta") {
            continue
        }

        backup, err := s.loadMetadata(filepath.Join(s.backupDir, name))
        if err != nil {
            continue
        }

        backups = append(backups, backup)
    }

    // 按创建时间倒序
    sort.Slice(backups, func(i, j int) bool {
        return backups[i].CreatedAt.After(backups[j].CreatedAt)
    })

    return backups, nil
}

// GetBackup 获取指定备份
func (s *BackupService) GetBackup(backupID string) (*BackupInfo, error) {
    metaPath := filepath.Join(s.backupDir, backupID+".meta")
    return s.loadMetadata(metaPath)
}

// RestoreBackup 恢复备份
func (s *BackupService) RestoreBackup(backupID string, migrator DatabaseMigrator) error {
    backup, err := s.GetBackup(backupID)
    if err != nil {
        return fmt.Errorf("备份不存在: %s", backupID)
    }

    if backup.Status != "completed" {
        return fmt.Errorf("备份状态无效: %s", backup.Status)
    }

    // 检查文件是否存在
    if _, err := os.Stat(backup.FilePath); err != nil {
        return fmt.Errorf("备份文件不存在: %s", backup.FilePath)
    }

    // 打开备份文件
    file, err := os.Open(backup.FilePath)
    if err != nil {
        return err
    }
    defer file.Close()

    // 导入数据
    return migrator.ImportData(file)
}

// DeleteBackup 删除备份
func (s *BackupService) DeleteBackup(backupID string) error {
    backup, err := s.GetBackup(backupID)
    if err != nil {
        return fmt.Errorf("备份不存在: %s", backupID)
    }

    // 删除备份文件
    if _, err := os.Stat(backup.FilePath); err == nil {
        if err := os.Remove(backup.FilePath); err != nil {
            return fmt.Errorf("删除备份文件失败: %w", err)
        }
    }

    // 删除元数据文件
    metaPath := filepath.Join(s.backupDir, backupID+".meta")
    if _, err := os.Stat(metaPath); err == nil {
        if err := os.Remove(metaPath); err != nil {
            return fmt.Errorf("删除元数据文件失败: %w", err)
        }
    }

    return nil
}

// Cleanup 清理旧备份
func (s *BackupService) Cleanup() error {
    backups, err := s.ListBackups()
    if err != nil {
        return err
    }

    if len(backups) <= s.maxBackups {
        return nil
    }

    // 删除超量的旧备份
    toDelete := backups[s.maxBackups:]
    for _, backup := range toDelete {
        if err := s.DeleteBackup(backup.ID); err != nil {
            // 继续删除其他备份
            continue
        }
    }

    return nil
}

// saveMetadata 保存元数据
func (s *BackupService) saveMetadata(backup *BackupInfo) error {
    metaPath := filepath.Join(s.backupDir, backup.ID+".meta")
    content := fmt.Sprintf(`{"id":"%s","fileName":"%s","filePath":"%s","description":"%s","size":%d,"createdAt":"%s","status":"%s","driver":"%s"}`,
        backup.ID, backup.FileName, backup.FilePath, backup.Description, backup.Size, backup.CreatedAt.Format(time.RFC3339), backup.Status, backup.Driver)
    return os.WriteFile(metaPath, []byte(content), 0644)
}

// loadMetadata 加载元数据
func (s *BackupService) loadMetadata(metaPath string) (*BackupInfo, error) {
    content, err := os.ReadFile(metaPath)
    if err != nil {
        return nil, err
    }

    // 简单解析 JSON
    backup := &BackupInfo{}
    _, err = fmt.Sscanf(string(content),
        `{"id":"%s","fileName":"%s","filePath":"%s","description":"%s","size":%d,"createdAt":"%s","status":"%s","driver":"%s"}`,
        &backup.ID, &backup.FileName, &backup.FilePath, &backup.Description, &backup.Size, new(string), &backup.Status, &backup.Driver)
    if err != nil {
        // 尝试解析 createdAt
        backup.CreatedAt = time.Now()
    }

    // 再次尝试解析时间
    if backup.CreatedAt.IsZero() {
        // 从文件名提取时间
        parts := strings.Split(backup.ID, "_")
        if len(parts) >= 2 {
            timeStr := parts[1]
            if len(timeStr) == 14 {
                backup.CreatedAt, _ = time.ParseInLocation("20060102150405", timeStr, time.Local)
            }
        }
    }

    return backup, nil
}

// CheckDiskSpace 检查磁盘空间
// 简化实现：在 Windows 上直接跳过检查
func (s *BackupService) CheckDiskSpace(requiredBytes int64) error {
    // 在 Windows 上使用 os.Stat 检查备份目录是否可写
    testFile := filepath.Join(s.backupDir, ".space_check")
    if err := os.WriteFile(testFile, []byte("check"), 0644); err != nil {
        return &MigrationError{
            Code:       ErrorCodeDiskFull,
            Message:    "磁盘空间不足或备份目录不可写",
            Details:    map[string]string{"backupDir": s.backupDir},
            CanRetry:   false,
            Suggestion: "请清理磁盘空间或检查备份目录权限",
        }
    }
    os.Remove(testFile)
    return nil
}

// GetBackupDir 获取备份目录
func (s *BackupService) GetBackupDir() string {
    return s.backupDir
}

// SetMaxBackups 设置最大备份数量
func (s *BackupService) SetMaxBackups(max int) {
    s.maxBackups = max
}