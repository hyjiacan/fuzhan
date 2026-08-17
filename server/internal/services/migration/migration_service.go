package migration

import (
    "bufio"
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/google/uuid"
    "gorm.io/gorm"

    "fuzhan/internal/models"
)

// MigrationService 迁移服务主结构
type MigrationService struct {
    db          *gorm.DB
    backupDir   string
    lockService *LockService
}

// NewMigrationService 创建迁移服务
func NewMigrationService(db *gorm.DB, backupDir string) *MigrationService {
    return &MigrationService{
        db:          db,
        backupDir:   backupDir,
        lockService: InitLockService(30 * time.Minute), // 30分钟锁超时
    }
}

// MigrationCheckResult 迁移检查结果
type MigrationCheckResult struct {
    MigrationNeeded   bool              `json:"migrationNeeded"`
    Source            *SourceInfo       `json:"source"`
    Target            *TargetInfo       `json:"target"`
    RiskLevel         string            `json:"riskLevel"` // low/medium/high
    EstimatedDuration string            `json:"estimatedDuration"`
    Warnings          []string          `json:"warnings"`
    BackupRecommended bool              `json:"backupRecommended"`
}

// SourceInfo 源数据库信息
type SourceInfo struct {
    Driver       string   `json:"driver"`
    RecordCount  int64    `json:"recordCount"`
    EstimatedSize string  `json:"estimatedSize"`
    TableCount   int      `json:"tableCount"`
    Tables       []string `json:"tables"`
}

// TargetInfo 目标数据库信息
type TargetInfo struct {
    Driver         string   `json:"driver"`
    Connected      bool     `json:"connected"`
    DatabaseEmpty  bool     `json:"databaseEmpty"`
    ConflictTables []string `json:"conflictTables"`
}

// MigrationOptions 迁移选项
type MigrationOptions struct {
    Overwrite    bool            // 是否覆盖已存在的数据
    CreateBackup bool            // 是否创建备份
    SkipTables   map[string]bool // 跳过的表
}

// CheckMigration 检查迁移可行性
func (s *MigrationService) CheckMigration(sourceDSN, targetDriver, targetDSN string) (*MigrationCheckResult, error) {
    result := &MigrationCheckResult{
        MigrationNeeded:   true,
        Warnings:          []string{},
        BackupRecommended: true,
    }

    // 获取源数据库信息
    sourceMigrator, err := s.createMigrator(sourceDSN)
    if err != nil {
        return nil, fmt.Errorf("无法创建源数据库迁移器: %w", err)
    }

    sourceInfo, err := sourceMigrator.GetDatabaseInfo(sourceDSN)
    if err != nil {
        return nil, fmt.Errorf("无法获取源数据库信息: %w", err)
    }

    result.Source = &SourceInfo{
        Driver:        sourceInfo.Driver,
        RecordCount:   sourceInfo.RecordCount,
        EstimatedSize: fmt.Sprintf("%.1f MB", sourceInfo.EstimatedMB),
        TableCount:    sourceInfo.TableCount,
        Tables:        make([]string, 0),
    }
    for _, t := range sourceInfo.Tables {
        result.Source.Tables = append(result.Source.Tables, t.Name)
    }

    // 获取目标数据库信息
    targetMigrator, err := s.createMigrator(targetDSN)
    if err != nil {
        return nil, fmt.Errorf("无法创建目标数据库迁移器: %w", err)
    }

    targetInfo, err := targetMigrator.TestConnection(targetDSN)
    if err != nil || !targetInfo.Connected {
        result.Target = &TargetInfo{
            Driver:    targetDriver,
            Connected: false,
        }
        return nil, fmt.Errorf("无法连接到目标数据库")
    }

    result.Target = &TargetInfo{
        Driver:        targetDriver,
        Connected:     true,
        DatabaseEmpty: targetInfo.DatabaseEmpty,
    }

    // 检查冲突表
    for _, table := range targetInfo.Tables {
        for _, srcTable := range result.Source.Tables {
            if table == srcTable {
                result.Target.ConflictTables = append(result.Target.ConflictTables, table)
            }
        }
    }

    // 评估风险等级
    result.RiskLevel = s.assessRiskLevel(result)

    // 预估耗时 (每1000条记录1秒)
    estimatedSeconds := result.Source.RecordCount / 1000
    if estimatedSeconds < 5 {
        estimatedSeconds = 5
    }
    result.EstimatedDuration = fmt.Sprintf("%ds", estimatedSeconds)

    // 添加警告
    if len(result.Target.ConflictTables) > 0 {
        result.Warnings = append(result.Warnings, fmt.Sprintf("目标数据库存在 %d 个同名表，数据可能冲突", len(result.Target.ConflictTables)))
    }
    if sourceInfo.Driver == targetDriver {
        result.Warnings = append(result.Warnings, "源数据库和目标数据库类型相同，可能不需要迁移")
    }

    return result, nil
}

