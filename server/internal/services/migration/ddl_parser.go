package migration

import (
    "regexp"
    "strings"
)

// ColumnDefinition 列定义
type ColumnDefinition struct {
    Name         string
    Type         string
    NotNull      bool
    PrimaryKey   bool
    Default      string
    AutoIncrement bool
    Unique       bool
    Comment      string
}

// IndexDefinition 索引定义
type IndexDefinition struct {
    Name      string
    TableName string
    Columns   []string
    Unique    bool
    Type      string // BTREE, HASH, etc.
}

// ForeignKeyDefinition 外键定义
type ForeignKeyDefinition struct {
    Name         string
    TableName    string
    Column       string
    RefTable     string
    RefColumn    string
    OnDelete     string
    OnUpdate     string
}

// TableDefinition 表定义
type TableDefinition struct {
    Name         string
    Columns      []ColumnDefinition
    Indexes      []IndexDefinition
    ForeignKeys  []ForeignKeyDefinition
    PrimaryKey   []string
    DDL          string
}

// DDLParser DDL 解析器
type DDLParser struct{}

// NewDDLParser 创建 DDL 解析器
func NewDDLParser() *DDLParser {
    return &DDLParser{}
}

// ParseDDL 解析 DDL 语句
func (p *DDLParser) ParseDDL(ddl string) (*TableDefinition, error) {
    // 提取表名
    tableName := extractTableName(ddl)
    if tableName == "" {
        return nil, ErrInvalidDDL
    }

    // 提取括号内的定义
    start := strings.Index(ddl, "(")
    end := strings.LastIndex(ddl, ")")
    if start < 0 || end < 0 {
        return nil, ErrInvalidDDL
    }

    definitionsStr := ddl[start+1 : end]

    // 按逗号分割（处理嵌套括号）
    parts := splitDefinitions(definitionsStr)

    table := &TableDefinition{
        Name:         tableName,
        Columns:      []ColumnDefinition{},
        Indexes:      []IndexDefinition{},
        ForeignKeys:  []ForeignKeyDefinition{},
        PrimaryKey:   []string{},
        DDL:          ddl,
    }

    for _, part := range parts {
        part = strings.TrimSpace(part)
        if part == "" {
            continue
        }

        upper := strings.ToUpper(part)

        // 检测类型
        switch {
        case strings.HasPrefix(upper, "PRIMARY KEY"):
            table.PrimaryKey = extractPrimaryKeyColumns(part)
        case strings.HasPrefix(upper, "FOREIGN KEY"):
            fk := extractForeignKey(part, tableName)
            if fk != nil {
                table.ForeignKeys = append(table.ForeignKeys, *fk)
            }
        case strings.HasPrefix(upper, "UNIQUE") || strings.HasPrefix(upper, "KEY ") || strings.HasPrefix(upper, "INDEX "):
            idx := extractIndex(part, tableName)
            if idx != nil {
                table.Indexes = append(table.Indexes, *idx)
            }
        case strings.HasPrefix(upper, "CONSTRAINT"):
            // 检查约束类型
            if strings.Contains(upper, "FOREIGN KEY") {
                fk := extractForeignKey(part, tableName)
                if fk != nil {
                    table.ForeignKeys = append(table.ForeignKeys, *fk)
                }
            } else if strings.Contains(upper, "UNIQUE") {
                idx := extractIndex(part, tableName)
                if idx != nil {
                    table.Indexes = append(table.Indexes, *idx)
                }
            }
        default:
            // 列定义
            col := parseColumnDefinition(part)
            if col.Name != "" {
                table.Columns = append(table.Columns, col)
                if col.PrimaryKey {
                    table.PrimaryKey = append(table.PrimaryKey, col.Name)
                }
            }
        }
    }

    return table, nil
}

// extractTableName 提取表名
func extractTableName(ddl string) string {
    upper := strings.ToUpper(ddl)
    upper = strings.Replace(upper, "`", "", -1)

    if idx := strings.Index(upper, "CREATE TABLE"); idx >= 0 {
        rest := strings.TrimSpace(ddl[idx+12:])
        rest = strings.TrimPrefix(rest, "IF NOT EXISTS")
        rest = strings.TrimSpace(rest)

        parts := strings.SplitN(rest, "(", 2)
        if len(parts) > 0 {
            name := strings.TrimSpace(parts[0])
            name = strings.Trim(name, `"' `)
            return name
        }
    }
    return ""
}

// splitDefinitions 按逗号分割定义（处理嵌套括号）
func splitDefinitions(s string) []string {
    var parts []string
    var current strings.Builder
    depth := 0

    for _, ch := range s {
        switch ch {
        case '(':
            depth++
            current.WriteRune(ch)
        case ')':
            depth--
            current.WriteRune(ch)
        case ',':
            if depth == 0 {
                parts = append(parts, current.String())
                current.Reset()
            } else {
                current.WriteRune(ch)
            }
        default:
            current.WriteRune(ch)
        }
    }

    if current.Len() > 0 {
        parts = append(parts, current.String())
    }

    return parts
}

