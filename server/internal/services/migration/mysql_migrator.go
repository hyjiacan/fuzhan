package migration

import (
    "context"
    "fmt"
    "io"
    "strings"
    "time"

    _ "github.com/go-sql-driver/mysql"
    "gorm.io/driver/mysql"
    "gorm.io/gorm"
    "gorm.io/gorm/logger"
)

// MySQLMigrator MySQL 数据库迁移器
type MySQLMigrator struct {
    db *gorm.DB
}

// NewMySQLMigrator 创建 MySQL 迁移器
func NewMySQLMigrator(dsn string) (*MySQLMigrator, error) {
    db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
        Logger: logger.Default.LogMode(4),
    })
    if err != nil {
        return nil, fmt.Errorf("MySQL 连接失败: %w", err)
    }
    return &MySQLMigrator{db: db}, nil
}

// DriverName 返回驱动名称
func (m *MySQLMigrator) DriverName() string {
    return "mysql"
}

// TestConnection 测试数据库连接（带5秒超时）
func (m *MySQLMigrator) TestConnection(dsn string) (*ConnectionInfo, error) {
    // 创建带超时的上下文
    ctx, cancel := context.WithTimeout(context.Background(), ConnectionTimeout)
    defer cancel()

    // 创建一个channel来接收结果
    type result struct {
        info *ConnectionInfo
        err  error
    }
    resultCh := make(chan result, 1)

    go func() {
        db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
            Logger: logger.Default.LogMode(4),
        })
        if err != nil {
            resultCh <- result{info: &ConnectionInfo{Connected: false, ErrorInfo: classifyError(err)}, err: nil}
            return
        }
        defer func() {
            sqlDB, _ := db.DB()
            if sqlDB != nil {
                sqlDB.Close()
            }
        }()

        sqlDB, err := db.DB()
        if err != nil {
            resultCh <- result{info: &ConnectionInfo{Connected: false, ErrorInfo: classifyError(err)}, err: nil}
            return
        }

        // 设置连接超时
        sqlDB.SetConnMaxLifetime(ConnectionTimeout)

        var version string
        if err := sqlDB.QueryRow("SELECT VERSION()").Scan(&version); err != nil {
            resultCh <- result{info: &ConnectionInfo{Connected: false, ErrorInfo: classifyError(err)}, err: nil}
            return
        }

        // 获取数据库名
        dbName := extractDBName(dsn)

        // 获取字符集
        var charset string
        sqlDB.QueryRow("SELECT DEFAULT_CHARACTER_SET_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", dbName).Scan(&charset)

        // 获取表列表
        rows, err := sqlDB.Query("SHOW TABLES")
        if err != nil {
            resultCh <- result{info: &ConnectionInfo{Connected: false, ErrorInfo: classifyError(err)}, err: nil}
            return
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

        // 检查数据库是否为空
        var tableCount int
        sqlDB.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ?", dbName).Scan(&tableCount)

        // 计算记录数和大小
        var recordCount int64
        var dataSize, indexSize string
        sqlDB.QueryRow(`
            SELECT
                COALESCE(SUM(TABLE_ROWS), 0),
                COALESCE(ROUND(SUM(DATA_LENGTH)/1024/1024, 2), 0),
                COALESCE(ROUND(SUM(INDEX_LENGTH)/1024/1024, 2), 0)
            FROM information_schema.tables WHERE table_schema = ?
        `, dbName).Scan(&recordCount, &dataSize, &indexSize)

        resultCh <- result{info: &ConnectionInfo{
            Connected:     true,
            Version:       version,
            CharacterSet:  charset,
            TableCount:    tableCount,
            RecordCount:   recordCount,
            DatabaseName:  dbName,
            Tables:        tables,
            DatabaseEmpty: tableCount == 0,
            EstimatedSize: fmt.Sprintf("%s MB", dataSize),
            DatabaseInfo: &DatabaseInfo{
                Name:       dbName,
                Charset:    charset,
                TableCount: tableCount,
                DataSize:   fmt.Sprintf("%s MB", dataSize),
                IndexSize:  fmt.Sprintf("%s MB", indexSize),
            },
        }, err: nil}
    }()

    select {
    case <-ctx.Done():
        return &ConnectionInfo{
            Connected:   false,
            ErrorInfo: &MigrationError{
                Code:       ErrorCodeConnectTimeout,
                Message:    "连接超时",
                Details:    map[string]string{"timeout": ConnectionTimeout.String()},
                CanRetry:   true,
                Suggestion: "请检查网络连接和防火墙设置",
            },
        }, nil
    case res := <-resultCh:
        return res.info, res.err
    }
}