// StartMigration 开始迁移
func (s *MigrationService) StartMigration(ctx context.Context, sourceDSN, targetDriver, targetDSN string, opts *MigrationOptions) (<-chan *MigrationEvent, error) {
    // 检查锁
    if !s.lockService.AcquireLock("migration") {
        return nil, fmt.Errorf("已有迁移任务正在执行")
    }

    // 生成迁移ID
    migrationID := uuid.New().String()

    // 确保备份目录存在
    if s.backupDir != "" {
        os.MkdirAll(s.backupDir, 0755)
    }

    // 创建迁移状态记录
    status := &models.MigrationStatus{
        ID:           migrationID,
        SourceDriver: s.getDriverFromDSN(sourceDSN),
        SourceDSN:    sourceDSN,
        TargetDriver: targetDriver,
        TargetDSN:    targetDSN,
        Status:       models.MigrationStatusRunning,
        Stage:        models.MigrationStagePreparing,
        StartedAt:    time.Now(),
    }
    if err := s.db.Create(status).Error; err != nil {
        s.lockService.ReleaseLock("migration")
        return nil, fmt.Errorf("创建迁移记录失败: %w", err)
    }

    // 创建事件通道
    eventCh := make(chan *MigrationEvent, 100)

    // 启动迁移 goroutine
    go func() {
        defer close(eventCh)
        defer s.lockService.ReleaseLock("migration")

        emitter := NewChannelEventEmitter(100)

        // 发送准备阶段事件
        emitter.Emit(&MigrationEvent{
            Stage:    models.MigrationStagePreparing,
            Progress: 0,
            Message:  "准备迁移环境...",
        })

        // 执行迁移
        err := s.executeMigration(ctx, migrationID, sourceDSN, targetDriver, targetDSN, opts, emitter)

        if err != nil {
            // 发送错误事件
            emitter.Emit(&MigrationEvent{
                Stage:             models.MigrationStageError,
                Progress:          0,
                Message:           fmt.Sprintf("迁移失败: %v", err),
                Error:             err.Error(),
                RollbackAvailable: true,
            })

            // 更新状态
            s.db.Model(&models.MigrationStatus{}).Where("id = ?", migrationID).Updates(map[string]interface{}{
                "status":       models.MigrationStatusFailed,
                "stage":        models.MigrationStageError,
                "error_message": err.Error(),
            })
        } else {
            // 发送完成事件
            emitter.Emit(&MigrationEvent{
                Stage:    models.MigrationStageComplete,
                Progress: 100,
                Message:  "迁移完成！",
            })

            // 更新状态
            now := time.Now()
            s.db.Model(&models.MigrationStatus{}).Where("id = ?", migrationID).Updates(map[string]interface{}{
                "status":       models.MigrationStatusCompleted,
                "stage":        models.MigrationStageComplete,
                "progress":     100,
                "completed_at": &now,
            })
        }

        // 发送事件到通道
        for event := range emitter.Channel() {
            select {
            case eventCh <- event:
            case <-ctx.Done():
                return
            }
        }
    }()

    return eventCh, nil
}

