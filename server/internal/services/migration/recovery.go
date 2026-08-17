package migration

import (
    "context"
    "fmt"
    "time"

    "gorm.io/gorm"
    "fuzhan/internal/models"
)

// RecoveryService 恢复服务
type RecoveryService struct {
    db               *gorm.DB
    migrationService *MigrationService
}

// NewRecoveryService 创建恢复服务
func NewRecoveryService(db *gorm.DB, migrationService *MigrationService) *RecoveryService {
    return &RecoveryService{
        db:               db,
        migrationService: migrationService,
    }
}

// InterruptedMigration 中断的迁移信息
type InterruptedMigration struct {
    MigrationID    string     `json:"migrationId"`
    Stage          string     `json:"stage"`
    Progress       int        `json:"progress"`
    TablesCompleted int       `json:"tablesCompleted"`
    TablesTotal    int        `json:"tablesTotal"`
    StartedAt      time.Time  `json:"startedAt"`
    CanRollback    bool       `json:"canRollback"`
    BackupPath     string     `json:"backupPath,omitempty"`
    LastBackupID   string     `json:"lastBackupId,omitempty"`
}

// DetectInterruptedMigration 检测中断的迁移
func (s *RecoveryService) DetectInterruptedMigration() (*InterruptedMigration, error) {
    var status models.MigrationStatus

    // 查找最新的 running 状态的迁移
    err := s.db.Where("status = ?", models.MigrationStatusRunning).
        Order("started_at DESC").
        First(&status).Error

    if err != nil {
        if err == gorm.ErrRecordNotFound {
            return nil, nil // 没有中断的迁移
        }
        return nil, err
    }

    // 获取表进度
    var tableProgress []models.MigrationTableProgress
    s.db.Where("migration_id = ?", status.ID).Find(&tableProgress)

    tablesCompleted := 0
    for _, tp := range tableProgress {
        if tp.Status == models.TableProgressStatusCompleted {
            tablesCompleted++
        }
    }

    return &InterruptedMigration{
        MigrationID:    status.ID,
        Stage:          status.Stage,
        Progress:       status.Progress,
        TablesCompleted: tablesCompleted,
        TablesTotal:    status.TablesTotal,
        StartedAt:      status.StartedAt,
        CanRollback:    status.BackupPath != "",
        BackupPath:     status.BackupPath,
        LastBackupID:   extractBackupID(status.BackupPath),
    }, nil
}

// ResumeMigration 继续迁移
func (s *RecoveryService) ResumeMigration(ctx context.Context, migrationID string, targetDriver, targetDSN string) (<-chan *MigrationEvent, error) {
    // 获取迁移状态
    var status models.MigrationStatus
    if err := s.db.Where("id = ?", migrationID).First(&status).Error; err != nil {
        return nil, fmt.Errorf("迁移记录不存在: %w", err)
    }

    // 获取已完成的表
    var completedTables []models.MigrationTableProgress
    s.db.Where("migration_id = ? AND status = ?", migrationID, models.TableProgressStatusCompleted).
        Find(&completedTables)

    skipTables := make(map[string]bool)
    for _, tp := range completedTables {
        skipTables[tp.TableName] = true
    }

    // 创建带跳过表的选项
    opts := &MigrationOptions{
        SkipTables:   skipTables,
        CreateBackup: false, // 已有的不要重复创建
    }

    // 继续迁移
    _, err := s.migrationService.StartMigration(ctx, status.SourceDSN, targetDriver, targetDSN, opts)
    return nil, err
}

// RestartMigration 重新迁移
func (s *RecoveryService) RestartMigration(ctx context.Context, migrationID string, targetDriver, targetDSN string) (<-chan *MigrationEvent, error) {
    // 更新迁移状态
    s.db.Model(&models.MigrationStatus{}).Where("id = ?", migrationID).Updates(map[string]interface{}{
        "status":           models.MigrationStatusRunning,
        "stage":            models.MigrationStagePreparing,
        "progress":         0,
        "tables_completed": 0,
        "records_migrated": 0,
        "error_message":    "",
    })

    // 删除之前的表进度记录
    s.db.Where("migration_id = ?", migrationID).Delete(&models.MigrationTableProgress{})

    // 开始新迁移
    _, err := s.migrationService.StartMigration(ctx, "", targetDriver, targetDSN, &MigrationOptions{})
    return nil, err
}

// GetCompletedTables 获取已完成的表列表
func (s *RecoveryService) GetCompletedTables(migrationID string) ([]string, error) {
    var tableProgress []models.MigrationTableProgress
    err := s.db.Where("migration_id = ? AND status = ?", migrationID, models.TableProgressStatusCompleted).
        Find(&tableProgress).Error

    if err != nil {
        return nil, err
    }

    tables := make([]string, len(tableProgress))
    for i, tp := range tableProgress {
        tables[i] = tp.TableName
    }

    return tables, nil
}

// ResetMigrationStatus 重置迁移状态
func (s *RecoveryService) ResetMigrationStatus(migrationID string) error {
    return s.db.Model(&models.MigrationStatus{}).Where("id = ?", migrationID).Updates(map[string]interface{}{
        "status":           models.MigrationStatusPending,
        "stage":            models.MigrationStagePreparing,
        "progress":         0,
        "tables_completed": 0,
        "tables_total":     0,
        "records_migrated": 0,
        "error_message":    "",
    }).Error
}


// extractBackupID 从备份路径提取备份ID
func extractBackupID(backupPath string) string {
    if backupPath == "" {
        return ""
    }

    // 格式: backup_20260612_143052.db
    // 提取 backup_20260612_143052 部分
    for i := len(backupPath) - 1; i >= 0; i-- {
        if backupPath[i] == '/' || backupPath[i] == '\\' {
            name := backupPath[i+1:]
            if len(name) > 3 {
                return name[:len(name)-3] // 去掉扩展名
            }
            return name
        }
    }
    return backupPath
}