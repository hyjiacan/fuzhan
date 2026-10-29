package repositories

import (
	"time"

	"fuzhan/internal/models"
)

// RecordFilter 个人操作记录查询过滤条件：字段为空/零值表示不限制
type RecordFilter struct {
	Action     string    // 操作类型：upload / download / download-by-hash / search / preview
	SourceType string    // 来源：public / private / temp（public 兼容历史空值）
	Status     string    // 结果：success / failed
	Keyword    string    // 关键词：匹配文件名、路径或搜索词
	StartTime  time.Time // 起始时间（含）
	EndTime    time.Time // 结束时间（含）
}

// AuditStore 审计日志存储接口：承载 OperationRecord（上传/下载/搜索等操作记录）
// 的持久化读写。
//
// 默认实现为 *RecordRepository（走主库 GORM）。定义本接口是为了将"审计日志
// 存储"与"业务存储"解耦：将来如需把审计日志拆分到独立的库/存储（例如避开
// 与 file_records 等热表争抢主库写锁，实现冷热分离），只需编写一个独立的
// AuditStore 实现，并在构建期替换注入即可，上层 handler 无需任何改动。
type AuditStore interface {
	Create(record *models.OperationRecord) error
	// CreateBatch 批量创建操作记录（用于有界记录器攒批提交，降低逐条 insert 的写放大）
	CreateBatch(records []*models.OperationRecord) error
	GetByID(id uint) (*models.OperationRecord, error)
	ListByUserID(userID string, page, pageSize int) ([]models.OperationRecord, int64, error)
	ListByIP(clientIP string, page, pageSize int) ([]models.OperationRecord, int64, error)
	// ListByUserIDOrIP 获取用户ID或IP相关的操作记录（个人操作记录，支持过滤条件）
	ListByUserIDOrIP(userID, clientIP string, filter RecordFilter, page, pageSize int) ([]models.OperationRecord, int64, error)
	ListRecent(clientIP string, limit int) ([]models.OperationRecord, error)
	ListRecentAll(limit int, action string) ([]models.OperationRecord, error)
	ListRecentAllPaginated(page, pageSize int, action string) ([]models.OperationRecord, int64, error)
	// ResolvePublicFileID 按路径反查公共文件索引记录 ID（文件身份标识）
	ResolvePublicFileID(rootName, fullPath string) (uint, error)
	// UpdateFileRecordID 更新操作记录关联的文件索引记录 ID
	UpdateFileRecordID(recordID, fileRecordID uint) error
	// AttachCurrentPaths 将操作记录路径替换为对应文件的当前路径（跟随移动/重命名）
	AttachCurrentPaths(records []models.OperationRecord) []models.OperationRecord
	Delete(id uint) error
	DeleteByIDAndAction(id uint, action string) (int64, error)
	DeleteByFile(rootName, relPath string, isDir bool, recordIDs []uint) (int64, error)
	GetAll() ([]models.OperationRecord, error)
	GetRecent(limit int) ([]models.OperationRecord, error)
	GetRecentSearches(limit int) ([]models.OperationRecord, error)
	DeleteByAction(action string) (int64, error)
}

// 编译期断言：*RecordRepository 作为 AuditStore 的默认 GORM 实现。
var _ AuditStore = (*RecordRepository)(nil)