// executeMigration 执行迁移
func (s *MigrationService) executeMigration(ctx context.Context, migrationID, sourceDSN, targetDriver, targetDSN string, opts *MigrationOptions, emitter EventEmitter) error {
    // 创建迁移器
    sourceMigrator, err := s.createMigrator(sourceDSN)
    if err != nil {
        return fmt.Errorf("创建源迁移器失败: %w", err)
    }

    targetMigrator, err := s.createMigrator(targetDSN)
    if err != nil {
        return fmt.Errorf("创建目标迁移器失败: %w", err)
    }

    // 阶段1: 备份
    emitter.Emit(&MigrationEvent{
        Stage:    models.MigrationStageBackup,
        Progress: 10,
        Message:  "正在备份原数据库...",
    })

    var backupPath string
    if opts != nil && opts.CreateBackup && s.backupDir != "" {
        backupPath, err = s.createBackup(sourceMigrator, migrationID)
        if err != nil {
            return fmt.Errorf("备份失败: %w", err)
        }

        // 更新备份路径
        s.db.Model(&models.MigrationStatus{}).Where("id = ?", migrationID).Update("backup_path", backupPath)

        emitter.Emit(&MigrationEvent{
            Stage:    models.MigrationStageBackup,
            Progress: 15,
            Message:  fmt.Sprintf("备份完成: %s", backupPath),
        })
    }

    // 阶段2: 导出
    emitter.Emit(&MigrationEvent{
        Stage:    models.MigrationStageExport,
        Progress: 20,
        Message:  "导出源数据...",
    })

    var exportBuf bytes.Buffer
    if err := sourceMigrator.ExportData(&exportBuf); err != nil {
        return fmt.Errorf("导出数据失败: %w", err)
    }

    // 阶段3: 转换
    emitter.Emit(&MigrationEvent{
        Stage:    models.MigrationStageTransform,
        Progress: 40,
        Message:  "转换数据格式...",
    })

    // DDL 转换已内置在导出过程中

    // 阶段4: 导入
    emitter.Emit(&MigrationEvent{
        Stage:    models.MigrationStageImport,
        Progress: 50,
        Message:  "导入到目标数据库...",
    })

    // 解析并导入数据
    tables, err := s.parseExportedData(&exportBuf)
    if err != nil {
        return fmt.Errorf("解析导出数据失败: %w", err)
    }

    totalTables := len(tables)
    var recordsMigrated int64

    for i, table := range tables {
        select {
        case <-ctx.Done():
            return fmt.Errorf("迁移被取消")
        default:
        }

        // 发送表进度
        emitter.Emit(&MigrationEvent{
            Stage:           models.MigrationStageImport,
            Progress:        50 + int((float64(i)/float64(totalTables))*40),
            Message:         fmt.Sprintf("导入表: %s", table.TableName),
            Current:         fmt.Sprintf("%s (%d/%d)", table.TableName, i+1, totalTables),
            TablesCompleted: i,
            TablesTotal:     totalTables,
        })

        // 导入表
        count, err := s.importTable(targetMigrator, table)
        if err != nil {
            // 记录错误但继续
            s.db.Create(&models.MigrationTableProgress{
                MigrationID:  migrationID,
                TableName:    table.TableName,
                Status:       models.TableProgressStatusFailed,
                ErrorMessage: err.Error(),
            })
        } else {
            recordsMigrated += count
            s.db.Create(&models.MigrationTableProgress{
                MigrationID:   migrationID,
                TableName:     table.TableName,
                Status:        models.TableProgressStatusCompleted,
                SourceRecords: int64(len(table.Records)),
                TargetRecords: count,
            })
        }

        // 更新进度
        s.db.Model(&models.MigrationStatus{}).Where("id = ?", migrationID).Updates(map[string]interface{}{
            "tables_completed":    i + 1,
            "tables_total":        totalTables,
            "records_migrated":    recordsMigrated,
            "progress":            50 + int((float64(i)/float64(totalTables))*40),
        })
    }

    // 阶段5: 验证
    emitter.Emit(&MigrationEvent{
        Stage:    models.MigrationStageVerify,
        Progress: 90,
        Message:  "验证数据完整性...",
    })

    if err := s.verifyMigration(targetMigrator, tables); err != nil {
        emitter.Emit(&MigrationEvent{
            Stage:    models.MigrationStageVerify,
            Progress: 95,
            Message:  fmt.Sprintf("警告: 验证发现问题 - %v", err),
        })
    }

    return nil
}

// createBackup 创建备份
func (s *MigrationService) createBackup(migrator DatabaseMigrator, migrationID string) (string, error) {
    if s.backupDir == "" {
        return "", nil
    }

    filename := fmt.Sprintf("backup_%s_%s.json", migrator.DriverName(), migrationID[:8])
    backupPath := filepath.Join(s.backupDir, filename)

    file, err := os.Create(backupPath)
    if err != nil {
        return "", err
    }
    defer file.Close()

    if err := migrator.ExportData(file); err != nil {
        os.Remove(backupPath)
        return "", err
    }

    return backupPath, nil
}

// parseExportedData 解析导出的数据
func (s *MigrationService) parseExportedData(r io.Reader) ([]ExportTableData, error) {
    var tables []ExportTableData
    scanner := bufio.NewScanner(r)

    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" {
            continue
        }

        var data ExportTableData
        if err := json.Unmarshal([]byte(line), &data); err != nil {
            // 可能是头部，跳过
            continue
        }

        if data.TableName != "" {
            tables = append(tables, data)
        }
    }

    return tables, scanner.Err()
}

