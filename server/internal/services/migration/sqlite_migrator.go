package migration

import (
    "encoding/json"
    "fmt"
    "io"
    "strings"
    "time"

    "github.com/glebarez/sqlite"
    _ "github.com/glebarez/sqlite" // SQLite driver
    "gorm.io/gorm"
    "gorm.io/gorm/logger"
)

// SQLiteMigrator SQLite 数据库迁移器
type SQLiteMigrator struct {
    db *gorm.DB
}

// NewSQLiteMigrator 创建 SQLite 迁移器
func NewSQLiteMigrator(dsn string) (*SQLiteMigrator, error) {
    db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
        Logger: logger.Default.LogMode(4), // Silent mode
    })
    if err != nil {
        return nil, fmt.Errorf("SQLite 连接失败: %w", err)
    }
    return &SQLiteMigrator{db: db}, nil
}

// DriverName 返回驱动名称
func (m *SQLiteMigrator) DriverName() string {
    return "sqlite"
}

// TestConnection 测试数据库连接
func (m *SQLiteMigrator) TestConnection(dsn string) (*ConnectionInfo, error) {
    db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
        Logger: logger.Default.LogMode(4),
    })
    if err != nil {
        return &ConnectionInfo{Connected: false}, nil
    }
    defer func() {
        sqlDB, _ := db.DB()
        if sqlDB != nil {
            sqlDB.Close()
        }
    }()

    sqlDB, err := db.DB()
    if err != nil {
        return &ConnectionInfo{Connected: false}, err
    }

    var version string
    if err := sqlDB.QueryRow("SELECT sqlite_version()").Scan(&version); err != nil {
        return &ConnectionInfo{Connected: false}, err
    }

    // 获取表列表
    rows, err := sqlDB.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
    if err != nil {
        return &ConnectionInfo{Connected: false}, err
    }
    defer rows.Close()

    var tables []string
    for rows.Next() {
        var name string
        if err := rows.Scan(&name); err != nil {
            continue
        }
        tables = append(tables, name)
    }

    return &ConnectionInfo{
        Connected:     true,
        Version:       version,
        DatabaseName:  dsn,
        Tables:        tables,
        DatabaseEmpty: len(tables) == 0,
        EstimatedSize: "0 KB",
    }, nil
}

// GetDatabaseInfo 获取数据库信息
func (m *SQLiteMigrator) GetDatabaseInfo(dsn string) (*DBInfo, error) {
    db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
        Logger: logger.Default.LogMode(4),
    })
    if err != nil {
        return nil, err
    }
    defer func() {
        sqlDB, _ := db.DB()
        if sqlDB != nil {
            sqlDB.Close()
        }
    }()

    sqlDB, err := db.DB()
    if err != nil {
        return nil, err
    }

    var version string
    sqlDB.QueryRow("SELECT sqlite_version()").Scan(&version)

    // 获取表列表
    rows, err := sqlDB.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var tables []*TableInfo
    var totalRecords int64

    for rows.Next() {
        var name string
        if err := rows.Scan(&name); err != nil {
            continue
        }

        var count int64
        sqlDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", name)).Scan(&count)
        totalRecords += count

        tables = append(tables, &TableInfo{
            Name:        name,
            RecordCount: count,
        })
    }

    return &DBInfo{
        Driver:       "sqlite",
        Version:      version,
        DatabaseName: dsn,
        TableCount:   len(tables),
        RecordCount:  totalRecords,
        EstimatedMB:  0,
        Tables:       tables,
    }, nil
}

// ExportData 导出所有数据
func (m *SQLiteMigrator) ExportData(w io.Writer) error {
    tables, err := m.GetTables()
    if err != nil {
        return err
    }

    header := ExportHeader{
        Format:     ExportDataFormat,
        Driver:     "sqlite",
        Version:    m.getSQLiteVersion(),
        ExportedAt: time.Now(),
        Tables:     make([]string, 0, len(tables)),
    }

    for _, t := range tables {
        header.Tables = append(header.Tables, t.Name)
    }

    // 写入头部
    headerBytes, err := json.Marshal(header)
    if err != nil {
        return err
    }
    fmt.Fprintf(w, "%s\n", string(headerBytes))

    // 导出每个表
    for _, table := range tables {
        data, err := m.ExportTable(table.Name, w)
        if err != nil {
            return fmt.Errorf("导出表 %s 失败: %w", table.Name, err)
        }
        if _, err := w.Write(data); err != nil {
            return err
        }
    }

    return nil
}

