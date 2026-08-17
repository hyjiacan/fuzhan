package models

import (
    "time"
)

// MigrationStatus 迁移状态
type MigrationStatus struct {
    ID              string     `gorm:"primaryKey"`           // UUID
    SourceDriver    string     `gorm:"size:20"`              // 源数据库类型: sqlite/mysql/postgres
    SourceDSN       string     `gorm:"size:500"`             // 源数据库连接
    TargetDriver    string     `gorm:"size:20"`              // 目标数据库类型
    TargetDSN       string     `gorm:"size:500"`             // 目标数据库连接
    Status          string     `gorm:"size:20"`              // pending/running/completed/failed/rolled_back
    Stage           string     `gorm:"size:30"`              // 当前阶段: preparing/backup/export/transform/import/verify/complete
    Progress        int        `gorm:"default:0"`            // 进度 0-100
    TablesCompleted int        `gorm:"default:0"`            // 已完成表数
    TablesTotal     int        `gorm:"default:0"`            // 总表数
    RecordsMigrated int64      `gorm:"default:0"`            // 已迁移记录数
    ErrorMessage    string     `gorm:"size:1000"`            // 错误信息
    BackupPath      string     `gorm:"size:500"`             // 备份文件路径
    StartedAt       time.Time  `gorm:"index"`                // 开始时间
    CompletedAt     *time.Time `gorm:"index"`                // 完成时间
    CreatedAt       time.Time                           // 创建时间
}

// MigrationTableProgress 表迁移进度
type MigrationTableProgress struct {
    ID            uint       `gorm:"primaryKey"`            // 主键
    MigrationID   string     `gorm:"index"`                 // 迁移ID
    TableName     string     `gorm:"size:100"`              // 表名
    Status        string     `gorm:"size:20"`               // pending/running/completed/failed
    SourceRecords int64      `gorm:"default:0"`             // 源表记录数
    TargetRecords int64      `gorm:"default:0"`             // 目标表记录数
    ErrorMessage  string     `gorm:"size:500"`              // 错误信息
    StartedAt     *time.Time                         // 开始时间
    CompletedAt   *time.Time                         // 完成时间
}

// MigrationStage 迁移阶段常量
const (
    MigrationStagePreparing = "preparing"
    MigrationStageBackup    = "backup"
    MigrationStageExport    = "export"
    MigrationStageTransform = "transform"
    MigrationStageImport    = "import"
    MigrationStageVerify    = "verify"
    MigrationStageComplete  = "complete"
    MigrationStageError     = "error"
)

// MigrationStatus 常量
const (
    MigrationStatusPending    = "pending"
    MigrationStatusRunning    = "running"
    MigrationStatusCompleted  = "completed"
    MigrationStatusFailed     = "failed"
    MigrationStatusRolledBack = "rolled_back"
)

// TableProgressStatus 常量
const (
    TableProgressStatusPending   = "pending"
    TableProgressStatusRunning   = "running"
    TableProgressStatusCompleted = "completed"
    TableProgressStatusFailed    = "failed"
)