package models

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	ID            uint           `gorm:"primaryKey" json:"id"`
	FileName      string         `gorm:"size:512;not null" json:"fileName"`
	FilePath      string         `gorm:"size:1024;not null" json:"filePath"`
	RootName      string         `gorm:"size:128;not null" json:"rootName"`
	FullPath      string         `gorm:"size:1024;not null" json:"fullPath"` // /rootName/filePath 完整路径（带前导 /）
	FileSize      int64          `gorm:"not null" json:"fileSize"`
	IsDir         bool           `gorm:"not null;default:false" json:"isDir"`
	Xxh3Hash      string         `gorm:"size:64" json:"xxh3Hash"`
	HashStatus    string         `gorm:"size:20;default:pending" json:"hashStatus"` // pending/done/failed
	HashError     string         `gorm:"size:512" json:"hashError"`                 // 失败原因
	HashRetries   int            `gorm:"default:0" json:"hashRetries"`              // 已重试次数
	ModTime       time.Time      `gorm:"not null" json:"modTime"`
	Notes         string         `gorm:"size:4096" json:"notes"`
	DownloadCount int64          `gorm:"not null;default:0" json:"downloadCount"` // 公共文件下载次数
	UploaderIP    string         `gorm:"size:45" json:"uploaderIp,omitempty"`     // 上传者IP（公开文件），用于"IP一致允许覆盖/重命名/删除"
	Status        FileStatus     `gorm:"size:20;default:active" json:"status"`
	OwnerID       string         `gorm:"size:64" json:"ownerID"` // 所有者标识：public 为空，temp 为 IP，private 为用户 ID
	LastSyncedAt  time.Time      `gorm:"not null" json:"lastSyncedAt"`
	CreatedAt     time.Time      `gorm:"not null" json:"createdAt"`
	UpdatedAt     time.Time      `gorm:"not null" json:"updatedAt"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"deletedAt"`
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
// ShareCode 承载分享码（128bit hex 32字符），仅作分享/下载凭证、与存储布局解耦；
// FilePath 为私有存储内的真实相对路径（含子目录+真实文件名）。
type FileRecordPrivate struct {
	FileRecordBase
	ShareCode string `gorm:"size:64" json:"shareCode,omitempty"`
}

func (FileRecordPrivate) TableName() string {
	return "file_records_private"
}

// createIndexIfMissing 幂等创建索引，兼容 SQLite / MySQL / PostgreSQL。
//
// 幂等判定交给 GORM Migrator.HasIndex（各驱动按自身方言实现，天然跨库）；
// 索引名/表名/列名以 clause.Column / clause.Table 交给方言引用，既避免反引号在
// PostgreSQL 报错，也避免 read 等保留字在 MySQL 未加引号报错。
//
// 不使用 Migrator.CreateIndex：索引名按表动态生成（三张索引表共用 FileRecordBase，
// 无法用结构体 tag 表达），而 CreateIndex 要求索引已在模型 schema 中声明。
func createIndexIfMissing(db *gorm.DB, model interface{}, name string, unique bool, columns ...string) {
	if db == nil || db.Migrator().HasIndex(model, name) {
		return
	}

	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil || stmt.Table == "" {
		db.Logger.Warn(nil, "创建索引失败: 无法解析模型表名, index: %s, err: %v", name, err)
		return
	}

	placeholders := make([]string, len(columns))
	args := make([]interface{}, 0, len(columns)+2)
	args = append(args, clause.Column{Name: name}, clause.Table{Name: stmt.Table})
	for i, col := range columns {
		placeholders[i] = "?"
		args = append(args, clause.Column{Name: col})
	}

	prefix := "CREATE INDEX "
	if unique {
		prefix = "CREATE UNIQUE INDEX "
	}
	sql := prefix + "? ON ? (" + strings.Join(placeholders, ", ") + ")"
	if err := db.Exec(sql, args...).Error; err != nil {
		// 索引创建失败不阻断启动（仅提示）
		db.Logger.Warn(nil, "创建索引失败: %v, SQL: %s", err, sql)
	}
}

// fileRecordIndexTables 三张索引表（模型 + 表名）。
// 三表共用 FileRecordBase，索引名需按表区分，故不能用结构体 tag 声明。
var fileRecordIndexTables = []struct {
	model interface{}
	name  string
}{
	{model: &FileRecordPublic{}, name: FileRecordPublic{}.TableName()},
	{model: &FileRecordTemp{}, name: FileRecordTemp{}.TableName()},
	{model: &FileRecordPrivate{}, name: FileRecordPrivate{}.TableName()},
}