// parseColumnDefinition 解析列定义
func parseColumnDefinition(s string) ColumnDefinition {
    col := ColumnDefinition{}

    // 使用正则提取列名和类型
    re := regexp.MustCompile(`^\s*(\w+)\s+(\w+(?:\([^)]+\))?)\s*(.*)$`)
    matches := re.FindStringSubmatch(s)
    if len(matches) < 3 {
        return col
    }

    col.Name = strings.Trim(matches[1], "`\"'")
    col.Type = matches[2]
    rest := matches[3]

    // 检测约束
    restUpper := strings.ToUpper(rest)
    col.NotNull = strings.Contains(restUpper, "NOT NULL")
    col.PrimaryKey = strings.Contains(restUpper, "PRIMARY KEY")
    col.Unique = strings.Contains(restUpper, "UNIQUE")
    col.AutoIncrement = strings.Contains(restUpper, "AUTO_INCREMENT") ||
        strings.Contains(restUpper, "AUTOINCREMENT") ||
        strings.Contains(restUpper, "GENERATED")

    // 提取默认值
    if idx := strings.Index(restUpper, "DEFAULT"); idx >= 0 {
        defaultPart := rest[idx+7:]
        defaultPart = strings.TrimSpace(defaultPart)
        // 提取到下一个关键字
        keywords := []string{"NOT", "PRIMARY", "UNIQUE", "AUTO", "COMMENT"}
        minIdx := len(defaultPart)
        for _, kw := range keywords {
            if idx := strings.Index(defaultPart, kw); idx >= 0 && idx < minIdx {
                minIdx = idx
            }
        }
        col.Default = strings.TrimSpace(defaultPart[:minIdx])
    }

    return col
}

// extractPrimaryKeyColumns 提取主键列
func extractPrimaryKeyColumns(s string) []string {
    s = strings.ToUpper(s)
    s = strings.Replace(s, "PRIMARY KEY", "", 1)
    s = strings.TrimSpace(s)
    s = strings.Trim(s, "()")

    var columns []string
    for _, col := range strings.Split(s, ",") {
        col = strings.TrimSpace(col)
        col = strings.Trim(col, "`\"'")
        if col != "" {
            columns = append(columns, col)
        }
    }
    return columns
}

// extractForeignKey 提取外键定义
func extractForeignKey(s, tableName string) *ForeignKeyDefinition {
    fk := &ForeignKeyDefinition{TableName: tableName}

    // 提取外键名
    nameRe := regexp.MustCompile(`(?i)CONSTRAINT\s+(\w+)`)
    nameMatch := nameRe.FindStringSubmatch(s)
    if len(nameMatch) > 1 {
        fk.Name = nameMatch[1]
    }

    // 提取列
    colRe := regexp.MustCompile(`(?i)FOREIGN\s+KEY\s*\(([^)]+)\)`)
    colMatch := colRe.FindStringSubmatch(s)
    if len(colMatch) > 1 {
        fk.Column = strings.Trim(strings.TrimSpace(colMatch[1]), "`\"'")
    }

    // 提取引用表和列
    refRe := regexp.MustCompile(`(?i)REFERENCES\s+(\w+)\s*\(([^)]+)\)`)
    refMatch := refRe.FindStringSubmatch(s)
    if len(refMatch) > 2 {
        fk.RefTable = refMatch[1]
        fk.RefColumn = strings.Trim(strings.TrimSpace(refMatch[2]), "`\"'")
    }

    // 提取 ON DELETE/UPDATE
    if strings.Contains(strings.ToUpper(s), "ON DELETE CASCADE") {
        fk.OnDelete = "CASCADE"
    } else if strings.Contains(strings.ToUpper(s), "ON DELETE SET NULL") {
        fk.OnDelete = "SET NULL"
    } else if strings.Contains(strings.ToUpper(s), "ON DELETE RESTRICT") {
        fk.OnDelete = "RESTRICT"
    }

    if strings.Contains(strings.ToUpper(s), "ON UPDATE CASCADE") {
        fk.OnUpdate = "CASCADE"
    } else if strings.Contains(strings.ToUpper(s), "ON UPDATE SET NULL") {
        fk.OnUpdate = "SET NULL"
    } else if strings.Contains(strings.ToUpper(s), "ON UPDATE RESTRICT") {
        fk.OnUpdate = "RESTRICT"
    }

    if fk.Column != "" && fk.RefTable != "" {
        return fk
    }
    return nil
}

// extractIndex 提取索引定义
func extractIndex(s, tableName string) *IndexDefinition {
    idx := &IndexDefinition{TableName: tableName}

    upper := strings.ToUpper(s)

    // 提取索引名
    nameRe := regexp.MustCompile(`(?i)(?:UNIQUE\s+)?(?:KEY|INDEX)\s+(\w+)`)
    nameMatch := nameRe.FindStringSubmatch(s)
    if len(nameMatch) > 1 {
        idx.Name = nameMatch[1]
    }

    idx.Unique = strings.HasPrefix(upper, "UNIQUE")

    // 提取列
    colRe := regexp.MustCompile(`\(([^)]+)\)`)
    colMatch := colRe.FindStringSubmatch(s)
    if len(colMatch) > 1 {
        for _, col := range strings.Split(colMatch[1], ",") {
            col = strings.TrimSpace(col)
            col = strings.Trim(col, "`\"'")
            // 提取列名（可能包含 ASC/DESC）
            col = strings.Fields(col)[0]
            if col != "" {
                idx.Columns = append(idx.Columns, col)
            }
        }
    }

    if len(idx.Columns) > 0 {
        return idx
    }
    return nil
}

// ErrInvalidDDL 无效的 DDL 错误
var ErrInvalidDDL = &ParseError{Message: "无效的 DDL 语句"}

// ParseError 解析错误
type ParseError struct {
    Message string
}

func (e *ParseError) Error() string {
    return e.Message
}