// GetDatabaseInfo 获取数据库信息
func (m *MySQLMigrator) GetDatabaseInfo(dsn string) (*DBInfo, error) {
    db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
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
    sqlDB.QueryRow("SELECT VERSION()").Scan(&version)

    dbName := extractDBName(dsn)

    // 获取表列表
    rows, err := sqlDB.Query("SHOW TABLES")
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
        Driver:       "mysql",
        Version:      version,
        DatabaseName: dbName,
        TableCount:   len(tables),
        RecordCount:  totalRecords,
        EstimatedMB:  0,
        Tables:       tables,
    }, nil
}

// ExportData 导出所有数据
func (m *MySQLMigrator) ExportData(w io.Writer) error {
    tables, err := m.GetTables()
    if err != nil {
        return err
    }

    header := ExportHeader{
        Format:     ExportDataFormat,
        Driver:     "mysql",
        Version:    m.getMySQLVersion(),
        ExportedAt: time.Now(),
        Tables:     make([]string, 0, len(tables)),
    }

    for _, t := range tables {
        header.Tables = append(header.Tables, t.Name)
    }

    // 写入头部
    headerBytes, err := defaultJSONMarshal(header)
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
func (m *MySQLMigrator) ExportTable(tableName string, w io.Writer) ([]byte, error) {
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

    bytes, err := defaultJSONMarshal(data)
    if err != nil {
        return nil, err
    }

    fmt.Fprintf(w, "%s\n", string(bytes))
    return bytes, nil
}

// ImportData 导入数据
func (m *MySQLMigrator) ImportData(r io.Reader) error {
    return nil // 实现导入逻辑
}

// GetTables 获取所有表
func (m *MySQLMigrator) GetTables() ([]*TableInfo, error) {
    type tableResult struct {
        TableName string `gorm:"column:Tables_in_database"`
    }

    var results []tableResult
    if err := m.db.Raw("SHOW TABLES").Scan(&results).Error; err != nil {
        return nil, err
    }

    tables := make([]*TableInfo, 0, len(results))
    for _, t := range results {
        count, _ := m.GetTableRecordCount(t.TableName)
        tables = append(tables, &TableInfo{
            Name:        t.TableName,
            RecordCount: count,
        })
    }

    return tables, nil
}

// TransformDDL 转换 DDL 以适配 MySQL
func (m *MySQLMigrator) TransformDDL(ddl string) string {
    // SQLite 到 MySQL 的类型映射
    result := ddl

    // 替换 BOOLEAN -> TINYINT(1)
    result = replaceTypeRegex(result, `(?i)\bBOOLEAN\b`, "TINYINT(1)")

    // 替换 BLOB -> LONGBLOB
    result = replaceTypeRegex(result, `(?i)\bBLOB\b`, "LONGBLOB")

    // JSON 类型保持或转为 TEXT
    result = replaceTypeRegex(result, `(?i)\bJSON\b`, "JSON")

    return result
}

// GetTableDDL 获取表的 DDL
func (m *MySQLMigrator) GetTableDDL(tableName string) (string, error) {
    var ddl string
    err := m.db.Raw(fmt.Sprintf("SHOW CREATE TABLE %s", tableName)).Scan(&ddl).Error
    return ddl, err
}

// GetTableRecordCount 获取记录数
func (m *MySQLMigrator) GetTableRecordCount(tableName string) (int64, error) {
    var count int64
    err := m.db.Table(tableName).Count(&count).Error
    return count, err
}

// Ping 测试连接
func (m *MySQLMigrator) Ping() error {
    return m.db.Exec("SELECT 1").Error
}

// Close 关闭连接
func (m *MySQLMigrator) Close() error {
    sqlDB, err := m.db.DB()
    if err != nil {
        return err
    }
    return sqlDB.Close()
}

// getMySQLVersion 获取 MySQL 版本
func (m *MySQLMigrator) getMySQLVersion() string {
    var version string
    m.db.Raw("SELECT VERSION()").Scan(&version)
    return version
}

// getTableColumns 获取表列名
func (m *MySQLMigrator) getTableColumns(tableName string) ([]string, error) {
    rows, err := m.db.Raw(fmt.Sprintf("SHOW COLUMNS FROM %s", tableName)).Rows()
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var columns []string
    for rows.Next() {
        var field, colType string
        var null, key string
        var defaultVal, extra interface{}
        rows.Scan(&field, &colType, &null, &key, &defaultVal, &extra)
        columns = append(columns, field)
    }

    return columns, nil
}

// extractDBName 从 DSN 中提取数据库名
func extractDBName(dsn string) string {
    // 格式: user:password@tcp(host:port)/dbname
    parts := strings.Split(dsn, "/")
    if len(parts) > 1 {
        dbPart := parts[len(parts)-1]
        // 可能包含查询参数
        if idx := strings.Index(dbPart, "?"); idx > 0 {
            dbPart = dbPart[:idx]
        }
        return dbPart
    }
    return ""
}