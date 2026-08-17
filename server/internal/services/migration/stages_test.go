package migration

import (
    "testing"
)

// TestStages_Order tests migration stage ordering
func TestStages_Order(t *testing.T) {
    tests := []struct {
        stage    MigrationStage
        expected int
    }{
        {StagePreparing, 0},
        {StageBackup, 1},
        {StageSchemaExport, 2},
        {StageSchemaImport, 3},
        {StageDataExport, 4},
        {StageDataImport, 5},
        {StageForeignKeys, 6},
        {StageVerify, 7},
        {StageComplete, 8},
    }

    for _, tt := range tests {
        t.Run(string(tt.stage), func(t *testing.T) {
            order := GetStageOrder(tt.stage)
            if order != tt.expected {
                t.Errorf("GetStageOrder(%s) = %d, want %d", tt.stage, order, tt.expected)
            }
        })
    }
}

// TestStages_Ranges tests stage progress ranges
func TestStages_Ranges(t *testing.T) {
    tests := []struct {
        stage        MigrationStage
        minProgress  int
        maxProgress  int
    }{
        {StagePreparing, 0, 5},
        {StageBackup, 5, 10},
        {StageSchemaExport, 10, 15},
        {StageSchemaImport, 15, 20},
        {StageDataExport, 20, 50},
        {StageDataImport, 50, 85},
        {StageForeignKeys, 85, 90},
        {StageVerify, 90, 95},
        {StageComplete, 100, 100},
    }

    for _, tt := range tests {
        t.Run(string(tt.stage), func(t *testing.T) {
            range_, ok := StageRanges[tt.stage]
            if !ok {
                t.Errorf("阶段 %s 不在 StageRanges 中", tt.stage)
                return
            }

            if range_[0] != tt.minProgress || range_[1] != tt.maxProgress {
                t.Errorf("阶段 %s 范围为 [%d, %d]，预期 [%d, %d]",
                    tt.stage, range_[0], range_[1], tt.minProgress, tt.maxProgress)
            }
        })
    }
}

// TestGetStageMessage tests stage message retrieval
func TestGetStageMessage(t *testing.T) {
    tests := []struct {
        stage    MigrationStage
        expected string
    }{
        {StagePreparing, "准备迁移环境..."},
        {StageBackup, "正在备份原数据库..."},
        {StageSchemaExport, "导出表结构..."},
        {StageSchemaImport, "创建目标表结构..."},
        {StageDataExport, "导出数据..."},
        {StageDataImport, "导入数据..."},
        {StageForeignKeys, "重建外键约束..."},
        {StageVerify, "验证数据完整性..."},
        {StageComplete, "迁移完成！"},
        {StageError, "迁移出错"},
        {"unknown", "unknown"}, // Unknown stage returns as-is
    }

    for _, tt := range tests {
        t.Run(string(tt.stage), func(t *testing.T) {
            msg := GetStageMessage(tt.stage)
            if msg != tt.expected {
                t.Errorf("GetStageMessage(%s) = %q, want %q", tt.stage, msg, tt.expected)
            }
        })
    }
}

// TestGetNextStage tests getting next stage
func TestGetNextStage(t *testing.T) {
    tests := []struct {
        current MigrationStage
        next    MigrationStage
    }{
        {StagePreparing, StageBackup},
        {StageBackup, StageSchemaExport},
        {StageSchemaExport, StageSchemaImport},
        {StageSchemaImport, StageDataExport},
        {StageDataExport, StageDataImport},
        {StageDataImport, StageForeignKeys},
        {StageForeignKeys, StageVerify},
        {StageVerify, StageComplete},
        {StageComplete, StageComplete}, // Last stage stays the same
        {StageError, StageError},       // Error stage stays the same
    }

    for _, tt := range tests {
        t.Run(string(tt.current), func(t *testing.T) {
            next := GetNextStage(tt.current)
            if next != tt.next {
                t.Errorf("GetNextStage(%s) = %s, want %s", tt.current, next, tt.next)
            }
        })
    }
}

// TestIsValidStage tests stage validation
func TestIsValidStage(t *testing.T) {
    // Note: StageError is not in StageRanges, so it's considered invalid
    validStages := []MigrationStage{
        StagePreparing, StageBackup, StageSchemaExport, StageSchemaImport,
        StageDataExport, StageDataImport, StageForeignKeys, StageVerify,
        StageComplete,
    }

    for _, stage := range validStages {
        t.Run(string(stage), func(t *testing.T) {
            if !IsValidStage(stage) {
                t.Errorf("IsValidStage(%s) 应返回 true", stage)
            }
        })
    }

    invalidStages := []MigrationStage{
        StageError, // StageError is not in StageRanges
        "invalid",
        "unknown",
        "",
        "PREPARING", // Wrong case
    }

    for _, stage := range invalidStages {
        t.Run(string(stage), func(t *testing.T) {
            if IsValidStage(stage) {
                t.Errorf("IsValidStage(%s) 应返回 false", stage)
            }
        })
    }
}