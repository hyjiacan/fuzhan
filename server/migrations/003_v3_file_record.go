// Package migrations 提供数据库迁移定义
// 003: v3.0 文件索引表（Phase 0）
//
// 迁移说明:
//
// FileRecord 模型的迁移通过 server/main.go 中的 db.AutoMigrate() 自动完成,
// GORM 会根据模型结构体定义自动创建或更新表结构。
//
// 索引表体系（三表设计）:
//   - file_records: 原表，向后兼容
//   - file_records_public: 公开文件索引表（新表）
//   - file_records_temp: 临时文件索引表
//   - file_records_private: 私有文件索引表
//
// 三表共用 FileRecordBase 结构，通过 TableName() 区分表名。
// 扫描器同时写入 file_records 和 file_records_public。
//
// 索引策略:
//   - (file_path, root_name) 复合索引: idx_path_root — 覆盖按路径加根目录的查询
//   - xxh3_hash 单列索引 — 加速重复文件检测
//   - status 单列索引 — 加速活跃/已删除过滤
//   - last_synced_at 单列索引 — 加速增量同步判断
//   - owner_id 单列索引 — 加速按所有者维度查询
//
// 数据模型: server/models/file_record.go
// 索引服务: server/internal/index/
package migrations
