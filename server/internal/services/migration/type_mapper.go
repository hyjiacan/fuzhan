package migration

import (
    "regexp"
    "strings"
)

// SQLite 到 MySQL 类型映射
var sqliteToMySQLMapping = map[string]string{
    "INTEGER":     "INT",
    "INT":         "INT",
    "INT2":        "SMALLINT",
    "INT8":        "BIGINT",
    "TINYINT":     "TINYINT",
    "SMALLINT":    "SMALLINT",
    "MEDIUMINT":   "MEDIUMINT",
    "BIGINT":      "BIGINT",
    "REAL":        "DOUBLE",
    "FLOAT":       "DOUBLE",
    "DOUBLE":      "DOUBLE",
    "DECIMAL":     "DECIMAL",
    "NUMERIC":     "DECIMAL",
    "BLOB":        "LONGBLOB",
    "TINYBLOB":    "TINYBLOB",
    "MEDIUMBLOB":  "MEDIUMBLOB",
    "TEXT":        "LONGTEXT",
    "CHAR":        "CHAR",
    "VARCHAR":     "VARCHAR",
    "TINYTEXT":    "TINYTEXT",
    "MEDIUMTEXT":  "MEDIUMTEXT",
    "TIMESTAMP":   "DATETIME",
    "DATE":        "DATE",
    "TIME":        "TIME",
    "DATETIME":    "DATETIME",
    "BOOLEAN":     "TINYINT(1)",
    "BOOL":        "TINYINT(1)",
    "JSON":        "JSON",
    "UUID":        "VARCHAR(36)",
}

// SQLite 到 PostgreSQL 类型映射
var sqliteToPostgresMapping = map[string]string{
    "INTEGER":     "INTEGER",
    "INT":         "INTEGER",
    "INT2":        "SMALLINT",
    "INT8":        "BIGINT",
    "TINYINT":     "SMALLINT",
    "SMALLINT":    "SMALLINT",
    "MEDIUMINT":   "INTEGER",
    "BIGINT":      "BIGINT",
    "REAL":        "DOUBLE PRECISION",
    "FLOAT":       "DOUBLE PRECISION",
    "DOUBLE":      "DOUBLE PRECISION",
    "DECIMAL":     "DECIMAL",
    "NUMERIC":     "NUMERIC",
    "BLOB":        "BYTEA",
    "TINYBLOB":    "BYTEA",
    "MEDIUMBLOB":  "BYTEA",
    "TEXT":        "TEXT",
    "CHAR":        "CHAR",
    "VARCHAR":     "VARCHAR",
    "TINYTEXT":    "TEXT",
    "MEDIUMTEXT":  "TEXT",
    "LONGTEXT":    "TEXT",
    "TIMESTAMP":   "TIMESTAMP",
    "DATE":        "DATE",
    "TIME":        "TIME",
    "DATETIME":    "TIMESTAMP",
    "BOOLEAN":     "BOOLEAN",
    "BOOL":        "BOOLEAN",
    "JSON":        "JSONB",
    "UUID":        "UUID",
}

// MySQL 到 PostgreSQL 类型映射
var mysqlToPostgresMapping = map[string]string{
    "TINYINT":     "SMALLINT",
    "SMALLINT":    "SMALLINT",
    "MEDIUMINT":   "INTEGER",
    "INT":         "INTEGER",
    "INTEGER":     "INTEGER",
    "BIGINT":      "BIGINT",
    "REAL":        "DOUBLE PRECISION",
    "FLOAT":       "DOUBLE PRECISION",
    "DOUBLE":      "DOUBLE PRECISION",
    "DECIMAL":     "DECIMAL",
    "NUMERIC":     "NUMERIC",
    "LONGBLOB":    "BYTEA",
    "MEDIUMBLOB":  "BYTEA",
    "BLOB":        "BYTEA",
    "TINYBLOB":    "BYTEA",
    "LONGTEXT":    "TEXT",
    "MEDIUMTEXT":  "TEXT",
    "TEXT":        "TEXT",
    "TINYTEXT":    "TEXT",
    "CHAR":        "CHAR",
    "VARCHAR":     "VARCHAR",
    "TINYINT(1)":  "BOOLEAN",
    "TIMESTAMP":   "TIMESTAMP",
    "DATETIME":    "TIMESTAMP",
    "DATE":        "DATE",
    "TIME":        "TIME",
    "JSON":        "JSONB",
}

