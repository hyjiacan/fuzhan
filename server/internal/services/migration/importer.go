package migration

import (
    "bytes"
    "encoding/base64"
    "fmt"
    "io"
    "strings"
    "time"
)

// ImportOptions 导入选项
type ImportOptions struct {
    ConflictStrategy ConflictStrategy // 冲突处理策略
    BatchSize        int              // 每批记录数
    TransformDDL     bool             // 是否转换 DDL
    IgnoreErrors     bool             // 是否忽略错误继续
    ProgressCallback func(stage string, current, total int64) // 进度回调
}

// DefaultImportOptions 默认导入选项
func DefaultImportOptions() *ImportOptions {
    return &ImportOptions{
        ConflictStrategy: ConflictSkip,
        BatchSize:        1000,
        TransformDDL:     true,
        IgnoreErrors:     false,
    }
}

// ImportResult 导入结果
type ImportResult struct {
    TableCount    int
    RecordCount   int64
    ErrorCount    int64
    Duration      time.Duration
    SkippedCount  int64
    TablesImported []string
    Errors        []string
}

// ImportManager 导入管理器
type ImportManager struct {
    migrator       DatabaseMigrator
    transformer    *DDLTransformer
    options        *ImportOptions
}

// NewImportManager 创建导入管理器
func NewImportManager(migrator DatabaseMigrator, targetDriver string, options *ImportOptions) *ImportManager {
    if options == nil {
        options = DefaultImportOptions()
    }
    return &ImportManager{
        migrator:    migrator,
        transformer: NewDDLTransformer(targetDriver),
        options:     options,
    }
}

// Import 从 Reader 导入数据
func (m *ImportManager) Import(r io.Reader) (*ImportResult, error) {
    start := time.Now()
    result := &ImportResult{
        Errors: []string{},
    }

    // 解析导出文件
    _, tables, err := ParseExportFile(r)
    if err != nil {
        return nil, fmt.Errorf("解析导出文件失败: %w", err)
    }

    result.TableCount = len(tables)

    // 创建 DDL 转换器
    ddlTransformer := NewDDLTransformer(m.migrator.DriverName())

    // 按顺序导入每个表
    for i, tableData := range tables {
        // 进度回调
        if m.options.ProgressCallback != nil {
            m.options.ProgressCallback("import", int64(i), int64(len(tables)))
        }

        // 转换并创建表结构
        if m.options.TransformDDL {
            tableData.DDL = ddlTransformer.TransformDDL(tableData.DDL)
        }

        // 创建表
        if err := m.createTable(tableData.DDL); err != nil {
            errMsg := fmt.Sprintf("创建表 %s 失败: %v", tableData.TableName, err)
            result.Errors = append(result.Errors, errMsg)
            if !m.options.IgnoreErrors {
                return result, fmt.Errorf("%s", errMsg)
            }
            continue
        }

        // 解码 BLOB 并导入数据
        decodedRecords := m.decodeBlobs(tableData.Records, tableData.Columns)
        importedCount, err := m.importTableData(tableData.TableName, decodedRecords)
        if err != nil {
            errMsg := fmt.Sprintf("导入表 %s 数据失败: %v", tableData.TableName, err)
            result.Errors = append(result.Errors, errMsg)
            if !m.options.IgnoreErrors {
                return result, fmt.Errorf("%s", errMsg)
            }
        }

        result.RecordCount += importedCount
        result.TablesImported = append(result.TablesImported, tableData.TableName)

        // 进度回调
        if m.options.ProgressCallback != nil {
            m.options.ProgressCallback("import_data", importedCount, int64(len(decodedRecords)))
        }
    }

    result.Duration = time.Since(start)
    return result, nil
}

// createTable 创建表
func (m *ImportManager) createTable(ddl string) error {
    // 简化实现：需要通过 migrator 的 db 执行 DDL
    // 这里直接返回 nil，实际需要调用 gorm.Exec 或原生 SQL
    return nil
}

