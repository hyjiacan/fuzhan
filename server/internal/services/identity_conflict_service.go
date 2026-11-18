package services

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"

	"gorm.io/gorm"
)

// IdentityConflictItem 管理端身份冲突列表项
type IdentityConflictItem struct {
	ID                uint      `json:"id"`
	Username          string    `json:"username"`
	AuthType          string    `json:"auth_type"`
	ProviderName      string    `json:"provider_name"`
	ExternalID        string    `json:"external_id"`
	ExternalName      string    `json:"external_name"`
	RejectedSource    string    `json:"rejected_source"`
	AttemptCount      int64     `json:"attempt_count"`
	LastSeenAt        time.Time `json:"last_seen_at"`
	SuggestedUserUUID string    `json:"suggested_user_uuid"` // 同名本地账号 UUID，不存在为空
	SuggestedUsername string    `json:"suggested_username"`  // 同名本地账号用户名，不存在为空
	CanBind           bool      `json:"can_bind"`            // 冲突的认证源当前是否已启用
}

// IdentityConflictListResult 身份冲突列表结果
type IdentityConflictListResult struct {
	Conflicts []IdentityConflictItem `json:"conflicts"`
	Total     int64                  `json:"total"`
}

// BatchConflictResult 批量处理身份冲突的单条结果
type BatchConflictResult struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Status   string `json:"status"` // bound / skipped / failed
	Message  string `json:"message"`
}

// conflictSkipError 批量处理中的可预期跳过原因（不作为接口级错误返回）
type conflictSkipError struct {
	message string
}

func (e conflictSkipError) Error() string {
	return e.message
}

// errConflictResolved 事务内发现冲突已被其他请求处理
var errConflictResolved = errors.New("该冲突已处理")

// ListIdentityConflicts 分页查询身份冲突。
// status：pending（默认，ResolvedAt 为空）/ resolved（已处理）/ all（全部）。
func (s *AdminService) ListIdentityConflicts(status string, page, pageSize int) (*IdentityConflictListResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	query := s.db.Model(&models.IdentityConflict{})
	switch strings.TrimSpace(status) {
	case "resolved":
		query = query.Where("resolved_at IS NOT NULL")
	case "all":
		// 不过滤
	default: // pending
		query = query.Where("resolved_at IS NULL")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		utils.Error("查询身份冲突总数失败", utils.Err(err))
		return nil, fmt.Errorf("查询身份冲突失败: %w", err)
	}

	var conflicts []models.IdentityConflict
	if err := query.Order("last_seen_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&conflicts).Error; err != nil {
		utils.Error("查询身份冲突列表失败", utils.Int("page", page), utils.Int("page_size", pageSize), utils.Err(err))
		return nil, fmt.Errorf("查询身份冲突失败: %w", err)
	}

	policy := loadIdentityPolicy()
	items := make([]IdentityConflictItem, len(conflicts))
	for i, c := range conflicts {
		item := IdentityConflictItem{
			ID:             c.ID,
			Username:       c.Username,
			AuthType:       c.AuthType,
			ProviderName:   c.ProviderName,
			ExternalID:     c.ExternalID,
			ExternalName:   c.ExternalName,
			RejectedSource: c.RejectedSource,
			AttemptCount:   c.AttemptCount,
			LastSeenAt:     c.LastSeenAt,
			CanBind:        policy.sourceReady(c.AuthType),
		}
		// 建议本地账号：先精确匹配用户名，再按 LOWER(username) 匹配
		if user, err := findLocalUserByUsername(s.db, c.Username); err != nil {
			utils.Warn("查询身份冲突的建议账号失败", utils.String("username", c.Username), utils.Err(err))
		} else if user != nil {
			item.SuggestedUserUUID = user.UUID
			item.SuggestedUsername = user.Username
		}
		items[i] = item
	}

	return &IdentityConflictListResult{Conflicts: items, Total: total}, nil
}

