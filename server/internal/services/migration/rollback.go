package migration

import (
    "fmt"
    "os"
    "time"

    "gorm.io/gorm"
    "fuzhan/internal/appconfig"
    "fuzhan/internal/models"
)

// RollbackService 回滚服务
type RollbackService struct {
    db            *gorm.DB
    backupService *BackupService
}

// NewRollbackService 创建回滚服务
func NewRollbackService(db *gorm.DB) *RollbackService {
    return &RollbackService{
        db:            db,
        backupService: NewBackupService(appconfig.GetBackupsDir(), appconfig.GlobalConfig.Database.Driver),
    }
}

// RollbackResult 回滚结果
type RollbackResult struct {
    Success   bool      `json:"success"`
    Message   string    `json:"message"`
    BackupID  string    `json:"backupId,omitempty"`
    Duration  time.Duration `json:"duration"`
}

// Rollback 回滚到迁移前的状态
func (s *RollbackService) Rollback(migrationID string) (*RollbackResult, error) {
    start := time.Now()

    // 获取迁移记录
    var status models.MigrationStatus
    if err := s.db.Where("id = ?", migrationID).First(&status).Error; err != nil {
        return nil, fmt.Errorf("迁移记录不存在: %w", err)
    }

    // 检查备份文件
    if status.BackupPath == "" {
        return nil, fmt.Errorf("没有可用的备份文件")
    }

    // 检查备份文件是否存在
    if _, err := os.Stat(status.BackupPath); err != nil {
        return nil, fmt.Errorf("备份文件不存在或已删除: %s", status.BackupPath)
    }

    // 获取备份信息
    backupID := extractBackupID(status.BackupPath)
    _, err := s.backupService.GetBackup(backupID)
    if err != nil {
        return nil, fmt.Errorf("无法读取备份信息: %w", err)
    }

    // 关闭当前数据库连接
    if err := appconfig.CloseDB(); err != nil {
        // 继续尝试回滚
    }

    // 根据源数据库类型执行回滚
    var rollbackErr error
    switch status.SourceDriver {
    case "sqlite":
        rollbackErr = s.rollbackSQLite(status.BackupPath, appconfig.GlobalConfig.Database.DSN)
    case "mysql":
        rollbackErr = s.rollbackMySQL(status.BackupPath, appconfig.GlobalConfig.Database.DSN)
    case "postgres":
        rollbackErr = s.rollbackPostgres(status.BackupPath, appconfig.GlobalConfig.Database.DSN)
    default:
        rollbackErr = s.rollbackSQLite(status.BackupPath, appconfig.GlobalConfig.Database.DSN)
    }

    if rollbackErr != nil {
        // 重新初始化数据库连接
        appconfig.InitDB(&appconfig.GlobalConfig.Database)
        return nil, fmt.Errorf("回滚失败: %w", rollbackErr)
    }

    // 重新初始化数据库连接
    db, err := appconfig.InitDB(&appconfig.GlobalConfig.Database)
    if err != nil {
        return nil, fmt.Errorf("重新连接数据库失败: %w", err)
    }
    s.db = db

    // 更新迁移状态
    now := time.Now()
    s.db.Model(&models.MigrationStatus{}).Where("id = ?", migrationID).Updates(map[string]interface{}{
        "status":       models.MigrationStatusRolledBack,
        "stage":        "rolled_back",
        "completed_at": &now,
    })

    return &RollbackResult{
        Success:   true,
        Message:   "回滚成功，数据库已恢复到迁移前的状态",
        BackupID:  backupID,
        Duration:  time.Since(start),
    }, nil
}

// rollbackSQLite 回滚 SQLite 数据库
func (s *RollbackService) rollbackSQLite(backupPath, targetDSN string) error {
    // SQLite 回滚：复制备份文件到目标位置
    backup, err := os.Open(backupPath)
    if err != nil {
        return err
    }
    defer backup.Close()

    // 删除原数据库文件
    os.Remove(targetDSN)

    // 复制备份文件
    target, err := os.Create(targetDSN)
    if err != nil {
        return err
    }
    defer target.Close()

    buffer := make([]byte, 32*1024)
    for {
        n, err := backup.Read(buffer)
        if n > 0 {
            if _, writeErr := target.Write(buffer[:n]); writeErr != nil {
                return writeErr
            }
        }
        if err != nil {
            break
        }
    }

    return nil
}

// rollbackMySQL 回滚 MySQL 数据库
func (s *RollbackService) rollbackMySQL(backupPath, targetDSN string) error {
    // MySQL 回滚需要使用 mysql 命令行工具
    // 这里简化实现，实际应该执行 mysqldump 备份的 SQL 文件
    return fmt.Errorf("MySQL 回滚需要使用 mysql 命令行工具执行: mysql -u user -p dbname < %s", backupPath)
}

// rollbackPostgres 回滚 PostgreSQL 数据库
func (s *RollbackService) rollbackPostgres(backupPath, targetDSN string) error {
    // PostgreSQL 回滚需要使用 psql 命令行工具
    return fmt.Errorf("PostgreSQL 回滚需要使用 psql 命令行工具执行: psql -U user -d dbname -f %s", backupPath)
}

// GetRollbackInfo 获取回滚信息
func (s *RollbackService) GetRollbackInfo(migrationID string) (*RollbackInfo, error) {
    var status models.MigrationStatus
    if err := s.db.Where("id = ?", migrationID).First(&status).Error; err != nil {
        return nil, err
    }

    info := &RollbackInfo{
        MigrationID: status.ID,
        CanRollback: status.BackupPath != "",
        BackupPath:  status.BackupPath,
    }

    if info.CanRollback {
        info.BackupID = extractBackupID(status.BackupPath)
        info.BackupSize = getFileSize(status.BackupPath)
        info.BackupCreatedAt = getFileTime(status.BackupPath)
    }

    return info, nil
}

// RollbackInfo 回滚信息
type RollbackInfo struct {
    MigrationID    string `json:"migrationId"`
    CanRollback    bool   `json:"canRollback"`
    BackupPath     string `json:"backupPath,omitempty"`
    BackupID       string `json:"backupId,omitempty"`
    BackupSize     int64  `json:"backupSize,omitempty"`
    BackupCreatedAt time.Time `json:"backupCreatedAt,omitempty"`
}

// getFileSize 获取文件大小
func getFileSize(path string) int64 {
    info, err := os.Stat(path)
    if err != nil {
        return 0
    }
    return info.Size()
}

// getFileTime 获取文件创建时间
func getFileTime(path string) time.Time {
    info, err := os.Stat(path)
    if err != nil {
        return time.Time{}
    }
    return info.ModTime()
}