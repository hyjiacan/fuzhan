package migration

import (
    "fmt"
    "strings"
)

// ForeignKeyHandler 外键处理器
type ForeignKeyHandler struct {
    targetDriver string
}

// NewForeignKeyHandler 创建外键处理器
func NewForeignKeyHandler(targetDriver string) *ForeignKeyHandler {
    return &ForeignKeyHandler{targetDriver: targetDriver}
}

// BuildAddForeignKeySQL 构建添加外键 SQL
func (h *ForeignKeyHandler) BuildAddForeignKeySQL(fk ForeignKeyDefinition) string {
    var sql strings.Builder

    sql.WriteString("ALTER TABLE ")
    switch h.targetDriver {
    case "mysql":
        sql.WriteString("`")
        sql.WriteString(fk.TableName)
        sql.WriteString("` ADD CONSTRAINT ")
        sql.WriteString("`")
        sql.WriteString(fk.Name)
        sql.WriteString("` ")
    case "postgres":
        sql.WriteString("\"")
        sql.WriteString(fk.TableName)
        sql.WriteString("\" ADD CONSTRAINT ")
        sql.WriteString("\"")
        sql.WriteString(fk.Name)
        sql.WriteString("\" ")
    case "sqlite":
        // SQLite 不支持添加外键约束，只能在表定义时指定
        return ""
    }

    sql.WriteString("FOREIGN KEY (")
    switch h.targetDriver {
    case "mysql":
        sql.WriteString("`")
        sql.WriteString(fk.Column)
        sql.WriteString("`) REFERENCES ")
        sql.WriteString("`")
        sql.WriteString(fk.RefTable)
        sql.WriteString("`")
        sql.WriteString("(`")
        sql.WriteString(fk.RefColumn)
        sql.WriteString("`)")
    case "postgres", "sqlite":
        sql.WriteString("\"")
        sql.WriteString(fk.Column)
        sql.WriteString("\") REFERENCES ")
        sql.WriteString("\"")
        sql.WriteString(fk.RefTable)
        sql.WriteString("\"")
        sql.WriteString("(\"")
        sql.WriteString(fk.RefColumn)
        sql.WriteString("\")")
    }

    // ON DELETE
    if fk.OnDelete != "" {
        sql.WriteString(" ON DELETE ")
        sql.WriteString(fk.OnDelete)
    }

    // ON UPDATE
    if fk.OnUpdate != "" {
        sql.WriteString(" ON UPDATE ")
        sql.WriteString(fk.OnUpdate)
    }

    return sql.String()
}

// BuildDropForeignKeySQL 构建删除外键 SQL
func (h *ForeignKeyHandler) BuildDropForeignKeySQL(fk ForeignKeyDefinition) string {
    var sql strings.Builder

    sql.WriteString("ALTER TABLE ")
    switch h.targetDriver {
    case "mysql":
        sql.WriteString("`")
        sql.WriteString(fk.TableName)
        sql.WriteString("` DROP FOREIGN KEY ")
        sql.WriteString("`")
        sql.WriteString(fk.Name)
        sql.WriteString("`")
    case "postgres":
        sql.WriteString("\"")
        sql.WriteString(fk.TableName)
        sql.WriteString("\" DROP CONSTRAINT ")
        sql.WriteString("\"")
        sql.WriteString(fk.Name)
        sql.WriteString("\"")
    case "sqlite":
        return ""
    }

    return sql.String()
}

// BuildAllForeignKeys 批量构建外键 SQL
func (h *ForeignKeyHandler) BuildAllForeignKeys(table *TableDefinition) []string {
    var sqls []string
    for _, fk := range table.ForeignKeys {
        sql := h.BuildAddForeignKeySQL(fk)
        if sql != "" {
            sqls = append(sqls, sql)
        }
    }
    return sqls
}

// ValidateForeignKey 验证外键定义是否有效
func ValidateForeignKey(fk ForeignKeyDefinition) error {
    if fk.TableName == "" {
        return fmt.Errorf("外键表名不能为空")
    }
    if fk.Column == "" {
        return fmt.Errorf("外键列名不能为空")
    }
    if fk.RefTable == "" {
        return fmt.Errorf("引用表名不能为空")
    }
    if fk.RefColumn == "" {
        return fmt.Errorf("引用列名不能为空")
    }
    return nil
}

// GenerateForeignKeyName 生成默认外键名
func GenerateForeignKeyName(tableName, columnName string) string {
    return fmt.Sprintf("fk_%s_%s", tableName, columnName)
}

// ResolveForeignKeyDependencies 解析外键依赖顺序
// 确保被引用的表在外键创建前已存在
func ResolveForeignKeyDependencies(tables []*TableDefinition) ([][]ForeignKeyDefinition, error) {
    // 建立表名到定义的映射
    tableMap := make(map[string]*TableDefinition)
    for _, t := range tables {
        tableMap[strings.ToLower(t.Name)] = t
    }

    // 按顺序返回外键：先处理没有被其他表引用的表
    var phases [][]ForeignKeyDefinition

    // 第一阶段：没有外键依赖的表
    phase := []ForeignKeyDefinition{}
    for _, t := range tables {
        hasDependency := false
        for _, fk := range t.ForeignKeys {
            refTable := strings.ToLower(fk.RefTable)
            if _, exists := tableMap[refTable]; exists {
                // 引用了其他表
                hasDependency = true
                break
            }
        }
        if !hasDependency {
            phase = append(phase, t.ForeignKeys...)
        }
    }
    if len(phase) > 0 {
        phases = append(phases, phase)
    }

    // 第二阶段：有外键依赖的表
    phase = []ForeignKeyDefinition{}
    for _, t := range tables {
        for _, fk := range t.ForeignKeys {
            refTable := strings.ToLower(fk.RefTable)
            if _, exists := tableMap[refTable]; exists {
                phase = append(phase, fk)
            }
        }
    }
    if len(phase) > 0 {
        phases = append(phases, phase)
    }

    return phases, nil
}