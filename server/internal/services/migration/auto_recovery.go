package migration

import (
    "fmt"
    "time"

    "fuzhan/internal/utils"
)

// AutoRecoveryService 自动恢复服务
type AutoRecoveryService struct {
    recoveryService *RecoveryService
    rollbackService *RollbackService
    autoRollback    bool // 是否自动回滚
}

// NewAutoRecoveryService 创建自动恢复服务
func NewAutoRecoveryService(recoveryService *RecoveryService, rollbackService *RollbackService) *AutoRecoveryService {
    return &AutoRecoveryService{
        recoveryService: recoveryService,
        rollbackService: rollbackService,
        autoRollback:    true, // 默认启用自动回滚
    }
}

// SetAutoRollback 设置是否自动回滚
func (s *AutoRecoveryService) SetAutoRollback(enabled bool) {
    s.autoRollback = enabled
}

// HandleMigrationError 处理迁移错误
func (s *AutoRecoveryService) HandleMigrationError(migrationID string, err error, stage MigrationStage) *MigrationError {
    migrationErr := classifyError(err)
    migrationErr.Stage = string(stage)

    // 检查是否可以回滚
    info, rollbackErr := s.rollbackService.GetRollbackInfo(migrationID)
    if rollbackErr == nil && info.CanRollback {
        migrationErr.RollbackAvailable = true
    }

    // 如果启用了自动回滚且可以回滚，则自动执行
    if s.autoRollback && migrationErr.RollbackAvailable {
        go s.autoRollbackMigration(migrationID)
    }

    return migrationErr
}

// autoRollbackMigration 自动回滚
func (s *AutoRecoveryService) autoRollbackMigration(migrationID string) {
    // 等待一小段时间让用户看到错误
    time.Sleep(5 * time.Second)

    result, err := s.rollbackService.Rollback(migrationID)
    if err != nil {
        // 回滚失败，记录日志但不中断
        utils.Error("自动回滚失败", utils.Err(err))
    } else {
        utils.Info("自动回滚成功", utils.Any("result", result.Message))
    }
}

// RecoveryAction 恢复操作选项
type RecoveryAction string

const (
    RecoveryActionResume   RecoveryAction = "resume"   // 继续迁移
    RecoveryActionRestart  RecoveryAction = "restart"  // 重新迁移
    RecoveryActionRollback RecoveryAction = "rollback" // 回滚
    RecoveryActionCancel   RecoveryAction = "cancel"   // 取消
)

// RecoveryDecision 恢复决策
type RecoveryDecision struct {
    Action RecoveryAction
    Error  error
}

// MakeRecoveryDecision 执行恢复决策
func (s *AutoRecoveryService) MakeRecoveryDecision(interrupted *InterruptedMigration, action RecoveryAction) (*RecoveryDecision, error) {
    switch action {
    case RecoveryActionResume:
        return s.handleResume(interrupted)
    case RecoveryActionRestart:
        return s.handleRestart(interrupted)
    case RecoveryActionRollback:
        return s.handleRollback(interrupted)
    case RecoveryActionCancel:
        return &RecoveryDecision{Action: action}, nil
    default:
        return nil, fmt.Errorf("未知的恢复操作: %s", action)
    }
}

// handleResume 处理继续迁移
func (s *AutoRecoveryService) handleResume(interrupted *InterruptedMigration) (*RecoveryDecision, error) {
    // 获取当前配置
    targetDriver := ""
    targetDSN := ""

    // 继续迁移
    _, err := s.recoveryService.ResumeMigration(nil, interrupted.MigrationID, targetDriver, targetDSN)
    if err != nil {
        return nil, err
    }

    return &RecoveryDecision{Action: RecoveryActionResume}, nil
}

// handleRestart 处理重新迁移
func (s *AutoRecoveryService) handleRestart(interrupted *InterruptedMigration) (*RecoveryDecision, error) {
    // 获取当前配置
    targetDriver := ""
    targetDSN := ""

    // 重新迁移
    _, err := s.recoveryService.RestartMigration(nil, interrupted.MigrationID, targetDriver, targetDSN)
    if err != nil {
        return nil, err
    }

    return &RecoveryDecision{Action: RecoveryActionRestart}, nil
}

// handleRollback 处理回滚
func (s *AutoRecoveryService) handleRollback(interrupted *InterruptedMigration) (*RecoveryDecision, error) {
    result, err := s.rollbackService.Rollback(interrupted.MigrationID)
    if err != nil {
        return nil, err
    }

    return &RecoveryDecision{
        Action: RecoveryActionRollback,
        Error:  fmt.Errorf("%s", result.Message),
    }, nil
}