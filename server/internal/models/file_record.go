package models

import (
    "time"

    "gorm.io/gorm"
)

// FileStatus 文件状态
type FileStatus string

const (
    // FileStatusActive 文件正常
    FileStatusActive FileStatus = "active"
    // FileStatusDeleted 文件已删除（软删除）
    FileStatusDeleted FileStatus = "deleted"
)

// FileRecordBase 文件索引记录基础结构
// 三张索引表共用此结构体，通过不同的 TableName 区分
// 索引由 EnsureFileRecordIndexes 函数统一创建，命名格式为 idx__{table}__{col1}_{col2}_...
// FullPath 字段同步存储 rootName/filePath，避免运行时拼接
type FileRecordBase struct {
    ID           uint           `gorm:"primaryKey" json:"id"`
    FileName     string         `gorm:"size:512;not null" json:"fileName"`
    FilePath     string         `gorm:"size:1024;not null" json:"filePath"`
    RootName     string         `gorm:"size:128;not null" json:"rootName"`
    FullPath     string         `gorm:"size:1024;not null" json:"fullPath"` // /rootName/filePath 完整路径（带前导 /）
    FileSize     int64          `gorm:"not null" json:"fileSize"`
    IsDir        bool           `gorm:"not null;default:false" json:"isDir"`
    Xxh3Hash     string         `gorm:"size:64" json:"xxh3Hash"`
    HashStatus   string         `gorm:"size:20;default:pending" json:"hashStatus"` // pending/done/failed
    HashError    string         `gorm:"size:512" json:"hashError"`                 // 失败原因
    HashRetries  int            `gorm:"default:0" json:"hashRetries"`              // 已重试次数
    ModTime      time.Time      `gorm:"not null" json:"modTime"`
    Notes        string         `gorm:"size:2048" json:"notes"`
    Status       FileStatus     `gorm:"size:20;default:active" json:"status"`
    OwnerID      string         `gorm:"size:64" json:"ownerID"` // 所有者标识：public 为空，temp 为 IP，private 为用户 ID
    LastSyncedAt time.Time      `gorm:"not null" json:"lastSyncedAt"`
    CreatedAt    time.Time      `gorm:"not null" json:"createdAt"`
    UpdatedAt    time.Time      `gorm:"not null" json:"updatedAt"`
    DeletedAt    gorm.DeletedAt `gorm:"index" json:"deletedAt"`
}

// FileRecordPublic 公开文件索引表
type FileRecordPublic struct {
    FileRecordBase
}

func (FileRecordPublic) TableName() string {
    return "file_records_public"
}

// FileRecordTemp 临时文件索引表
type FileRecordTemp struct {
    FileRecordBase
}

func (FileRecordTemp) TableName() string {
    return "file_records_temp"
}

// FileRecordPrivate 私有文件索引表
type FileRecordPrivate struct {
    FileRecordBase
}

func (FileRecordPrivate) TableName() string {
    return "file_records_private"
}

// fileRecordTables 三张索引表名列表
var fileRecordTables = []string{
    "file_records_public",
    "file_records_temp",
    "file_records_private",
}

// EnsureFileRecordIndexes 为三张索引表创建索引
// 索引命名格式：idx__{table}__{col1}_{col2}_...
// 在 AutoMigrate 后调用，与 GORM 的自动索引创建解耦，避免 SQLite 索引名冲突
func EnsureFileRecordIndexes(db *gorm.DB) {
    tables := fileRecordTables
    for _, table := range tables {
        // 单列索引
        singleColIndexes := []struct {
            name  string
            column string
        }{
            {name: "full_path", column: "full_path"},
            {name: "xxh3_hash", column: "xxh3_hash"},
            {name: "hash_status", column: "hash_status"},
            {name: "status", column: "status"},
            {name: "owner_id", column: "owner_id"},
            {name: "last_synced_at", column: "last_synced_at"},
            {name: "deleted_at", column: "deleted_at"},
        }
        for _, idx := range singleColIndexes {
            indexName := "idx__" + table + "__" + idx.name
            sql := "CREATE INDEX IF NOT EXISTS " + indexName + " ON " + table + "(" + idx.column + ")"
            if err := db.Exec(sql).Error; err != nil {
                // 使用 GORM 日志记录，不中断启动
                db.Logger.Warn(nil, "创建索引失败: %v, SQL: %s", err, sql)
            }
        }

        // 复合索引
        compositeIndexes := []struct {
            name     string
            columns  string
        }{
            {name: "root_name_file_path", columns: "root_name, file_path"},
            {name: "root_name_status", columns: "root_name, status"},
            {name: "hash_status_status", columns: "hash_status, status"},
            {name: "owner_id_status_deleted_at", columns: "owner_id, status, deleted_at"},
        }
        for _, idx := range compositeIndexes {
            indexName := "idx__" + table + "__" + idx.name
            sql := "CREATE INDEX IF NOT EXISTS " + indexName + " ON " + table + "(" + idx.columns + ")"
            if err := db.Exec(sql).Error; err != nil {
                db.Logger.Warn(nil, "创建索引失败: %v, SQL: %s", err, sql)
            }
        }
    }
}
