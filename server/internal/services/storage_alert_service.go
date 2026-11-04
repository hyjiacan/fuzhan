package services

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/mailer"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"fuzhan/internal/webhook"
	"gorm.io/gorm"
)

// StorageAlertRoot 单个根目录的使用率评估结果。
type StorageAlertRoot struct {
	Name         string  `json:"name"`
	Path         string  `json:"path"`
	UsagePercent float64 `json:"usagePercent"`
	Used         int64   `json:"used"`
	Quota        int64   `json:"quota"`
	Total        int64   `json:"total"`
	Free         int64   `json:"free"`
}

// StorageAlertEvaluation 一次存储告警评估结果。
type StorageAlertEvaluation struct {
	ThresholdPercent float64            `json:"thresholdPercent"`
	Triggered        bool               `json:"triggered"`
	Level            string             `json:"level"`
	Roots            []StorageAlertRoot `json:"roots"`
	MaxUsagePercent  float64            `json:"maxUsagePercent"`
	OccurredAt       time.Time          `json:"occurredAt"`
}

// StorageAlertService 周期性检查存储使用率，并在达到阈值时通过
// Web 消息（仅管理员）、Webhook 与邮件三种通道发送告警。
type StorageAlertService struct {
	db *gorm.DB

	lastCheck time.Time
	lastAlert time.Time
	wasOver   bool
}

// NewStorageAlertService 创建存储告警服务。
func NewStorageAlertService(db *gorm.DB) *StorageAlertService {
	return &StorageAlertService{db: db}
}