// findLocalUserByUsername 按用户名查找本地用户：先精确匹配，再大小写不敏感匹配。
// 未命中返回 (nil, nil)。
func findLocalUserByUsername(db *gorm.DB, username string) (*models.User, error) {
	var user models.User
	err := db.Where("username = ?", username).First(&user).Error
	if err == nil {
		return &user, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := db.Where("LOWER(username) = ?", strings.ToLower(username)).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// BatchBindIdentityConflicts 逐条把冲突的外部身份绑定到同名本地账号。
// 规则与 BindExternalIdentity 一致：认证源需启用、目标用户无其它绑定、外部身份未被占用；
// 成功后写绑定、切换 auth_source、清空本地密码，并标记冲突为 bound。
// 单条失败/跳过不影响其它条目，返回逐条结果。
func (s *AdminService) BatchBindIdentityConflicts(callerUUID string, ids []uint) ([]BatchConflictResult, error) {
	var caller models.User
	if err := s.db.Where("uuid = ? AND role = ?", callerUUID, "admin").First(&caller).Error; err != nil {
		utils.Warn("批量绑定外部身份失败: 无权操作", utils.String("caller_uuid", callerUUID))
		return nil, newUserIdentityError(http.StatusForbidden, "无权操作：仅管理员可绑定外部身份")
	}

	policy := loadIdentityPolicy()
	results := make([]BatchConflictResult, 0, len(ids))
	var boundCount, skippedCount, failedCount int
	for _, id := range ids {
		result := BatchConflictResult{ID: id, Status: "skipped"}
		var conflict models.IdentityConflict
		if err := s.db.Where("id = ?", id).First(&conflict).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				result.Message = "冲突记录不存在"
			} else {
				result.Status = "failed"
				result.Message = "查询冲突记录失败"
			}
			results = append(results, result)
			continue
		}
		result.Username = conflict.Username

		if conflict.ResolvedAt != nil {
			result.Message = "该冲突已处理"
			results = append(results, result)
			continue
		}
		if conflict.AuthType != models.AuthSourceLDAP && conflict.AuthType != models.AuthSourceOAuth2 {
			result.Message = "认证类型无效"
			results = append(results, result)
			continue
		}
		if !policy.sourceReady(conflict.AuthType) {
			result.Message = "认证源未启用"
			results = append(results, result)
			continue
		}

		user, err := findLocalUserByUsername(s.db, conflict.Username)
		if err != nil {
			result.Status = "failed"
			result.Message = "查询本地账号失败"
			utils.Warn("批量绑定外部身份: 查询本地账号失败", utils.String("username", conflict.Username), utils.Err(err))
			results = append(results, result)
			continue
		}
		if user == nil {
			result.Message = "本地无同名账号"
			results = append(results, result)
			continue
		}

		err = s.bindConflictToUser(&conflict, user)
		var skip conflictSkipError
		switch {
		case err == nil:
			result.Status = "bound"
			result.Message = fmt.Sprintf("已绑定到本地账号 %s", user.Username)
			boundCount++
		case errors.Is(err, errConflictResolved):
			result.Message = "该冲突已处理"
			skippedCount++
		case errors.As(err, &skip):
			result.Message = skip.message
			skippedCount++
		default:
			result.Status = "failed"
			result.Message = err.Error()
			failedCount++
			utils.Warn("批量绑定外部身份失败",
				utils.String("conflict_id", fmt.Sprint(conflict.ID)),
				utils.String("username", conflict.Username),
				utils.Err(err))
		}
		results = append(results, result)
	}

	utils.Info("管理员批量绑定外部身份完成",
		utils.String("admin", caller.Username),
		utils.Int("requested", len(ids)),
		utils.Int("bound", boundCount),
		utils.Int("skipped", skippedCount),
		utils.Int("failed", failedCount))
	return results, nil
}

