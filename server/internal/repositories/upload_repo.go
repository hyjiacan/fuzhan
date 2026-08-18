package repositories

import (
    "time"

    "gorm.io/gorm"
    "fuzhan/internal/models"
)

// SessionRepository 会话仓库
type SessionRepository struct {
    db *gorm.DB
}

// NewSessionRepository 创建会话仓库实例
func NewSessionRepository(db *gorm.DB) *SessionRepository {
    return &SessionRepository{db: db}
}

// Create 创建上传会话
func (r *SessionRepository) Create(session *models.UploadSession) error {
    return r.db.Create(session).Error
}

// GetByID 根据ID获取会话
func (r *SessionRepository) GetByID(id uint) (*models.UploadSession, error) {
    var session models.UploadSession
    err := r.db.First(&session, id).Error
    if err != nil {
        return nil, err
    }
    return &session, nil
}

// Update 更新会话
func (r *SessionRepository) Update(session *models.UploadSession) error {
    return r.db.Save(session).Error
}

// UpdateStatus 更新会话状态
func (r *SessionRepository) UpdateStatus(id uint, status models.UploadStatus) error {
    return r.db.Model(&models.UploadSession{}).Where("id = ?", id).Update("status", status).Error
}

// RefreshExpiredAt 刷新会话过期时间（有交互则延期24小时）
func (r *SessionRepository) RefreshExpiredAt(id uint) error {
    newExpiredAt := time.Now().Add(24 * time.Hour)
    return r.db.Model(&models.UploadSession{}).Where("id = ?", id).Update("expired_at", newExpiredAt).Error
}

// Delete 删除会话
func (r *SessionRepository) Delete(id uint) error {
    return r.db.Delete(&models.UploadSession{}, id).Error
}

// ListByUser 根据用户ID查询上传会话（非临时/非私有，即公共上传）
func (r *SessionRepository) ListByUser(userID string, page, pageSize int) ([]models.UploadSession, int64, error) {
    var sessions []models.UploadSession
    query := r.db.Model(&models.UploadSession{}).
        Where("target_type = ? OR target_type = ?", models.TargetTypeRegular, "").
        Where("user_id = ?", userID)

    var total int64
    if err := query.Count(&total).Error; err != nil {
        return nil, 0, err
    }

    err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&sessions).Error
    return sessions, total, err
}

// ListByUserAndType 根据用户ID和存储类型查询上传会话
func (r *SessionRepository) ListByUserAndType(userID string, targetType models.TargetType, page, pageSize int) ([]models.UploadSession, int64, error) {
    var sessions []models.UploadSession
    query := r.db.Model(&models.UploadSession{}).
        Where("target_type = ?", targetType).
        Where("user_id = ?", userID)

    var total int64
    if err := query.Count(&total).Error; err != nil {
        return nil, 0, err
    }

    err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&sessions).Error
    return sessions, total, err
}

// ChunkRepository 分片仓库
type ChunkRepository struct {
    db *gorm.DB
}

// NewChunkRepository 创建分片仓库实例
func NewChunkRepository(db *gorm.DB) *ChunkRepository {
    return &ChunkRepository{db: db}
}

// Create 创建分片记录
func (r *ChunkRepository) Create(chunk *models.UploadedChunk) error {
    return r.db.Create(chunk).Error
}

// GetUploadedIndexes 获取已上传的分片索引
func (r *ChunkRepository) GetUploadedIndexes(sessionID uint) ([]int, error) {
    var chunks []models.UploadedChunk
    err := r.db.Where("session_id = ?", sessionID).Order("chunk_index ASC").Find(&chunks).Error
    if err != nil {
        return nil, err
    }
    indexes := make([]int, len(chunks))
    for i, c := range chunks {
        indexes[i] = c.ChunkIndex
    }
    return indexes, nil
}

// DeleteBySessionID 删除会话的所有分片记录
func (r *ChunkRepository) DeleteBySessionID(sessionID uint) error {
    return r.db.Where("session_id = ?", sessionID).Delete(&models.UploadedChunk{}).Error
}

// RecordRepository 上传记录仓库
type RecordRepository struct {
    db *gorm.DB
}