// Run 启动周期检查循环，直到 ctx 结束。
func (s *StorageAlertService) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *StorageAlertService) tick() {
	cfg := appconfig.GetStorageAlertConfig()
	if !cfg.Enabled {
		s.wasOver = false
		return
	}
	interval := time.Duration(cfg.CheckIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	if !s.lastCheck.IsZero() && time.Since(s.lastCheck) < interval {
		return
	}
	s.lastCheck = time.Now()
	s.checkAndNotify(cfg)
}

func (s *StorageAlertService) checkAndNotify(cfg appconfig.StorageAlertConfig) {
	evaluation, err := s.evaluate(cfg)
	if err != nil {
		utils.Warn("存储告警检查失败", utils.Err(err))
		return
	}
	if !evaluation.Triggered {
		s.wasOver = false
		return
	}

	cooldown := time.Duration(cfg.CooldownMinutes) * time.Minute
	if s.wasOver && time.Since(s.lastAlert) < cooldown {
		return
	}
	s.notify(cfg, evaluation)
	s.lastAlert = time.Now()
	s.wasOver = true
}

func (s *StorageAlertService) evaluate(cfg appconfig.StorageAlertConfig) (*StorageAlertEvaluation, error) {
	evaluation := &StorageAlertEvaluation{
		ThresholdPercent: cfg.ThresholdPercent,
		OccurredAt:       time.Now(),
		Roots:            make([]StorageAlertRoot, 0),
		Level:            models.AlertLevelWarning,
	}
	// 远端存储后端的根目录 Path 是虚拟前缀而非本地路径，无法通过本机磁盘统计
	// 真实使用率，跳过检测以避免误报或永不告警。
	if appconfig.StorageBackendIsRemote() {
		return evaluation, nil
	}

	// 使用索引表聚合已用空间，避免每次检查都对所有根目录执行整盘 Walk。
	usedByRoot, err := s.indexedBytesByRoot()
	if err != nil {
		return nil, err
	}

	for _, rootDir := range appconfig.GlobalConfig.Storage.Public.RootDirs {
		if rootDir.Path == "" {
			continue
		}
		name := rootDirName(rootDir.Path)
		root := RootStorage{
			Name:  name,
			Path:  rootDir.Path,
			Quota: rootDir.Quota,
			Used:  usedByRoot[name],
		}
		if root.Quota <= 0 {
			root.Total, root.Free, _ = GetDiskSpace(rootDir.Path)
		}
		percent := rootUsagePercent(root)
		if percent <= 0 {
			continue
		}
		if percent >= cfg.ThresholdPercent {
			evaluation.Roots = append(evaluation.Roots, StorageAlertRoot{
				Name:         root.Name,
				Path:         root.Path,
				UsagePercent: percent,
				Used:         root.Used,
				Quota:        root.Quota,
				Total:        root.Total,
				Free:         root.Free,
			})
		}
		if percent > evaluation.MaxUsagePercent {
			evaluation.MaxUsagePercent = percent
		}
	}

	sort.Slice(evaluation.Roots, func(i, j int) bool {
		return evaluation.Roots[i].UsagePercent > evaluation.Roots[j].UsagePercent
	})
	evaluation.Triggered = len(evaluation.Roots) > 0
	evaluation.Level = models.AlertLevelWarning
	if evaluation.MaxUsagePercent >= 95 {
		evaluation.Level = models.AlertLevelCritical
	}
	return evaluation, nil
}

// indexedBytesByRoot 按根目录汇总索引中活动文件的大小。
func (s *StorageAlertService) indexedBytesByRoot() (map[string]int64, error) {
	type rootBytes struct {
		RootName string
		Total    int64
	}
	var rows []rootBytes
	err := s.db.Model(&models.FileRecordPublic{}).
		Select("root_name, COALESCE(SUM(file_size), 0) AS total").
		Where("status = ? AND is_dir = ?", models.FileStatusActive, false).
		Group("root_name").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	used := make(map[string]int64, len(rows))
	for _, row := range rows {
		used[row.RootName] = row.Total
	}
	return used, nil
}

// rootDirName 返回根目录配置对应的根名（未在 RootNames 中登记时回退为目录名）。
func rootDirName(rootPath string) string {
	for name, path := range appconfig.RootNames {
		if path == rootPath {
			return name
		}
	}
	return filepath.Base(rootPath)
}

// rootUsagePercent 计算根目录使用率：配置配额时按配额计算，否则按所在磁盘使用率计算。
func rootUsagePercent(root RootStorage) float64 {
	if root.Quota > 0 {
		return percent(root.Used, root.Quota)
	}
	total, free, err := GetDiskSpace(root.Path)
	if err != nil || total <= 0 {
		if root.Total > 0 {
			return percent(root.Used, root.Total)
		}
		return 0
	}
	return percent(total-free, total)
}

func percent(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return math.Round(float64(used)/float64(total)*10000) / 100
}

func (s *StorageAlertService) notify(cfg appconfig.StorageAlertConfig, evaluation *StorageAlertEvaluation) {
	title, content := buildStorageAlertText(evaluation)
	payload := map[string]interface{}{
		"level":            evaluation.Level,
		"title":            title,
		"content":          content,
		"thresholdPercent": evaluation.ThresholdPercent,
		"maxUsagePercent":  evaluation.MaxUsagePercent,
		"roots":            evaluation.Roots,
		"occurredAt":       evaluation.OccurredAt.Format(time.RFC3339),
	}

	if cfg.WebEnabled {
		rootName := ""
		if len(evaluation.Roots) > 0 {
			rootName = evaluation.Roots[0].Name
		}
		message := models.AlertMessage{
			Level:        evaluation.Level,
			Title:        title,
			Content:      content,
			UsagePercent: evaluation.MaxUsagePercent,
			RootName:     rootName,
			CreatedAt:    evaluation.OccurredAt,
		}
		if err := s.db.Create(&message).Error; err != nil {
			utils.Warn("写入存储告警消息失败", utils.Err(err))
		}
	}

	if cfg.WebhookEnabled && webhook.Enabled() {
		if ok := webhook.Dispatch(webhook.EventStorageAlert, "storage", payload); !ok {
			utils.Warn("存储告警 Webhook 入队失败（可能队列已满或未启用）")
		}
	}

	if cfg.EmailEnabled {
		if err := sendStorageAlertEmail(cfg, title, content); err != nil {
			utils.Warn("发送存储告警邮件失败", utils.Err(err))
		}
	}

	utils.Warn("存储使用率告警已触发",
		utils.String("level", evaluation.Level),
		utils.String("max_usage_percent", fmt.Sprintf("%.2f", evaluation.MaxUsagePercent)),
		utils.String("threshold_percent", fmt.Sprintf("%.2f", evaluation.ThresholdPercent)),
		utils.Int("over_threshold_roots", len(evaluation.Roots)))
}

func sendStorageAlertEmail(cfg appconfig.StorageAlertConfig, title, content string) error {
	smtpCfg := mailer.Config{
		Host:     cfg.SMTP.Host,
		Port:     cfg.SMTP.Port,
		Username: cfg.SMTP.Username,
		Password: cfg.SMTP.Password,
		From:     cfg.SMTP.From,
		TLSMode:  cfg.SMTP.TLSMode,
	}
	return mailer.Send(smtpCfg, mailer.Message{
		To:      cfg.EmailRecipients,
		Subject: title,
		Body:    content,
	})
}

func buildStorageAlertText(evaluation *StorageAlertEvaluation) (title, content string) {
	if len(evaluation.Roots) == 0 {
		return "存储使用率告警", ""
	}
	first := evaluation.Roots[0]
	title = fmt.Sprintf("存储告警：根目录 %s 使用率 %.2f%%", first.Name, first.UsagePercent)

	var b strings.Builder
	fmt.Fprintf(&b, "存储使用率已达到告警阈值 %.2f%%。\n", evaluation.ThresholdPercent)
	fmt.Fprintf(&b, "最高使用率：%.2f%%\n\n", evaluation.MaxUsagePercent)
	for _, root := range evaluation.Roots {
		if root.Quota > 0 {
			fmt.Fprintf(&b, "- %s（%s）：%.2f%%，已用 %s / 配额 %s\n",
				root.Name, root.Path, root.UsagePercent, formatBytes(root.Used), formatBytes(root.Quota))
		} else {
			fmt.Fprintf(&b, "- %s（%s）：%.2f%%，磁盘 %s / %s（剩余 %s）\n",
				root.Name, root.Path, root.UsagePercent, formatBytes(root.Total-root.Free), formatBytes(root.Total), formatBytes(root.Free))
		}
	}
	content = b.String()
	return title, content
}

func formatBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	value := float64(n)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d%s", n, units[unit])
	}
	return fmt.Sprintf("%.2f%s", value, units[unit])
}

// ListMessages 分页查询管理员告警消息。
func (s *StorageAlertService) ListMessages(page, pageSize int, unreadOnly bool) ([]models.AlertMessage, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	query := s.db.Model(&models.AlertMessage{})
	if unreadOnly {
		query = query.Where("is_read = ?", false)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []models.AlertMessage
	if err := query.Order("created_at DESC, id DESC").
		Limit(pageSize).Offset((page - 1) * pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// UnreadCount 返回未读告警消息数量。
func (s *StorageAlertService) UnreadCount() (int64, error) {
	var count int64
	err := s.db.Model(&models.AlertMessage{}).Where("is_read = ?", false).Count(&count).Error
	return count, err
}

// MarkRead 将指定告警消息标记为已读。
func (s *StorageAlertService) MarkRead(id uint) error {
	return s.db.Model(&models.AlertMessage{}).Where("id = ?", id).Update("is_read", true).Error
}

// MarkAllRead 将所有未读告警消息标记为已读。
func (s *StorageAlertService) MarkAllRead() error {
	return s.db.Model(&models.AlertMessage{}).Where("is_read = ?", false).Update("is_read", true).Error
}
