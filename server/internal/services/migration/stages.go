package migration

// MigrationStage 迁移阶段
type MigrationStage string

const (
    StagePreparing    MigrationStage = "preparing"     // 0-5% 准备迁移环境
    StageBackup       MigrationStage = "backup"        // 5-10% 备份原数据库
    StageSchemaExport MigrationStage = "schema_export" // 10-15% 导出表结构
    StageSchemaImport MigrationStage = "schema_import" // 15-20% 创建目标表
    StageDataExport   MigrationStage = "data_export"   // 20-50% 导出数据
    StageDataImport   MigrationStage = "data_import"   // 50-85% 导入数据
    StageForeignKeys  MigrationStage = "foreign_keys"  // 85-90% 重建外键
    StageVerify       MigrationStage = "verify"        // 90-95% 验证完整性
    StageComplete     MigrationStage = "complete"      // 100% 迁移完成
    StageError        MigrationStage = "error"         // 错误
)

// StageRange 阶段进度范围
type StageRange [2]int

// 阶段进度范围映射
var StageRanges = map[MigrationStage]StageRange{
    StagePreparing:    {0, 5},
    StageBackup:       {5, 10},
    StageSchemaExport: {10, 15},
    StageSchemaImport: {15, 20},
    StageDataExport:   {20, 50},
    StageDataImport:   {50, 85},
    StageForeignKeys:  {85, 90},
    StageVerify:       {90, 95},
    StageComplete:     {100, 100},
}

// GetStageMessage 获取阶段描述
func GetStageMessage(stage MigrationStage) string {
    messages := map[MigrationStage]string{
        StagePreparing:    "准备迁移环境...",
        StageBackup:       "正在备份原数据库...",
        StageSchemaExport: "导出表结构...",
        StageSchemaImport: "创建目标表结构...",
        StageDataExport:   "导出数据...",
        StageDataImport:   "导入数据...",
        StageForeignKeys:  "重建外键约束...",
        StageVerify:       "验证数据完整性...",
        StageComplete:     "迁移完成！",
        StageError:        "迁移出错",
    }
    if msg, ok := messages[stage]; ok {
        return msg
    }
    return string(stage)
}

// GetNextStage 获取下一阶段
func GetNextStage(stage MigrationStage) MigrationStage {
    order := []MigrationStage{
        StagePreparing,
        StageBackup,
        StageSchemaExport,
        StageSchemaImport,
        StageDataExport,
        StageDataImport,
        StageForeignKeys,
        StageVerify,
        StageComplete,
    }

    for i, s := range order {
        if s == stage && i < len(order)-1 {
            return order[i+1]
        }
    }
    return stage
}

// IsValidStage 验证阶段是否有效
func IsValidStage(stage MigrationStage) bool {
    _, ok := StageRanges[stage]
    return ok
}

// GetStageOrder 获取阶段的顺序
func GetStageOrder(stage MigrationStage) int {
    order := []MigrationStage{
        StagePreparing,
        StageBackup,
        StageSchemaExport,
        StageSchemaImport,
        StageDataExport,
        StageDataImport,
        StageForeignKeys,
        StageVerify,
        StageComplete,
    }

    for i, s := range order {
        if s == stage {
            return i
        }
    }
    return -1
}