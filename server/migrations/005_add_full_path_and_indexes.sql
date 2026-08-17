-- Migration: 005_add_full_path_and_composite_indexes
-- Description:
--   1. 为 file_records_public/temp/private 添加 full_path 字段（rootName/filePath）
--   2. 添加复合索引 (root_name, file_path) 优化路径精确查询
--   3. 回填 full_path 字段
-- Created: 2026-08-07

-- ============================================
-- Step 1: 添加 full_path 字段（三表）
-- ============================================
ALTER TABLE file_records_public ADD COLUMN full_path VARCHAR(1024) NOT NULL DEFAULT '';
ALTER TABLE file_records_temp ADD COLUMN full_path VARCHAR(1024) NOT NULL DEFAULT '';
ALTER TABLE file_records_private ADD COLUMN full_path VARCHAR(1024) NOT NULL DEFAULT '';

-- ============================================
-- Step 2: 回填 full_path（root_name + '/' + file_path）
-- 注意：file_path 在数据库中已包含前导 '/'
-- ============================================
UPDATE file_records_public SET full_path = root_name || file_path;
UPDATE file_records_temp SET full_path = root_name || file_path;
UPDATE file_records_private SET full_path = root_name || file_path;

-- ============================================
-- Step 3: 添加复合索引 (root_name, file_path)
-- 覆盖核心查询：WHERE root_name = ? AND file_path = ?
-- ============================================
CREATE INDEX IF NOT EXISTS idx_file_records_public_root_path
    ON file_records_public(root_name, file_path);
CREATE INDEX IF NOT EXISTS idx_file_records_temp_root_path
    ON file_records_temp(root_name, file_path);
CREATE INDEX IF NOT EXISTS idx_file_records_private_root_path
    ON file_records_private(root_name, file_path);

-- ============================================
-- Step 4: 添加 full_path 索引（用于按完整路径查询）
-- ============================================
CREATE INDEX IF NOT EXISTS idx_file_records_public_full_path
    ON file_records_public(full_path);
CREATE INDEX IF NOT EXISTS idx_file_records_temp_full_path
    ON file_records_temp(full_path);
CREATE INDEX IF NOT EXISTS idx_file_records_private_full_path
    ON file_records_private(full_path);

-- ============================================
-- Step 5: 添加按 root_name 列表查询的索引（用于根目录列表）
-- 覆盖：WHERE root_name = ? AND status = 'active'
-- ============================================
CREATE INDEX IF NOT EXISTS idx_file_records_public_root_status
    ON file_records_public(root_name, status);
CREATE INDEX IF NOT EXISTS idx_file_records_temp_root_status
    ON file_records_temp(root_name, status);
CREATE INDEX IF NOT EXISTS idx_file_records_private_root_status
    ON file_records_private(root_name, status);

-- ============================================
-- Step 6: operation_records 表添加 full_path 字段 + 索引
-- 覆盖：最近上传/下载/搜索、热门下载
-- ============================================
ALTER TABLE operation_records ADD COLUMN full_path VARCHAR(1024) NOT NULL DEFAULT '';

-- 回填 full_path
UPDATE operation_records SET full_path = root_name || file_path;

-- 复合索引 (root_name, file_path) - 用于按根目录过滤
CREATE INDEX IF NOT EXISTS idx_operation_records_root_path
    ON operation_records(root_name, file_path);

-- full_path 索引 - 用于按完整路径直接查询
CREATE INDEX IF NOT EXISTS idx_operation_records_full_path
    ON operation_records(full_path);

-- (action, created_at) 复合索引 - 优化最近记录统计
CREATE INDEX IF NOT EXISTS idx_operation_records_action_created
    ON operation_records(action, created_at);
