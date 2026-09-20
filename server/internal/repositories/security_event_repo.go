package repositories

import (
	"time"

	"fuzhan/internal/models"
	"gorm.io/gorm"
)

// SecurityEventFilter 安全事件列表查询过滤条件（全部按 AND 组合，空字段不参与过滤）
type SecurityEventFilter struct {
	Severity   string
	EventType  string
	ClientIP   string
	Identity   string
	Result     string
	Guard      string
	Q          string    // 关键字：命中 reason / target / path / request_id
	UnreadOnly bool      // 仅未读
	From       time.Time // 开始时间（含）
	To         time.Time // 结束时间（不含）
}

// SecurityEventStore 安全事件存储接口
type SecurityEventStore interface {
	Create(ev *models.SecurityEvent) error
	CreateBatch(evs []*models.SecurityEvent) error
	List(filter SecurityEventFilter, page, pageSize int) ([]models.SecurityEvent, int64, error)
	// UnreadSummary 未读统计：总数、高危数及最近未读事件
	UnreadSummary(limit int) (total, high int64, recent []models.SecurityEvent, err error)
	MarkRead(ids []uint) (int64, error)
	MarkAllRead() (int64, error)
	// CountBySeverity 各严重级别计数（用于看板统计）
	CountBySeverity() (total, high, medium, low int64, err error)
	// EventsInRange 取时间范围内的事件（用于日趋势/序列分析与 TOP 聚合）
	EventsInRange(from, to time.Time, severities []string) ([]models.SecurityEvent, error)
}

// securityEventScope 查询基座（列出/统计共用）
type securityEventScope struct {
	db *gorm.DB
}

func (s *securityEventScope) applyFilter(q *gorm.DB, f SecurityEventFilter) *gorm.DB {
	if f.Severity != "" {
		q = q.Where("severity = ?", f.Severity)
	}
	if f.EventType != "" {
		q = q.Where("event_type = ?", f.EventType)
	}
	if f.ClientIP != "" {
		q = q.Where("client_ip = ?", f.ClientIP)
	}
	if f.Identity != "" {
		q = q.Where("identity = ?", f.Identity)
	}
	if f.Result != "" {
		q = q.Where("result = ?", f.Result)
	}
	if f.Guard != "" {
		q = q.Where("guard = ?", f.Guard)
	}
	if f.UnreadOnly {
		q = q.Where("read = ?", false)
	}
	if !f.From.IsZero() {
		q = q.Where("created_at >= ?", f.From)
	}
	if !f.To.IsZero() {
		q = q.Where("created_at < ?", f.To)
	}
	if f.Q != "" {
		kw := "%" + f.Q + "%"
		q = q.Where("(reason LIKE ? OR target LIKE ? OR path LIKE ? OR request_id LIKE ?)", kw, kw, kw, kw)
	}
	return q
}

// SecurityEventRepository 基于 GORM 的安全事件仓库
type SecurityEventRepository struct {
	db *gorm.DB
	securityEventScope
}

// NewSecurityEventRepository 创建安全事件仓库
func NewSecurityEventRepository(db *gorm.DB) *SecurityEventRepository {
	return &SecurityEventRepository{db: db, securityEventScope: securityEventScope{db: db}}
}

// Create 创建一条安全事件
func (r *SecurityEventRepository) Create(ev *models.SecurityEvent) error {
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now().UTC()
	}
	return r.db.Create(ev).Error
}

// CreateBatch 批量创建（单事务分片写入，降低高写流量下的逐条 insert 开销）
func (r *SecurityEventRepository) CreateBatch(evs []*models.SecurityEvent) error {
	if len(evs) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for i := range evs {
		if evs[i].CreatedAt.IsZero() {
			evs[i].CreatedAt = now
		}
	}
	return r.db.CreateInBatches(evs, 200).Error
}

// List 分页查询安全事件
func (r *SecurityEventRepository) List(filter SecurityEventFilter, page, pageSize int) ([]models.SecurityEvent, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var total int64
	q := r.applyFilter(r.db.Model(&models.SecurityEvent{}), filter)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []models.SecurityEvent
	err := r.applyFilter(r.db.Model(&models.SecurityEvent{}), filter).
		Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list).Error
	return list, total, err
}

// UnreadSummary 统计未读事件总数与高危未读数，并返回最近未读事件
func (r *SecurityEventRepository) UnreadSummary(limit int) (total, high int64, recent []models.SecurityEvent, err error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	base := r.db.Model(&models.SecurityEvent{}).Where("read = ?", false)
	if err = base.Count(&total).Error; err != nil {
		return
	}
	if err = base.Where("severity = ?", models.SeverityHigh).Count(&high).Error; err != nil {
		return
	}
	err = r.db.Model(&models.SecurityEvent{}).
		Where("read = ?", false).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&recent).Error
	return
}

// MarkRead 将指定事件标记为已读
func (r *SecurityEventRepository) MarkRead(ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := r.db.Model(&models.SecurityEvent{}).Where("id IN ?", ids).Update("read", true)
	return res.RowsAffected, res.Error
}

// MarkAllRead 清空未读标记
func (r *SecurityEventRepository) MarkAllRead() (int64, error) {
	res := r.db.Model(&models.SecurityEvent{}).Where("read = ?", false).Update("read", true)
	return res.RowsAffected, res.Error
}

// CountBySeverity 各严重级别事件总数
func (r *SecurityEventRepository) CountBySeverity() (total, high, medium, low int64, err error) {
	if err = r.db.Model(&models.SecurityEvent{}).Count(&total).Error; err != nil {
		return
	}
	rows := []struct {
		Severity string
		N        int64
	}{}
	if err = r.db.Model(&models.SecurityEvent{}).
		Select("severity, COUNT(*) as n").
		Group("severity").
		Find(&rows).Error; err != nil {
		return
	}
	for _, row := range rows {
		switch row.Severity {
		case models.SeverityHigh:
			high = row.N
		case models.SeverityMedium:
			medium = row.N
		case models.SeverityLow:
			low = row.N
		}
	}
	return
}

// EventsInRange 取时间范围（可为空）内指定级别的全部事件
func (r *SecurityEventRepository) EventsInRange(from, to time.Time, severities []string) ([]models.SecurityEvent, error) {
	q := r.db.Model(&models.SecurityEvent{})
	if len(severities) > 0 {
		q = q.Where("severity IN ?", severities)
	}
	if !from.IsZero() {
		q = q.Where("created_at >= ?", from)
	}
	if !to.IsZero() {
		q = q.Where("created_at < ?", to)
	}
	var list []models.SecurityEvent
	err := q.Order("created_at DESC, id DESC").Find(&list).Error
	return list, err
}
