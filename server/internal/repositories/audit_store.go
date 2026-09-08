package repositories

import "fuzhan/internal/models"

// AuditStore 审计日志存储接口：承载 OperationRecord（上传/下载/搜索等操作记录）
// 的持久化读写。
//
// 默认实现为 *RecordRepository（走主库 GORM）。定义本接口是为了将"审计日志
// 存储"与"业务存储"解耦：将来如需把审计日志拆分到独立的库/存储（例如避开
// 与 file_records 等热表争抢主库写锁，实现冷热分离），只需编写一个独立的
// AuditStore 实现，并在构建期替换注入即可，上层 handler 无需任何改动。
type AuditStore interface {
	Create(record *models.OperationRecord) error
	GetByID(id uint) (*models.OperationRecord, error)
	ListByUserID(userID string, page, pageSize int) ([]models.OperationRecord, int64, error)
	ListByIP(clientIP string, page, pageSize int) ([]models.OperationRecord, int64, error)
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
