package migration

import (
    "context"
    "fmt"
    "io"
    "strings"
    "time"

    _ "github.com/lib/pq"
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
    "gorm.io/gorm/logger"
)

// PostgresMigrator PostgreSQL 数据库迁移器
type PostgresMigrator struct {
    db *gorm.DB
}

// NewPostgresMigrator 创建 PostgreSQL 迁移器
func NewPostgresMigrator(dsn string) (*PostgresMigrator, error) {
    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
        Logger: logger.Default.LogMode(4),
    })
    if err != nil {
        return nil, fmt.Errorf("PostgreSQL 连接失败: %w", err)
    }
    return &PostgresMigrator{db: db}, nil
}

// DriverName 返回驱动名称
func (m *PostgresMigrator) DriverName() string {
    return "postgres"
}

// TestConnection 测试数据库连接（带5秒超时）
func (m *PostgresMigrator) TestConnection(dsn string) (*ConnectionInfo, error) {
    // 创建带超时的上下文
    ctx, cancel := context.WithTimeout(context.Background(), ConnectionTimeout)
    defer cancel()

    type result struct {
        info *ConnectionInfo
        err  error
    }
    resultCh := make(chan result, 1)

    go func() {
        db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
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

        sqlDB.SetConnMaxLifetime(ConnectionTimeout)

        var version string
        if err := sqlDB.QueryRow("SELECT version()").Scan(&version); err != nil {
            resultCh <- result{info: &ConnectionInfo{Connected: false, ErrorInfo: classifyError(err)}, err: nil}
            return
        }

        // 获取数据库名
        dbName := extractPostgresDBName(dsn)

        // 获取字符集（PostgreSQL 使用 encoding）
        var encoding string
        sqlDB.QueryRow("SELECT encoding FROM pg_database WHERE datname = current_database()").Scan(&encoding)

        // 获取表列表
        rows, err := sqlDB.Query(`
            SELECT table_name
            FROM information_schema.tables
            WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
            ORDER BY table_name
        `)
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
        sqlDB.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'").Scan(&tableCount)

        // 计算记录数
        var recordCount int64
        sqlDB.QueryRow(`
            SELECT COALESCE(SUM(reltuples), 0)
            FROM pg_class
            WHERE relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'public')
            AND reltype != 0
        `).Scan(&recordCount)

        // 计算大小
        var totalSize string
        sqlDB.QueryRow(`
            SELECT COALESCE(ROUND(SUM(pg_total_relation_size(schemaname||'.'||tablename))/1024/1024, 2), 0)
            FROM pg_tables
            WHERE schemaname = 'public'
        `).Scan(&totalSize)

        resultCh <- result{info: &ConnectionInfo{
            Connected:     true,
            Version:       version,
            CharacterSet:  encoding,
            TableCount:    tableCount,
            RecordCount:   recordCount,
            DatabaseName:  dbName,
            Tables:        tables,
            DatabaseEmpty: tableCount == 0,
            EstimatedSize: fmt.Sprintf("%s MB", totalSize),
            DatabaseInfo: &DatabaseInfo{
                Name:       dbName,
                Charset:    encoding,
                TableCount: tableCount,
                RecordCount: recordCount,
                DataSize:   fmt.Sprintf("%s MB", totalSize),
            },
        }, err: nil}
    }()

    select {
    case <-ctx.Done():
        return &ConnectionInfo{
            Connected: false,
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
func (m *PostgresMigrator) GetDatabaseInfo(dsn string) (*DBInfo, error) {
    db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
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
    sqlDB.QueryRow("SELECT version()").Scan(&version)

    dbName := extractPostgresDBName(dsn)

    // 获取表列表
    rows, err := sqlDB.Query(`
        SELECT table_name
        FROM information_schema.tables
        WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
        ORDER BY table_name
    `)
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
        Driver:       "postgres",
        Version:      version,
        DatabaseName: dbName,
        TableCount:   len(tables),
        RecordCount:  totalRecords,
        EstimatedMB:  0,
        Tables:       tables,
    }, nil
}

// ExportData 导出所有数据
func (m *PostgresMigrator) ExportData(w io.Writer) error {
    tables, err := m.GetTables()
    if err != nil {
        return err
    }

    header := ExportHeader{
        Format:     ExportDataFormat,
        Driver:     "postgres",
        Version:    m.getPostgresVersion(),
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
func (m *PostgresMigrator) ExportTable(tableName string, w io.Writer) ([]byte, error) {
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
func (m *PostgresMigrator) ImportData(r io.Reader) error {
    return nil // 实现导入逻辑
}

// GetTables 获取所有表
func (m *PostgresMigrator) GetTables() ([]*TableInfo, error) {
    type tableResult struct {
        TableName string `gorm:"column:table_name"`
    }

    var results []tableResult
    if err := m.db.Raw(`
        SELECT table_name
        FROM information_schema.tables
        WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
        ORDER BY table_name
    `).Scan(&results).Error; err != nil {
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

// TransformDDL 转换 DDL 以适配 PostgreSQL
func (m *PostgresMigrator) TransformDDL(ddl string) string {
    // SQLite/MySQL 到 PostgreSQL 的类型映射
    result := ddl

    // 替换 TINYINT(1) -> BOOLEAN (如果是布尔类型)
    result = replaceTypeRegex(result, `(?i)TINYINT\(1\)`, "BOOLEAN")

    // 替换 TEXT/JSON -> JSONB (性能更好)
    result = replaceTypeRegex(result, `(?i)\bJSON\b`, "JSONB")

    // BLOB -> BYTEA
    result = replaceTypeRegex(result, `(?i)\bBLOB\b`, "BYTEA")

    // DATETIME -> TIMESTAMP
    result = replaceTypeRegex(result, `(?i)\bDATETIME\b`, "TIMESTAMP")

    // INT -> INTEGER
    result = replaceTypeRegex(result, `(?i)\bINT\b`, "INTEGER")

    return result
}

// GetTableDDL 获取表的 DDL
func (m *PostgresMigrator) GetTableDDL(tableName string) (string, error) {
    // 手动构建 DDL
    columns, err := getPostgresColumns(m.db, tableName)
    if err != nil {
        return "", err
    }

    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("CREATE TABLE %s (\n", tableName))
    for i, col := range columns {
        if i > 0 {
            sb.WriteString(",\n")
        }
        sb.WriteString(fmt.Sprintf("  %s %s", col.Name, col.Type))
        if col.IsPrimaryKey {
            sb.WriteString(" PRIMARY KEY")
        }
        if !col.Nullable {
            sb.WriteString(" NOT NULL")
        }
        if col.Default != "" {
            sb.WriteString(fmt.Sprintf(" DEFAULT %s", col.Default))
        }
    }
    sb.WriteString("\n);")
    return sb.String(), nil
}

// PostgresColumn PostgreSQL 列信息
type PostgresColumn struct {
    Name         string
    Type         string
    Nullable     bool
    IsPrimaryKey bool
    Default      string
}

// getPostgresColumns 获取 PostgreSQL 列信息
func getPostgresColumns(db *gorm.DB, tableName string) ([]PostgresColumn, error) {
    rows, err := db.Raw(`
        SELECT
            column_name,
            data_type,
            is_nullable,
            COALESCE(column_default, ''),
            CASE WHEN EXISTS (
                SELECT 1 FROM information_schema.table_constraints tc
                JOIN information_schema.key_column_usage kcu
                ON tc.constraint_name = kcu.constraint_name
                WHERE tc.table_name = c.table_name
                AND kcu.column_name = c.column_name
                AND tc.constraint_type = 'PRIMARY KEY'
                AND tc.table_schema = c.table_schema
            ) THEN true ELSE false END as is_pk
        FROM information_schema.columns c
        WHERE table_name = $1 AND table_schema = 'public'
        ORDER BY ordinal_position
    `, tableName).Rows()
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var columns []PostgresColumn
    for rows.Next() {
        var col PostgresColumn
        var nullable string
        rows.Scan(&col.Name, &col.Type, &nullable, &col.Default, &col.IsPrimaryKey)
        col.Nullable = nullable == "YES"
        columns = append(columns, col)
    }
    return columns, nil
}

// GetTableRecordCount 获取记录数
func (m *PostgresMigrator) GetTableRecordCount(tableName string) (int64, error) {
    var count int64
    err := m.db.Table(tableName).Count(&count).Error
    return count, err
}

// Ping 测试连接
func (m *PostgresMigrator) Ping() error {
    return m.db.Exec("SELECT 1").Error
}

// Close 关闭连接
func (m *PostgresMigrator) Close() error {
    sqlDB, err := m.db.DB()
    if err != nil {
        return err
    }
    return sqlDB.Close()
}

// getPostgresVersion 获取 PostgreSQL 版本
func (m *PostgresMigrator) getPostgresVersion() string {
    var version string
    m.db.Raw("SELECT version()").Scan(&version)
    return version
}

// getTableColumns 获取表列名
func (m *PostgresMigrator) getTableColumns(tableName string) ([]string, error) {
    rows, err := m.db.Raw(`
        SELECT column_name
        FROM information_schema.columns
        WHERE table_name = $1 AND table_schema = 'public'
        ORDER BY ordinal_position
    `, tableName).Rows()
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var columns []string
    for rows.Next() {
        var name string
        rows.Scan(&name)
        columns = append(columns, name)
    }
    return columns, nil
}

// extractPostgresDBName 从 DSN 中提取数据库名
func extractPostgresDBName(dsn string) string {
    // 格式: host=localhost port=5432 user=postgres password=xxx dbname=mydb
    parts := strings.Split(dsn, " ")
    for _, part := range parts {
        if strings.HasPrefix(part, "dbname=") {
            return strings.TrimPrefix(part, "dbname=")
        }
        if strings.HasPrefix(part, "postgres://") || strings.HasPrefix(part, "postgresql://") {
            // URL 格式
            if idx := strings.LastIndex(part, "/"); idx > 0 {
                dbPart := part[idx+1:]
                if idx := strings.Index(dbPart, "?"); idx > 0 {
                    dbPart = dbPart[:idx]
                }
                return dbPart
            }
        }
    }
    return ""
}