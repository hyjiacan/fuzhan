package migration

import (
    "time"
)

// ProgressCalculator 进度计算器
type ProgressCalculator struct {
    totalTables    int
    totalRecords   int64
    tablesDone     int
    recordsDone    int64
    startTime      time.Time
    stage          MigrationStage
    stageProgress  float64 // 当前阶段内的进度 0-100
}

// NewProgressCalculator 创建进度计算器
func NewProgressCalculator(totalTables int, totalRecords int64) *ProgressCalculator {
    return &ProgressCalculator{
        totalTables:   totalTables,
        totalRecords:  totalRecords,
        startTime:     time.Now(),
        stage:         StagePreparing,
        stageProgress: 0,
    }
}

// SetStage 设置当前阶段
func (p *ProgressCalculator) SetStage(stage MigrationStage) {
    p.stage = stage
    p.stageProgress = 0
}

// UpdateTableProgress 更新表进度
func (p *ProgressCalculator) UpdateTableProgress(tablesDone int) {
    p.tablesDone = tablesDone
}

// UpdateRecordProgress 更新记录进度
func (p *ProgressCalculator) UpdateRecordProgress(recordsDone int64) {
    p.recordsDone = recordsDone
}

// UpdateStageProgress 更新阶段内进度
func (p *ProgressCalculator) UpdateStageProgress(progress float64) {
    p.stageProgress = progress
    if p.stageProgress > 100 {
        p.stageProgress = 100
    }
}

// CalculateOverallProgress 计算整体进度 0-100
func (p *ProgressCalculator) CalculateOverallProgress() int {
    range_, ok := StageRanges[p.stage]
    if !ok {
        return 0
    }

    baseProgress := range_[0]
    stageSpan := range_[1] - range_[0]

    return baseProgress + int(float64(stageSpan)*p.stageProgress/100)
}

// GetMigrationProgress 获取完整的迁移进度
func (p *ProgressCalculator) GetMigrationProgress() *MigrationProgressEvent {
    return &MigrationProgressEvent{
        Stage:           p.stage,
        Progress:        p.CalculateOverallProgress(),
        Message:         GetStageMessage(p.stage),
        TablesCompleted: p.tablesDone,
        TablesTotal:     p.totalTables,
        RecordsImported: p.recordsDone,
        StartTime:       p.startTime,
        EstimatedRemain: p.EstimateRemainingTime(),
    }
}

// EstimateRemainingTime 预估剩余时间
func (p *ProgressCalculator) EstimateRemainingTime() string {
    if p.stage == StageComplete {
        return "0s"
    }

    elapsed := time.Since(p.startTime)
    if elapsed < time.Second {
        return "计算中..."
    }

    overall := p.CalculateOverallProgress()
    if overall <= 0 {
        return "未知"
    }

    // 计算预计总时间
    totalEstimate := elapsed * 100 / time.Duration(overall)
    remaining := totalEstimate - elapsed

    if remaining < time.Second {
        return "< 1s"
    }
    if remaining < time.Minute {
        return remaining.Round(time.Second).String()
    }
    if remaining < time.Hour {
        return (remaining / time.Minute * time.Minute).Round(time.Minute).String()
    }
    return (remaining / time.Hour * time.Hour).Round(time.Hour).String()
}

// MigrationProgressEvent 迁移进度事件
type MigrationProgressEvent struct {
    Stage           MigrationStage `json:"stage"`
    Progress        int            `json:"progress"`
    Message         string         `json:"message"`
    CurrentTable    string         `json:"currentTable,omitempty"`
    TablesCompleted int            `json:"tablesCompleted"`
    TablesTotal     int            `json:"tablesTotal"`
    RecordsExported int64          `json:"recordsExported,omitempty"`
    RecordsImported int64          `json:"recordsImported,omitempty"`
    StartTime       time.Time      `json:"startTime"`
    EstimatedRemain string         `json:"estimatedRemain"`
}

// ToMigrationEvent 转换为 SSE 事件
func (p *MigrationProgressEvent) ToMigrationEvent() *MigrationEvent {
    return &MigrationEvent{
        Stage:           string(p.Stage),
        Progress:        p.Progress,
        Message:         p.Message,
        Current:         p.CurrentTable,
        RecordsMigrated: p.RecordsImported,
        TablesCompleted: p.TablesCompleted,
        TablesTotal:     p.TablesTotal,
    }
}

// CalculateStageProgress 计算阶段进度
func CalculateStageProgress(stage MigrationStage, stageProgress float64) int {
    range_, ok := StageRanges[stage]
    if !ok {
        return 0
    }

    baseProgress := range_[0]
    stageSpan := range_[1] - range_[0]
    return baseProgress + int(float64(stageSpan)*stageProgress/100)
}

// CalculateDataImportProgress 计算数据导入进度（考虑表和记录）
func CalculateDataImportProgress(tablesDone, totalTables int, recordsDone, totalRecords int64) float64 {
    if totalTables == 0 {
        return 0
    }

    // 表进度权重 30%
    tableWeight := 0.3
    // 记录进度权重 70%
    recordWeight := 0.7

    tableProgress := float64(tablesDone) / float64(totalTables) * 100

    var recordProgress float64
    if totalRecords > 0 {
        recordProgress = float64(recordsDone) / float64(totalRecords) * 100
    }

    return tableProgress*tableWeight + recordProgress*recordWeight
}