// NewRecordRepository 创建上传记录仓库实例
func NewRecordRepository(db *gorm.DB) *RecordRepository {
    return &RecordRepository{db: db}
}

// Create 创建上传记录
func (r *RecordRepository) Create(record *models.OperationRecord) error {
    return r.db.Create(record).Error
}

// GetByID 根据ID获取记录
func (r *RecordRepository) GetByID(id uint) (*models.OperationRecord, error) {
    var record models.OperationRecord
    err := r.db.First(&record, id).Error
    if err != nil {
        return nil, err
    }
    return &record, nil
}

// ListByUserID 获取用户的所有记录
func (r *RecordRepository) ListByUserID(userID string, page, pageSize int) ([]models.OperationRecord, int64, error) {
    var records []models.OperationRecord
    var total int64

    query := r.db.Model(&models.OperationRecord{}).Where("user_id = ?", userID)
    query.Count(&total)

    offset := (page - 1) * pageSize
    err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&records).Error
    return records, total, err
}

// ListByIP 获取IP的所有记录
func (r *RecordRepository) ListByIP(clientIP string, page, pageSize int) ([]models.OperationRecord, int64, error) {
    var records []models.OperationRecord
    var total int64

    query := r.db.Model(&models.OperationRecord{}).Where("client_ip = ?", clientIP)
    query.Count(&total)

    offset := (page - 1) * pageSize
    err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&records).Error
    return records, total, err
}

// ListRecent 获取最近的记录
func (r *RecordRepository) ListRecent(clientIP string, limit int) ([]models.OperationRecord, error) {
    var records []models.OperationRecord
    err := r.db.Where("client_ip = ?", clientIP).Order("created_at DESC").Limit(limit).Find(&records).Error
    return records, err
}

// ListRecentAll 获取最近的记录（不限IP，用于管理界面）
// action 参数可指定特定操作类型，如 "upload", "download", "search"，为空则返回所有类型
func (r *RecordRepository) ListRecentAll(limit int, action string) ([]models.OperationRecord, error) {
    var records []models.OperationRecord
    query := r.db.Model(&models.OperationRecord{})
    if action != "" {
        query = query.Where("`action` = ?", action)
    }
    err := query.Order("created_at DESC").Limit(limit).Find(&records).Error
    return records, err
}

// ListRecentAllPaginated 获取最近的记录（分页版，不限IP）
// 返回记录列表和总记录数
func (r *RecordRepository) ListRecentAllPaginated(page, pageSize int, action string) ([]models.OperationRecord, int64, error) {
    var records []models.OperationRecord
    var total int64

    query := r.db.Model(&models.OperationRecord{})
    if action != "" {
        query = query.Where("`action` = ?", action)
    }

    query.Count(&total)

    offset := (page - 1) * pageSize
    err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&records).Error
    return records, total, err
}

// Delete 删除记录
func (r *RecordRepository) Delete(id uint) error {
    return r.db.Delete(&models.OperationRecord{}, id).Error
}

// GetAll 获取所有记录（用于统计）
func (r *RecordRepository) GetAll() ([]models.OperationRecord, error) {
    var records []models.OperationRecord
    err := r.db.Order("created_at DESC").Find(&records).Error
    return records, err
}

// GetRecent 获取最近的记录（不限IP）
func (r *RecordRepository) GetRecent(limit int) ([]models.OperationRecord, error) {
    var records []models.OperationRecord
    err := r.db.Where("`action` IN ?", []string{"upload", "download"}).Order("created_at DESC").Limit(limit).Find(&records).Error
    return records, err
}

// GetRecentSearches 获取最近的搜索记录
func (r *RecordRepository) GetRecentSearches(limit int) ([]models.OperationRecord, error) {
	var records []models.OperationRecord
	err := r.db.Where("`action` = ? AND search_query IS NOT NULL AND search_query != ''", "search").Order("created_at DESC").Limit(limit).Find(&records).Error
	return records, err
}

// DeleteByAction 删除指定操作类型的所有记录
// action 有效值: "search", "upload", "download"
// 返回删除的记录数
func (r *RecordRepository) DeleteByAction(action string) (int64, error) {
	result := r.db.Where("`action` = ?", action).Delete(&models.OperationRecord{})
	return result.RowsAffected, result.Error
}