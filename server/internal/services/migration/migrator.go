package migration

import (
    "io"
    "time"
)

// ConnectionInfo 数据库连接信息
type ConnectionInfo struct {
    Connected      bool          // 是否连接成功
    Version        string        // 数据库版本
    CharacterSet   string        // 字符集
    TableCount     int           // 表数量
    RecordCount    int64         // 总记录数
    DatabaseName   string        // 数据库名称
    Tables         []string      // 表名列表
    DatabaseEmpty  bool          // 数据库是否为空
    EstimatedSize  string        // 预估大小
    ServerInfo     *ServerInfo   // 服务器信息
    DatabaseInfo   *DatabaseInfo // 数据库信息
    ErrorInfo      *MigrationError // 错误信息（连接失败时）
}

// ServerInfo 服务器信息
type ServerInfo struct {
    OS     string // 操作系统
    Arch   string // 架构
    Uptime string // 运行时间
}

// DatabaseInfo 数据库信息
type DatabaseInfo struct {
    Name       string // 库名
    Charset    string // 字符集
    TableCount int    // 表数
    RecordCount int64 // 记录数
    DataSize   string // 数据大小
    IndexSize  string // 索引大小
}

// DBInfo 数据库信息
type DBInfo struct {
    Driver       string    // 数据库类型
    Version      string    // 版本
    DatabaseName string    // 数据库名
    TableCount   int       // 表数量
    RecordCount  int64     // 总记录数
    EstimatedMB  float64   // 预估大小(MB)
    Tables       []*TableInfo // 表信息
}

// TableInfo 表信息
type TableInfo struct {
    Name         string // 表名
    RecordCount  int64  // 记录数
    DDL          string // 建表语句
    Indexes      []string // 索引列表
}

// DatabaseMigrator 数据库迁移接口
type DatabaseMigrator interface {
    // DriverName 返回驱动名称
    DriverName() string

    // TestConnection 测试数据库连接
    TestConnection(dsn string) (*ConnectionInfo, error)

    // GetDatabaseInfo 获取数据库信息
    GetDatabaseInfo(dsn string) (*DBInfo, error)

    // ExportData 导出数据到 Writer (JSON 格式)
    ExportData(w io.Writer) error

    // ExportTable 导出单个表数据
    ExportTable(tableName string, w io.Writer) ([]byte, error)

    // ImportData 从 Reader 导入数据 (JSON 格式)
    ImportData(r io.Reader) error

    // GetTables 获取所有表信息
    GetTables() ([]*TableInfo, error)

    // TransformDDL 转换 DDL 语句以适配目标数据库
    TransformDDL(ddl string) string

    // GetTableDDL 获取表的 DDL 语句
    GetTableDDL(tableName string) (string, error)

    // GetTableRecordCount 获取表记录数
    GetTableRecordCount(tableName string) (int64, error)

    // Ping 测试连接是否活跃
    Ping() error

    // Close 关闭连接
    Close() error
}

// ExportDataFormat 导出数据格式版本
const ExportDataFormat = "1.0"

// ExportHeader 导出数据头部
type ExportHeader struct {
    Format     string    `json:"format"`      // 格式版本
    Driver     string    `json:"driver"`      // 源数据库类型
    Version    string    `json:"version"`     // 数据库版本
    ExportedAt time.Time `json:"exported_at"` // 导出时间
    Tables     []string  `json:"tables"`      // 表名列表
}

// ExportTableData 单表导出数据
type ExportTableData struct {
    TableName   string              `json:"table_name"` // 表名
    DDL         string              `json:"ddl"`        // 建表语句
    Records     [][]interface{}     `json:"records"`    // 记录数据
    Columns     []string            `json:"columns"`    // 列名列表
    RecordCount int64               `json:"record_count,omitempty"` // 记录数（计算属性）
}

// ImportDataHeader 导入数据头部
type ImportDataHeader struct {
    Format     string    `json:"format"`
    Driver     string    `json:"driver"`
    Version    string    `json:"version"`
    ExportedAt time.Time `json:"exported_at"`
    Tables     []string  `json:"tables"`
}

// ImportTableData 导入表数据
type ImportTableData struct {
    TableName string            `json:"table_name"`
    DDL       string            `json:"ddl"`
    Records   []map[string]interface{} `json:"records"`
    Columns   []string          `json:"columns"`
}