// ExportTable 导出单个表
func (m *SQLiteMigrator) ExportTable(tableName string, w io.Writer) ([]byte, error) {
    // 获取 DDL
    ddl, err := m.GetTableDDL(tableName)
    if err != nil {
        return nil, err
    }

    // 获取列名
    columns, err := m.getTableColumns(tableName)
    if err != nil {
        return nil, err
    }

    // 获取数据
    rows, err := m.db.Table(tableName).Rows()
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    records := make([][]interface{}, 0)
    for rows.Next() {
        values, err := scanRow(rows, columns)
        if err != nil {
            continue
        }
        records = append(records, values)
    }

    data := ExportTableData{
        TableName: tableName,
        DDL:       ddl,
        Records:   records,
        Columns:   columns,
    }

    bytes, err := json.Marshal(data)
    if err != nil {
        return nil, err
    }

    fmt.Fprintf(w, "%s\n", string(bytes))
    return bytes, nil
}

// ImportData 导入数据
func (m *SQLiteMigrator) ImportData(r io.Reader) error {
    return nil // SQLite 不支持作为导入目标
}

// GetTables 获取所有表
func (m *SQLiteMigrator) GetTables() ([]*TableInfo, error) {
    type tableResult struct {
        Name string
    }

    var results []tableResult
    if err := m.db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name").Scan(&results).Error; err != nil {
        return nil, err
    }

    tables := make([]*TableInfo, 0, len(results))
    for _, t := range results {
        count, _ := m.GetTableRecordCount(t.Name)
        tables = append(tables, &TableInfo{
            Name:        t.Name,
            RecordCount: count,
        })
    }

    return tables, nil
}

// TransformDDL 转换 DDL (SQLite 输出为 SQLite 格式，无需转换)
func (m *SQLiteMigrator) TransformDDL(ddl string) string {
    return ddl
}

// GetTableDDL 获取表的 DDL
func (m *SQLiteMigrator) GetTableDDL(tableName string) (string, error) {
    var sql string
    err := m.db.Raw("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", tableName).Scan(&sql).Error
    return sql, err
}

// GetTableRecordCount 获取记录数
func (m *SQLiteMigrator) GetTableRecordCount(tableName string) (int64, error) {
    var count int64
    err := m.db.Table(tableName).Count(&count).Error
    return count, err
}

// Ping 测试连接
func (m *SQLiteMigrator) Ping() error {
    return m.db.Exec("SELECT 1").Error
}

// Close 关闭连接
func (m *SQLiteMigrator) Close() error {
    sqlDB, err := m.db.DB()
    if err != nil {
        return err
    }
    return sqlDB.Close()
}

// getSQLiteVersion 获取 SQLite 版本
func (m *SQLiteMigrator) getSQLiteVersion() string {
    var version string
    m.db.Raw("SELECT sqlite_version()").Scan(&version)
    return version
}

// getTableColumns 获取表列名
func (m *SQLiteMigrator) getTableColumns(tableName string) ([]string, error) {
    rows, err := m.db.Raw(fmt.Sprintf("PRAGMA table_info(%s)", tableName)).Rows()
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var columns []string
    for rows.Next() {
        var cid int
        var name, ctype string
        var notnull, pk int
        var dflt_value interface{}
        rows.Scan(&cid, &name, &ctype, &notnull, &dflt_value, &pk)
        columns = append(columns, name)
    }

    return columns, nil
}

// scanRow 扫描一行数据
func scanRow(rows interface{ Scan(...interface{}) error }, columns []string) ([]interface{}, error) {
    values := make([]interface{}, len(columns))
    valuePtrs := make([]interface{}, len(columns))
    for i := range values {
        valuePtrs[i] = &values[i]
    }
    if err := rows.Scan(valuePtrs...); err != nil {
        return nil, err
    }

    // 转换 []byte 到 string
    for i, v := range values {
        if b, ok := v.([]byte); ok {
            values[i] = string(b)
        }
    }

    return values, nil
}

// sqliteTypeToStandard 转换 SQLite 类型到标准类型
func sqliteTypeToStandard(sqliteType string) string {
    upperType := strings.ToUpper(sqliteType)
    if strings.HasPrefix(upperType, "INT") {
        return "INTEGER"
    } else if strings.HasPrefix(upperType, "VARCHAR") || strings.HasPrefix(upperType, "TEXT") {
        return "TEXT"
    } else if strings.HasPrefix(upperType, "REAL") || strings.HasPrefix(upperType, "FLOAT") || strings.HasPrefix(upperType, "DOUBLE") {
        return "REAL"
    } else if strings.HasPrefix(upperType, "BLOB") {
        return "BLOB"
    }
    return "TEXT"
}