-- Migration: 004_add_composite_indexes
-- Description: 添加关键复合索引，优化查询性能
-- Created: 2026-07-22
-- Note: GORM AutoMigrate 不会去重，已有的单列索引保留
-- 注意: 字段名使用下划线格式（GORM 自动转换驼峰命名）

-- ============================================
-- P0: users 表 - 登录优化
-- ============================================
-- 登录查询: WHERE username = ? AND disabled = false
CREATE INDEX IF NOT EXISTS idx_users_username_status
    ON users(username, disabled);

-- ============================================
-- P0: file_records_* 三表 - 文件列表过滤
-- ============================================
-- 文件列表: WHERE hash_status = ? AND status = 'active'
CREATE INDEX IF NOT EXISTS idx_file_records_public_hash_status
    ON file_records_public(hash_status, status);
CREATE INDEX IF NOT EXISTS idx_file_records_temp_hash_status
    ON file_records_temp(hash_status, status);
CREATE INDEX IF NOT EXISTS idx_file_records_private_hash_status
    ON file_records_private(hash_status, status);

-- 文件查询: WHERE owner_id = ? AND status = 'active' AND deleted_at IS NULL
CREATE INDEX IF NOT EXISTS idx_file_records_public_owner_status
    ON file_records_public(owner_id, status, deleted_at);
CREATE INDEX IF NOT EXISTS idx_file_records_temp_owner_status
    ON file_records_temp(owner_id, status, deleted_at);
CREATE INDEX IF NOT EXISTS idx_file_records_private_owner_status
    ON file_records_private(owner_id, status, deleted_at);

-- ============================================
-- P1: temp_files 表 - 临时文件查询
-- ============================================
-- 临时文件查询: WHERE client_ip = ? AND dir = ?
CREATE INDEX IF NOT EXISTS idx_temp_files_client_dir
    ON temp_files(client_ip, dir);

-- ============================================
-- P1: operation_records 表 - 审计查询
-- ============================================
-- 用户操作记录: WHERE user_id = ? AND deleted_at IS NULL ORDER BY created_at DESC
CREATE INDEX IF NOT EXISTS idx_operation_records_user_created
    ON operation_records(user_id, deleted_at, created_at);

-- IP 操作记录: WHERE client_ip = ? AND created_at > ?
CREATE INDEX IF NOT EXISTS idx_operation_records_client_created
    ON operation_records(client_ip, created_at);

-- ============================================
-- P1: upload_sessions 表 - 清理过期会话
-- ============================================
-- 清理查询: WHERE deleted_at IS NULL AND expired_at < ?
CREATE INDEX IF NOT EXISTS idx_upload_sessions_deleted_expired
    ON upload_sessions(deleted_at, expired_at);

-- ============================================
-- P2: auth_records 表 - 认证记录查询
-- ============================================
-- 认证查询: WHERE user_id = ? AND auth_type = ?
CREATE INDEX IF NOT EXISTS idx_auth_records_user_type
    ON auth_records(user_id, auth_type);