// PostgreSQL 到 MySQL 类型映射
var postgresToMySQLMapping = map[string]string{
    "SMALLINT":    "SMALLINT",
    "INTEGER":     "INT",
    "BIGINT":      "BIGINT",
    "SERIAL":      "INT AUTO_INCREMENT",
    "BIGSERIAL":   "BIGINT AUTO_INCREMENT",
    "DOUBLE PRECISION": "DOUBLE",
    "DECIMAL":     "DECIMAL",
    "NUMERIC":     "DECIMAL",
    "BYTEA":       "LONGBLOB",
    "TEXT":        "LONGTEXT",
    "CHAR":        "CHAR",
    "VARCHAR":     "VARCHAR",
    "BOOLEAN":     "TINYINT(1)",
    "TIMESTAMP":   "DATETIME",
    "TIMESTAMPTZ": "DATETIME",
    "DATE":        "DATE",
    "TIME":        "TIME",
    "TIMETZ":      "TIME",
    "JSONB":       "JSON",
    "JSON":        "JSON",
    "UUID":        "VARCHAR(36)",
    "XML":         "TEXT",
}

// TypeMapper 类型映射器
type TypeMapper struct {
    sourceDriver string
    targetDriver string
}

// NewTypeMapper 创建类型映射器
func NewTypeMapper(sourceDriver, targetDriver string) *TypeMapper {
    return &TypeMapper{
        sourceDriver: sourceDriver,
        targetDriver: targetDriver,
    }
}

// MapType 映射类型
func (m *TypeMapper) MapType(sqliteType string) string {
    sourceUpper := strings.ToUpper(strings.TrimSpace(sqliteType))

    // 提取类型名和精度
    typeName := sourceUpper
    precision := ""
    if idx := strings.Index(sourceUpper, "("); idx > 0 {
        typeName = sourceUpper[:idx]
        precision = sourceUpper[idx:]
    }

    var mapped string
    switch {
    case m.sourceDriver == "sqlite" && m.targetDriver == "mysql":
        mapped = sqliteToMySQLMapping[typeName]
    case m.sourceDriver == "sqlite" && m.targetDriver == "postgres":
        mapped = sqliteToPostgresMapping[typeName]
    case m.sourceDriver == "mysql" && m.targetDriver == "postgres":
        mapped = mysqlToPostgresMapping[typeName]
    case m.sourceDriver == "postgres" && m.targetDriver == "mysql":
        mapped = postgresToMySQLMapping[typeName]
    default:
        mapped = typeName
    }

    if mapped == "" {
        return sqliteType // 默认保持原样
    }

    // 保留精度信息
    if precision != "" && !strings.Contains(mapped, "(") {
        // 某些类型不需要精度
        if typeName == "VARCHAR" || typeName == "CHAR" {
            return mapped + precision
        }
    }

    return mapped
}

// MapColumnDefinition 映射列定义
func (m *TypeMapper) MapColumnDefinition(colDef string) string {
    // 使用正则提取列类型
    typeRegex := regexp.MustCompile(`(?i)(\w+(?:\([^)]+\))?)`)
    matches := typeRegex.FindStringSubmatch(colDef)
    if len(matches) > 1 {
        mappedType := m.MapType(matches[1])
        colDef = typeRegex.ReplaceAllString(colDef, mappedType)
    }
    return colDef
}

// SQLiteToMySQL SQLite 到 MySQL 类型映射
func SQLiteToMySQL(sqliteType string) string {
    mapper := NewTypeMapper("sqlite", "mysql")
    return mapper.MapType(sqliteType)
}

// SQLiteToPostgres SQLite 到 PostgreSQL 类型映射
func SQLiteToPostgres(sqliteType string) string {
    mapper := NewTypeMapper("sqlite", "postgres")
    return mapper.MapType(sqliteType)
}

// MySQLToPostgres MySQL 到 PostgreSQL 类型映射
func MySQLToPostgres(mysqlType string) string {
    mapper := NewTypeMapper("mysql", "postgres")
    return mapper.MapType(mysqlType)
}

// PostgresToMySQL PostgreSQL 到 MySQL 类型映射
func PostgresToMySQL(pgType string) string {
    mapper := NewTypeMapper("postgres", "mysql")
    return mapper.MapType(pgType)
}