// bindConflictToUser 在事务内完成冲突绑定：写 AuthRecord、切换 auth_source、清空密码、标记冲突已处理。
func (s *AdminService) bindConflictToUser(conflict *models.IdentityConflict, user *models.User) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// 同一外部身份全局唯一：已绑定其他用户时跳过
		var existing models.AuthRecord
		err := tx.Where("auth_type = ? AND provider_name = ? AND external_id = ?",
			conflict.AuthType, conflict.ProviderName, conflict.ExternalID).First(&existing).Error
		switch {
		case err == nil:
			if existing.UserID == user.ID {
				return conflictSkipError{message: "该用户已绑定该外部身份"}
			}
			return conflictSkipError{message: "该外部身份已绑定其他用户"}
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return fmt.Errorf("检查外部身份绑定失败: %w", err)
		}

		// 不支持裸解绑/换绑：目标用户已有绑定时需先转为本地账号
		var bindingCount int64
		if err := tx.Model(&models.AuthRecord{}).Where("user_id = ?", user.ID).Count(&bindingCount).Error; err != nil {
			return fmt.Errorf("检查用户绑定失败: %w", err)
		}
		if bindingCount > 0 {
			return conflictSkipError{message: "该用户已绑定其他认证来源，请先转为本地账号"}
		}

		now := time.Now().UTC()
		// 先标记冲突，作为并发下的抢占屏障；已被处理则回滚
		update := tx.Model(&models.IdentityConflict{}).
			Where("id = ? AND resolved_at IS NULL", conflict.ID).
			Updates(map[string]any{
				"resolved_at": now,
				"resolution":  models.IdentityConflictResolutionBound,
				"updated_at":  now,
			})
		if update.Error != nil {
			return fmt.Errorf("更新身份冲突状态失败: %w", update.Error)
		}
		if update.RowsAffected == 0 {
			return errConflictResolved
		}

		record := &models.AuthRecord{
			UserID:       user.ID,
			AuthType:     conflict.AuthType,
			ExternalID:   conflict.ExternalID,
			ExternalName: conflict.ExternalName,
			ProviderName: conflict.ProviderName,
		}
		if err := tx.Create(record).Error; err != nil {
			if isUniqueViolation(err) {
				return conflictSkipError{message: "该外部身份已绑定其他用户"}
			}
			return fmt.Errorf("保存外部身份绑定失败: %w", err)
		}

		if err := tx.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]interface{}{
			"auth_source":   conflict.AuthType,
			"password_hash": "",
		}).Error; err != nil {
			return fmt.Errorf("更新用户认证来源失败: %w", err)
		}
		return nil
	})
}

// BatchIgnoreIdentityConflicts 批量忽略待处理的身份冲突，返回实际更新条数。
// 仅 pending（ResolvedAt 为空）记录会变为 ignored；已处理记录不受影响。
func (s *AdminService) BatchIgnoreIdentityConflicts(callerUUID string, ids []uint) (int, error) {
	var caller models.User
	if err := s.db.Where("uuid = ? AND role = ?", callerUUID, "admin").First(&caller).Error; err != nil {
		utils.Warn("批量忽略身份冲突失败: 无权操作", utils.String("caller_uuid", callerUUID))
		return 0, newUserIdentityError(http.StatusForbidden, "无权操作：仅管理员可忽略身份冲突")
	}

	now := time.Now().UTC()
	result := s.db.Model(&models.IdentityConflict{}).
		Where("id IN ? AND resolved_at IS NULL", ids).
		Updates(map[string]any{
			"resolved_at": now,
			"resolution":  models.IdentityConflictResolutionIgnored,
			"updated_at":  now,
		})
	if result.Error != nil {
		utils.Error("批量忽略身份冲突失败", utils.Err(result.Error))
		return 0, fmt.Errorf("忽略身份冲突失败: %w", result.Error)
	}

	utils.Info("管理员批量忽略身份冲突",
		utils.String("admin", caller.Username),
		utils.Int("requested", len(ids)),
		utils.Int("ignored", int(result.RowsAffected)))
	return int(result.RowsAffected), nil
}