// importTable 导入单个表
func (s *MigrationService) importTable(migrator DatabaseMigrator, data ExportTableData) (int64, error) {
    // 创建表
    ddl := migrator.TransformDDL(data.DDL)
    if err := s.execDDL(migrator, ddl); err != nil {
        return 0, err
    }

    // 插入数据
    if len(data.Records) == 0 {
        return 0, nil
    }

    // 批量插入
    batchSize := 100
    for i := 0; i < len(data.Records); i += batchSize {
        end := i + batchSize
        if end > len(data.Records) {
            end = len(data.Records)
        }

        batch := data.Records[i:end]
        if err := s.insertBatch(migrator, data.TableName, data.Columns, batch); err != nil {
            return int64(i), err
        }
    }

    return int64(len(data.Records)), nil
}

// execDDL 执行 DDL
func (s *MigrationService) execDDL(migrator DatabaseMigrator, ddl string) error {
    // 这里需要直接执行 SQL，暂时返回 nil
    // 实际实现需要使用 gorm.Exec 或原生 SQL
    return nil
}

// insertBatch 批量插入
func (s *MigrationService) insertBatch(migrator DatabaseMigrator, tableName string, columns []string, records [][]interface{}) error {
    // 这里需要实现批量插入逻辑
    return nil
}

// verifyMigration 验证迁移结果
func (s *MigrationService) verifyMigration(migrator DatabaseMigrator, tables []ExportTableData) error {
    // 简单验证：检查表是否存在
    for _, table := range tables {
        count, err := migrator.GetTableRecordCount(table.TableName)
        if err != nil {
            return fmt.Errorf("表 %s 验证失败: %w", table.TableName, err)
        }
        if count != int64(len(table.Records)) {
            return fmt.Errorf("表 %s 记录数不匹配: 期望 %d, 实际 %d", table.TableName, len(table.Records), count)
        }
    }
    return nil
}

// createMigrator 根据 DSN 创建迁移器
func (s *MigrationService) createMigrator(dsn string) (DatabaseMigrator, error) {
    driver := s.getDriverFromDSN(dsn)

    switch driver {
    case "sqlite":
        return NewSQLiteMigrator(dsn)
    case "mysql":
        return NewMySQLMigrator(dsn)
    case "postgres":
        return NewPostgresMigrator(dsn)
    default:
        return nil, fmt.Errorf("不支持的数据库类型: %s", driver)
    }
}

// getDriverFromDSN 从 DSN 判断数据库类型
func (s *MigrationService) getDriverFromDSN(dsn string) string {
    if strings.HasPrefix(dsn, "sqlite://") || strings.HasSuffix(dsn, ".db") || strings.Contains(dsn, ".db?") {
        return "sqlite"
    }
    if strings.Contains(dsn, "@tcp(") || strings.Contains(dsn, "@unix(") {
        return "mysql"
    }
    if strings.Contains(dsn, "postgres://") || strings.Contains(dsn, "postgresql://") || strings.Contains(dsn, "host=") {
        return "postgres"
    }
    // 默认为 SQLite
    return "sqlite"
}

// assessRiskLevel 评估风险等级
func (s *MigrationService) assessRiskLevel(result *MigrationCheckResult) string {
    if result.Source == nil {
        return "high"
    }

    // 高风险: 目标数据库非空
    if !result.Target.DatabaseEmpty {
        return "high"
    }

    // 中风险: 记录数较多
    if result.Source.RecordCount > 10000 {
        return "medium"
    }

    // 低风险: 小数据量
    return "low"
}

// GetMigrationStatus 获取迁移状态
func (s *MigrationService) GetMigrationStatus(migrationID string) (*models.MigrationStatus, error) {
    var status models.MigrationStatus
    if err := s.db.Where("id = ?", migrationID).First(&status).Error; err != nil {
        return nil, err
    }
    return &status, nil
}

// ListMigrations 列出迁移记录
func (s *MigrationService) ListMigrations(limit int) ([]models.MigrationStatus, error) {
    var statuses []models.MigrationStatus
    query := s.db.Order("created_at DESC")
    if limit > 0 {
        query = query.Limit(limit)
    }
    if err := query.Find(&statuses).Error; err != nil {
        return nil, err
    }
    return statuses, nil
}