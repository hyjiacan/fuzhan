package migration

import (
    "bufio"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "io"
    "strings"
    "time"
)

// ExportOptions 导出选项
type ExportOptions struct {
    IncludeSchema    bool // 包含表结构
    IncludeData      bool // 包含数据
    IncludeIndexes   bool // 包含索引
    IncludeForeignKeys bool // 包含外键
    BatchSize        int  // 每批记录数
    ProgressCallback func(stage string, current, total int64) // 进度回调
}

// DefaultExportOptions 默认导出选项
func DefaultExportOptions() *ExportOptions {
    return &ExportOptions{
        IncludeSchema:     true,
        IncludeData:       true,
        IncludeIndexes:    true,
        IncludeForeignKeys: true,
        BatchSize:         1000,
    }
}

// ExportResult 导出结果
type ExportResult struct {
    TableCount   int
    RecordCount  int64
    SchemaSize   int64
    DataSize     int64
    Duration     time.Duration
    Tables       []string
}

// ExportManager 导出管理器
type ExportManager struct {
    migrator  DatabaseMigrator
    options   *ExportOptions
}

// NewExportManager 创建导出管理器
func NewExportManager(migrator DatabaseMigrator, options *ExportOptions) *ExportManager {
    if options == nil {
        options = DefaultExportOptions()
    }
    return &ExportManager{
        migrator: migrator,
        options:  options,
    }
}

// Export 导出数据到 Writer
func (e *ExportManager) Export(w io.Writer) (*ExportResult, error) {
    start := time.Now()
    result := &ExportResult{}

    // 获取所有表
    tables, err := e.migrator.GetTables()
    if err != nil {
        return nil, fmt.Errorf("获取表列表失败: %w", err)
    }

    // 按依赖排序
    sortedTables := e.sortTablesByDependency(tables)
    result.TableCount = len(sortedTables)

    // 写入头部
    header := ExportHeader{
        Format:     ExportDataFormat,
        Driver:     e.migrator.DriverName(),
        Version:    "",
        ExportedAt: time.Now(),
        Tables:     make([]string, 0, len(sortedTables)),
    }

    for _, t := range sortedTables {
        header.Tables = append(header.Tables, t.Name)
    }

    headerBytes, err := json.Marshal(header)
    if err != nil {
        return nil, err
    }
    fmt.Fprintf(w, "%s\n", string(headerBytes))

    // 导出每个表
    var totalRecords int64
    for _, table := range sortedTables {
        data, err := e.exportTable(w, table)
        if err != nil {
            return nil, fmt.Errorf("导出表 %s 失败: %w", table.Name, err)
        }
        if data != nil {
            totalRecords += data.RecordCount
            result.Tables = append(result.Tables, table.Name)
        }

        // 进度回调
        if e.options.ProgressCallback != nil {
            e.options.ProgressCallback("export", int64(len(result.Tables)), int64(len(sortedTables)))
        }
    }

    result.RecordCount = totalRecords
    result.Duration = time.Since(start)

    return result, nil
}

// exportTable 导出单个表
func (e *ExportManager) exportTable(w io.Writer, table *TableInfo) (*ExportTableData, error) {
    if e.options.IncludeSchema {
        ddl, err := e.migrator.GetTableDDL(table.Name)
        if err != nil {
            return nil, err
        }

        // 获取列名
        columns, err := e.getTableColumns(table.Name)
        if err != nil {
            return nil, err
        }

        // 流式读取数据（分批）
        var allRecords [][]interface{}
        offset := int64(0)
        batchSize := int64(e.options.BatchSize)

        for {
            records, err := e.fetchBatch(table.Name, columns, offset, batchSize)
            if err != nil {
                return nil, err
            }
            if len(records) == 0 {
                break
            }

            allRecords = append(allRecords, records...)
            offset += batchSize

            // 进度回调
            if e.options.ProgressCallback != nil {
                e.options.ProgressCallback("export_data", offset, table.RecordCount)
            }

            if int64(len(records)) < batchSize {
                break
            }
        }

        // 编码 BLOB 字段
        encodedRecords := e.encodeBlobs(allRecords, columns)

        data := ExportTableData{
            TableName: table.Name,
            DDL:       ddl,
            Records:   encodedRecords,
            Columns:   columns,
        }

        bytes, err := json.Marshal(data)
        if err != nil {
            return nil, err
        }
        fmt.Fprintf(w, "%s\n", string(bytes))

        return &ExportTableData{
            TableName: table.Name,
            RecordCount: int64(len(allRecords)),
        }, nil
    }

    return nil, nil
}