// EnsureFileRecordIndexes 为三张索引表创建索引
// 索引命名格式：idx__{table}__{col1}_{col2}_...
// 在 AutoMigrate 后调用，与 GORM 的自动索引创建解耦，避免 SQLite 索引名冲突
func EnsureFileRecordIndexes(db *gorm.DB) {
	// 单列索引
	singleColIndexes := []string{
		"full_path",
		"xxh3_hash",
		"hash_status",
		"status",
		"owner_id",
		"last_synced_at",
		"deleted_at",
	}

	// 复合索引
	compositeIndexes := []struct {
		name    string
		columns []string
	}{
		{name: "root_name_file_path", columns: []string{"root_name", "file_path"}},
		{name: "root_name_status", columns: []string{"root_name", "status"}},
		{name: "hash_status_status", columns: []string{"hash_status", "status"}},
		{name: "owner_id_status_deleted_at", columns: []string{"owner_id", "status", "deleted_at"}},
		// 支持按根目录+完整路径快速关联（ResolvePublicFileID 及历史记录路径回查）
		{name: "root_name_full_path", columns: []string{"root_name", "full_path"}},
		// 支持重复文件分组（xxh3_hash 全量排序前先按 status 过滤）
		{name: "status_xxh3_hash", columns: []string{"status", "xxh3_hash"}},
	}

	for _, t := range fileRecordIndexTables {
		for _, col := range singleColIndexes {
			createIndexIfMissing(db, t.model, "idx__"+t.name+"__"+col, false, col)
		}
		for _, idx := range compositeIndexes {
			createIndexIfMissing(db, t.model, "idx__"+t.name+"__"+idx.name, false, idx.columns...)
		}
	}

	// 私有表额外建 share_code 唯一索引（分享下载按码直接反查）。
	// 空的 share_code（存量未迁移记录）在 SQLite 唯一索引下按 NULL 处理，多行互不冲突。
	createIndexIfMissing(db, &FileRecordPrivate{}, "idx__"+FileRecordPrivate{}.TableName()+"__share_code", true, "share_code")
}

// EnsureOperationRecordIndexes 为操作记录表创建组合索引（单列索引由 GORM 自动迁移创建）。
// 最近/热门列表与统计都按 action + created_at 过滤排序，组合索引优于单列索引。
func EnsureOperationRecordIndexes(db *gorm.DB) {
	compositeIndexes := []struct {
		name    string
		columns []string
	}{
		{name: "action_created_at", columns: []string{"action", "created_at"}},
		// 行为分析：按 dl_event 等值 + created_at 区间过滤，dl_ok 作覆盖列做成功/失败分支，
		// 使 summary/trend/heatmap/top-files/sources/aggregate 等均命中该覆盖索引。
		{name: "dl_event_created_at_dl_ok", columns: []string{"dl_event", "created_at", "dl_ok"}},
		// 分析缓存水位：判定「自某水位以来是否新增公开下载事件/inline 取最大 id」，用 (dl_event, id) 快速命中。
		{name: "dl_event_id", columns: []string{"dl_event", "id"}},
		// 下载次数重算：按 dl_counted 等值 + full_path 计数（删除记录后回退下载次数），
		// 同时覆盖「按 dl_counted 分组统计各路径计数」的全量对齐扫描，避免扫全表。
		{name: "dl_counted_full_path", columns: []string{"dl_counted", "full_path"}},
	}
	for _, idx := range compositeIndexes {
		createIndexIfMissing(db, &OperationRecord{}, "idx__operation_records__"+idx.name, false, idx.columns...)
	}
}

// CleanupOperationRecordIndexes 幂等清理 operation_records 上的历史冗余索引。
// 目标：删除 migrations/005 遗留的 idx_operation_records_action_created，其列与 Ensure 新建的
// idx__operation_records__action_created_at 完全一致（action, created_at），导致每次 INSERT 维护两棵
// 相同 B-tree（写放大）。删除对象被保留索引完全覆盖，零查询退化；仅存在于手动执行过 005.sql 的存量库。
// 用 GORM Migrator 做 HasIndex 探测 + DropIndex，规避 MySQL 不支持 DROP INDEX IF EXISTS 的方言差异，
// 跨库幂等；必须先于本函数调用 EnsureOperationRecordIndexes（确保保留索引已建好）。
func CleanupOperationRecordIndexes(db *gorm.DB) {
	dup := "idx_operation_records_action_created"
	if !db.Migrator().HasIndex(&OperationRecord{}, dup) {
		return
	}
	if err := db.Migrator().DropIndex(&OperationRecord{}, dup); err != nil {
		db.Logger.Warn(nil, "清理 operation_records 冗余索引失败: %v, index: %s", err, dup)
		return
	}
	db.Logger.Info(nil, "已清理 operation_records 冗余索引 %s", dup)
}
