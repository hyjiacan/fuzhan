package migration

import (
    "fmt"
    "strings"
)

// IndexConverter 索引转换器
type IndexConverter struct {
    targetDriver string
}

// NewIndexConverter 创建索引转换器
func NewIndexConverter(targetDriver string) *IndexConverter {
    return &IndexConverter{targetDriver: targetDriver}
}

// ConvertIndex 转换索引定义到目标数据库语法
func (c *IndexConverter) ConvertIndex(idx IndexDefinition) string {
    switch c.targetDriver {
    case "mysql":
        return c.convertToMySQL(idx)
    case "postgres":
        return c.convertToPostgres(idx)
    default:
        return c.convertGeneric(idx)
    }
}

// convertToMySQL 转换为 MySQL 索引语法
func (c *IndexConverter) convertToMySQL(idx IndexDefinition) string {
    var sql strings.Builder

    if idx.Unique {
        sql.WriteString("UNIQUE ")
    }
    sql.WriteString("INDEX ")
    sql.WriteString(idx.Name)
    sql.WriteString(" (")

    for i, col := range idx.Columns {
        if i > 0 {
            sql.WriteString(", ")
        }
        sql.WriteString("`")
        sql.WriteString(col)
        sql.WriteString("`")
    }

    sql.WriteString(")")

    if idx.Type != "" && idx.Type != "BTREE" {
        sql.WriteString(" USING ")
        sql.WriteString(idx.Type)
    }

    return sql.String()
}

// convertToPostgres 转换为 PostgreSQL 索引语法
func (c *IndexConverter) convertToPostgres(idx IndexDefinition) string {
    var sql strings.Builder

    sql.WriteString("CREATE ")
    if idx.Unique {
        sql.WriteString("UNIQUE ")
    }
    sql.WriteString("INDEX ")

    if idx.Name != "" {
        sql.WriteString(idx.Name)
        sql.WriteString(" ")
    }

    sql.WriteString("ON ")
    sql.WriteString(idx.TableName)

    // PostgreSQL 需要 USING 子句
    if idx.Type == "" || idx.Type == "BTREE" {
        sql.WriteString(" USING btree")
    } else if idx.Type == "HASH" {
        sql.WriteString(" USING hash")
    } else {
        sql.WriteString(" USING ")
        sql.WriteString(strings.ToLower(idx.Type))
    }

    sql.WriteString(" (")

    for i, col := range idx.Columns {
        if i > 0 {
            sql.WriteString(", ")
        }
        sql.WriteString("\"")
        sql.WriteString(col)
        sql.WriteString("\"")
    }

    sql.WriteString(")")

    return sql.String()
}

// convertGeneric 通用转换
func (c *IndexConverter) convertGeneric(idx IndexDefinition) string {
    var sql strings.Builder

    sql.WriteString("CREATE ")
    if idx.Unique {
        sql.WriteString("UNIQUE ")
    }
    sql.WriteString("INDEX ")
    sql.WriteString(idx.Name)
    sql.WriteString(" ON ")
    sql.WriteString(idx.TableName)
    sql.WriteString(" (")

    for i, col := range idx.Columns {
        if i > 0 {
            sql.WriteString(", ")
        }
        sql.WriteString(col)
    }

    sql.WriteString(")")

    return sql.String()
}

// ConvertAllIndexes 转换所有索引
func (c *IndexConverter) ConvertAllIndexes(table *TableDefinition) []string {
    var sqls []string
    for _, idx := range table.Indexes {
        idx.TableName = table.Name
        sqls = append(sqls, c.ConvertIndex(idx))
    }
    return sqls
}

// BuildCreateIndexSQL 构建创建索引 SQL
func BuildCreateIndexSQL(idx IndexDefinition, targetDriver string) string {
    converter := NewIndexConverter(targetDriver)
    return converter.ConvertIndex(idx)
}

// BuildDropIndexSQL 构建删除索引 SQL
func BuildDropIndexSQL(idx IndexDefinition, targetDriver string) string {
    var sql strings.Builder

    switch targetDriver {
    case "mysql":
        sql.WriteString("DROP INDEX ")
        sql.WriteString(idx.Name)
        sql.WriteString(" ON ")
        sql.WriteString(idx.TableName)
    case "postgres":
        sql.WriteString("DROP INDEX ")
        if idx.Name != "" {
            sql.WriteString("IF EXISTS ")
            sql.WriteString(idx.Name)
        }
    case "sqlite":
        sql.WriteString("DROP INDEX ")
        sql.WriteString(idx.Name)
    }

    return sql.String()
}

// ValidateIndexName 验证索引名是否有效
func ValidateIndexName(name string) bool {
    if name == "" {
        return false
    }
    // 索引名不应包含特殊字符
    for _, ch := range name {
        if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '_' {
            return false
        }
    }
    return true
}

// GenerateIndexName 生成默认索引名
func GenerateIndexName(tableName, columnName string, unique bool) string {
    prefix := "idx"
    if unique {
        prefix = "uniq"
    }
    return fmt.Sprintf("%s_%s_%s", prefix, tableName, columnName)
}