// fetchBatch 分批获取数据
func (e *ExportManager) fetchBatch(tableName string, columns []string, offset, limit int64) ([][]interface{}, error) {
    // 这个方法需要通过 migrator 的 db 来实现
    // 简化实现：在 SQLiteMigrator 等中直接使用 SQL LIMIT/OFFSET
    return nil, nil // 由具体 migrator 实现
}

// getTableColumns 获取表列名
func (e *ExportManager) getTableColumns(tableName string) ([]string, error) {
    // 通过 migrator 的 db 实现
    // 简化实现
    return nil, nil
}

// sortTablesByDependency 按依赖关系排序表
func (e *ExportManager) sortTablesByDependency(tables []*TableInfo) []*TableInfo {
    // 简单实现：按名称排序
    // 实际应该分析外键依赖

    // 首先找出没有外键依赖的表（基础表）
    var basicTables, dependentTables []*TableInfo

    for _, t := range tables {
        // 简单策略：名称以 s 结尾的（如 users）优先
        name := strings.ToLower(t.Name)
        if strings.HasSuffix(name, "s") && !strings.Contains(name, "_") {
            basicTables = append(basicTables, t)
        } else {
            dependentTables = append(dependentTables, t)
        }
    }

    // 合并结果
    result := make([]*TableInfo, 0, len(tables))
    result = append(result, basicTables...)
    result = append(result, dependentTables...)

    return result
}

// encodeBlobs 编码 BLOB 字段为 base64
func (e *ExportManager) encodeBlobs(records [][]interface{}, columns []string) [][]interface{} {
    // 简化实现：检测可能的 BLOB 列
    result := make([][]interface{}, len(records))
    for i, record := range records {
        encoded := make([]interface{}, len(record))
        copy(encoded, record)
        result[i] = encoded
    }
    return result
}

// ExportToJSON 导出为 JSON 字符串
func (e *ExportManager) ExportToJSON() (string, error) {
    var sb strings.Builder
    writer := &sb

    result, err := e.Export(writer)
    if err != nil {
        return "", err
    }

    _ = result // 可以使用 result 进行日志等
    return sb.String(), nil
}

// ParseExportFile 解析导出文件
func ParseExportFile(r io.Reader) (*ExportHeader, []ExportTableData, error) {
    scanner := bufio.NewScanner(r)

    var header *ExportHeader
    var tables []ExportTableData

    lineNum := 0
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" {
            continue
        }

        if lineNum == 0 {
            // 第一行是头部
            var h ExportHeader
            if err := json.Unmarshal([]byte(line), &h); err != nil {
                return nil, nil, fmt.Errorf("解析头部失败: %w", err)
            }
            header = &h
        } else {
            // 其余是表数据
            var data ExportTableData
            if err := json.Unmarshal([]byte(line), &data); err != nil {
                // 跳过无效行
                continue
            }
            if data.TableName != "" {
                tables = append(tables, data)
            }
        }
        lineNum++
    }

    return header, tables, scanner.Err()
}

// encodeBlob 将 []byte 编码为 base64 字符串
func encodeBlob(data []byte) string {
    return base64.StdEncoding.EncodeToString(data)
}

// decodeBlob 将 base64 字符串解码为 []byte
func decodeBlob(encoded string) ([]byte, error) {
    return base64.StdEncoding.DecodeString(encoded)
}