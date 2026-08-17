package migration

import (
    "regexp"
    "strings"
)

// ConflictStrategy 冲突处理策略
type ConflictStrategy int

const (
    ConflictSkip ConflictStrategy = iota // 跳过已存在的记录
    ConflictOverwrite                    // 覆盖已存在的记录
    ConflictError                        // 报错终止
)

// DDLTransformer DDL 转换器
type DDLTransformer struct {
    targetDriver string
}

// NewDDLTransformer 创建 DDL 转换器
func NewDDLTransformer(targetDriver string) *DDLTransformer {
    return &DDLTransformer{targetDriver: targetDriver}
}

// TransformDDL 转换 DDL 以适配目标数据库
func (t *DDLTransformer) TransformDDL(ddl string) string {
    switch t.targetDriver {
    case "mysql":
        return t.transformForMySQL(ddl)
    case "postgres":
        return t.transformForPostgres(ddl)
    case "sqlite":
        return ddl
    default:
        return ddl
    }
}

// transformForMySQL 转换为 MySQL DDL
func (t *DDLTransformer) transformForMySQL(ddl string) string {
    result := ddl

    // BOOLEAN -> TINYINT(1)
    result = t.replaceType(result, `\bBOOLEAN\b`, "TINYINT(1)")

    // JSON -> JSON
    result = t.replaceType(result, `\bJSON\b`, "JSON")

    // BLOB (不区分大小写) -> LONGBLOB
    result = t.replaceType(result, `\bBLOB\b`, "LONGBLOB")

    // BYTEA -> LONGBLOB
    result = t.replaceType(result, `\bBYTEA\b`, "LONGBLOB")

    // UUID -> VARCHAR(36)
    result = t.replaceType(result, `\bUUID\b`, "VARCHAR(36)")

    // TIMESTAMP -> DATETIME (MySQL 5.6+ 兼容)
    result = t.replaceType(result, `\bTIMESTAMP\b`, "DATETIME")

    // SERIAL -> AUTO_INCREMENT
    result = t.replaceType(result, `\bSERIAL\b`, "BIGINT UNSIGNED AUTO_INCREMENT")

    // 删除 PostgreSQL 特有的 IF NOT EXISTS (MySQL 会报错)
    result = strings.Replace(result, "IF NOT EXISTS", "", -1)

    return result
}

// transformForPostgres 转换为 PostgreSQL DDL
func (t *DDLTransformer) transformForPostgres(ddl string) string {
    result := ddl

    // TINYINT(1) -> BOOLEAN (如果是布尔字段)
    result = t.replaceType(result, `\bTINYINT\(1\)\b`, "BOOLEAN")

    // JSON -> JSONB (性能更好)
    result = t.replaceType(result, `\bJSON\b`, "JSONB")

    // LONGBLOB/MEDIUMBLOB/BLOB -> BYTEA
    result = t.replaceType(result, `\bLONGBLOB\b`, "BYTEA")
    result = t.replaceType(result, `\bMEDIUMBLOB\b`, "BYTEA")
    result = t.replaceType(result, `\bTINYBLOB\b`, "BYTEA")

    // VARCHAR -> VARCHAR (保持)
    // INT -> INTEGER
    result = t.replaceType(result, `\bINT\b`, "INTEGER")
    result = t.replaceType(result, `\bBIGINT\b`, "BIGINT")

    // AUTO_INCREMENT -> SERIAL
    result = t.replaceType(result, `\bAUTO_INCREMENT\b`, "GENERATED ALWAYS AS IDENTITY")
    result = t.replaceType(result, `\bAUTO_INCREMENT\s*\(\d+\)\b`, "GENERATED ALWAYS AS IDENTITY")

    // 删除 MySQL 特有的注释
    result = t.removeMySQLComments(result)

    return result
}

// replaceType 替换类型（使用正则）
func (t *DDLTransformer) replaceType(s, pattern, replacement string) string {
    re := regexp.MustCompile(`(?i)` + pattern)
    return re.ReplaceAllString(s, replacement)
}

// removeMySQLComments 移除 MySQL 注释
func (t *DDLTransformer) removeMySQLComments(ddl string) string {
    // 移除 -- 注释
    lines := strings.Split(ddl, "\n")
    var result []string
    for _, line := range lines {
        if idx := strings.Index(line, "--"); idx >= 0 {
            line = strings.TrimSpace(line[:idx])
        }
        if line != "" {
            result = append(result, line)
        }
    }
    return strings.Join(result, "\n")
}

// TransformColumnDefinition 转换列定义
func (t *DDLTransformer) TransformColumnDefinition(colDef string) string {
    colDef = strings.TrimSpace(colDef)

    // 处理 DEFAULT NULL
    colDef = strings.Replace(colDef, "DEFAULT NULL", "NULL", -1)

    // 处理 NOT NULL DEFAULT ''
    colDef = strings.Replace(colDef, "NOT NULL DEFAULT ''", "NOT NULL", -1)

    return colDef
}

// ExtractTableName 从 DDL 提取表名
func (t *DDLTransformer) ExtractTableName(ddl string) string {
    // 简单实现：查找 CREATE TABLE 后的表名
    upper := strings.ToUpper(ddl)
    upper = strings.Replace(upper, "`", "", -1)

    // 查找 CREATE TABLE
    if idx := strings.Index(upper, "CREATE TABLE"); idx >= 0 {
        rest := strings.TrimSpace(ddl[idx+12:])
        // 跳过空格和 IF NOT EXISTS
        rest = strings.TrimPrefix(rest, "IF NOT EXISTS")
        rest = strings.TrimSpace(rest)

        // 提取表名
        parts := strings.SplitN(rest, "(", 2)
        if len(parts) > 0 {
            name := strings.TrimSpace(parts[0])
            name = strings.Trim(name, `"' `)
            return name
        }
    }

    return ""
}

// ExtractColumnNames 提取列名列表
func (t *DDLTransformer) ExtractColumnNames(ddl string) []string {
    // 找到第一个括号内的内容
    start := strings.Index(ddl, "(")
    end := strings.LastIndex(ddl, ")")
    if start < 0 || end < 0 {
        return nil
    }

    columnsStr := ddl[start+1 : end]
    lines := strings.Split(columnsStr, ",")

    var columns []string
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }

        // 移除 PRIMARY KEY, FOREIGN KEY, INDEX 等
        upper := strings.ToUpper(line)
        if strings.HasPrefix(upper, "PRIMARY KEY") ||
            strings.HasPrefix(upper, "FOREIGN KEY") ||
            strings.HasPrefix(upper, "INDEX ") ||
            strings.HasPrefix(upper, "KEY ") ||
            strings.HasPrefix(upper, "UNIQUE ") ||
            strings.HasPrefix(upper, "CONSTRAINT") {
            continue
        }

        // 提取列名（第一个单词）
        parts := strings.Fields(line)
        if len(parts) > 0 {
            name := parts[0]
            name = strings.Trim(name, `"'`)
            columns = append(columns, name)
        }
    }

    return columns
}