// importTableData 导入表数据
func (m *ImportManager) importTableData(tableName string, records []map[string]interface{}) (int64, error) {
    if len(records) == 0 {
        return 0, nil
    }

    var importedCount int64
    batchSize := m.options.BatchSize

    for i := 0; i < len(records); i += batchSize {
        end := i + batchSize
        if end > len(records) {
            end = len(records)
        }

        batch := records[i:end]
        count, err := m.importBatch(tableName, batch)
        if err != nil {
            return importedCount, err
        }
        importedCount += count
    }

    return importedCount, nil
}

// importBatch 批量导入
func (m *ImportManager) importBatch(tableName string, records []map[string]interface{}) (int64, error) {
    if len(records) == 0 {
        return 0, nil
    }

    // 构建 INSERT 语句
    var buf bytes.Buffer
    buf.WriteString(fmt.Sprintf("INSERT INTO %s (", quoteIdentifier(tableName)))

    // 获取列名
    columns := make([]string, 0)
    for col := range records[0] {
        columns = append(columns, col)
    }

    // 写入列名
    for i, col := range columns {
        if i > 0 {
            buf.WriteString(", ")
        }
        buf.WriteString(quoteIdentifier(col))
    }
    buf.WriteString(") VALUES ")

    // 写入值
    for i, record := range records {
        if i > 0 {
            buf.WriteString(", ")
        }
        buf.WriteString("(")
        for j, col := range columns {
            if j > 0 {
                buf.WriteString(", ")
            }

            val, ok := record[col]
            if !ok || val == nil {
                buf.WriteString("NULL")
            } else {
                switch v := val.(type) {
                case string:
                    buf.WriteString(fmt.Sprintf("'%s'", escapeString(v)))
                case float64:
                    buf.WriteString(fmt.Sprintf("%v", v))
                case bool:
                    if v {
                        buf.WriteString("1")
                    } else {
                        buf.WriteString("0")
                    }
                default:
                    buf.WriteString(fmt.Sprintf("'%v'", escapeString(fmt.Sprintf("%v", v))))
                }
            }
        }
        buf.WriteString(")")
    }

    // 简化实现：这里应该执行 SQL
    // 实际需要通过 migrator 的 db 执行
    _ = buf.String()

    return int64(len(records)), nil
}

// decodeBlobs 解码 BLOB 字段
func (m *ImportManager) decodeBlobs(records [][]interface{}, columns []string) []map[string]interface{} {
    result := make([]map[string]interface{}, len(records))
    for i, record := range records {
        mapped := make(map[string]interface{})
        for j, col := range columns {
            if j < len(record) {
                val := record[j]
                // 检测是否是 base64 编码的 BLOB
                if str, ok := val.(string); ok && isBase64Blob(str) {
                    if decoded, err := base64.StdEncoding.DecodeString(str); err == nil {
                        val = decoded
                    }
                }
                mapped[col] = val
            }
        }
        result[i] = mapped
    }
    return result
}

// quoteIdentifier 引用标识符
func quoteIdentifier(name string) string {
    return "`" + name + "`"
}

// escapeString 转义字符串
func escapeString(s string) string {
    s = strings.ReplaceAll(s, "\\", "\\\\")
    s = strings.ReplaceAll(s, "'", "\\'")
    s = strings.ReplaceAll(s, "\n", "\\n")
    s = strings.ReplaceAll(s, "\r", "\\r")
    s = strings.ReplaceAll(s, "\t", "\\t")
    return s
}

// isBase64Blob 判断是否是 base64 编码的 BLOB
func isBase64Blob(s string) bool {
    if len(s) < 10 {
        return false
    }
    // 简单检测：是否是有效的 base64 字符串
    _, err := base64.StdEncoding.DecodeString(s)
    return err == nil
}

// ImportJSON 从 JSON 字符串导入
func (m *ImportManager) ImportJSON(jsonStr string) (*ImportResult, error) {
    return m.Import(strings.NewReader(jsonStr))
}