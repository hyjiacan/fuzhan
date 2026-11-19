package services

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

// BatchUserResult 批量用户操作的逐条结果
type BatchUserResult struct {
	UUID     string `json:"uuid"`
	Username string `json:"username"`
	Status   string `json:"status"` // ok / skipped / failed
	Message  string `json:"message"`
}

// loadUsersByUUIDs 按 UUID 批量查询用户，返回 uuid → 用户 映射（不存在的 UUID 不会出现在结果中）
func (s *AdminService) loadUsersByUUIDs(uuids []string) (map[string]models.User, error) {
	users := make([]models.User, 0, len(uuids))
	if err := s.db.Where("uuid IN ?", uuids).Find(&users).Error; err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	result := make(map[string]models.User, len(users))
	for _, user := range users {
		result[user.UUID] = user
	}
	return result, nil
}

// requireAdmin 校验调用者为管理员，批量操作入口统一使用
func (s *AdminService) requireAdmin(callerUUID, action string) (models.User, error) {
	var caller models.User
	if err := s.db.Where("uuid = ? AND role = ?", callerUUID, "admin").First(&caller).Error; err != nil {
		utils.Warn("批量操作用户失败: 无权操作", utils.String("action", action), utils.String("caller_uuid", callerUUID))
		return caller, newUserIdentityError(http.StatusForbidden, "无权操作：仅管理员可执行该操作")
	}
	return caller, nil
}

// BatchDeleteUsers 批量删除用户，逐条返回结果；管理员账号与不存在的账号记为跳过。
func (s *AdminService) BatchDeleteUsers(callerUUID string, uuids []string) ([]BatchUserResult, error) {
	caller, err := s.requireAdmin(callerUUID, "batch_delete")
	if err != nil {
		return nil, err
	}
	users, err := s.loadUsersByUUIDs(uuids)
	if err != nil {
		utils.Error("批量删除用户失败: 查询用户错误", utils.Err(err))
		return nil, err
	}

	results := make([]BatchUserResult, 0, len(uuids))
	var okCount, skippedCount, failedCount int
	for _, uuid := range uuids {
		result := BatchUserResult{UUID: uuid, Status: "skipped"}
		user, exists := users[uuid]
		if !exists {
			result.Message = "用户不存在"
			skippedCount++
			results = append(results, result)
			continue
		}
		result.Username = user.Username
		if err := s.DeleteUser(uuid); err != nil {
			result.Message = err.Error()
			if strings.Contains(err.Error(), "不能删除") || strings.Contains(err.Error(), "不存在") {
				skippedCount++
			} else {
				result.Status = "failed"
				failedCount++
			}
			results = append(results, result)
			continue
		}
		result.Status = "ok"
		result.Message = "已删除"
		okCount++
		results = append(results, result)
	}

	utils.Info("管理员批量删除用户完成",
		utils.String("admin", caller.Username),
		utils.Int("requested", len(uuids)),
		utils.Int("ok", okCount),
		utils.Int("skipped", skippedCount),
		utils.Int("failed", failedCount))
	return results, nil
}

// BatchDisableUsers 批量禁用用户，逐条返回结果；管理员账号、已禁用账号记为跳过。
func (s *AdminService) BatchDisableUsers(callerUUID string, uuids []string) ([]BatchUserResult, error) {
	caller, err := s.requireAdmin(callerUUID, "batch_disable")
	if err != nil {
		return nil, err
	}
	users, err := s.loadUsersByUUIDs(uuids)
	if err != nil {
		utils.Error("批量禁用用户失败: 查询用户错误", utils.Err(err))
		return nil, err
	}

	results := make([]BatchUserResult, 0, len(uuids))
	var okCount, skippedCount, failedCount int
	for _, uuid := range uuids {
		result := BatchUserResult{UUID: uuid, Status: "skipped"}
		user, exists := users[uuid]
		if !exists {
			result.Message = "用户不存在"
			skippedCount++
			results = append(results, result)
			continue
		}
		result.Username = user.Username
		switch {
		case user.Role == "admin":
			result.Message = "不能禁用管理员账号"
			skippedCount++
			results = append(results, result)
			continue
		case user.Disabled:
			result.Message = "该用户已是禁用状态"
			skippedCount++
			results = append(results, result)
			continue
		}
		if err := s.ToggleUserStatus(uuid, true); err != nil {
			result.Status = "failed"
			result.Message = err.Error()
			failedCount++
			results = append(results, result)
			continue
		}
		result.Status = "ok"
		result.Message = "已禁用"
		okCount++
		results = append(results, result)
	}

	utils.Info("管理员批量禁用用户完成",
		utils.String("admin", caller.Username),
		utils.Int("requested", len(uuids)),
		utils.Int("ok", okCount),
		utils.Int("skipped", skippedCount),
		utils.Int("failed", failedCount))
	return results, nil
}

// BatchConvertUsersToLocal 批量把外部认证账号转为本地账号，统一写入同一新密码。
// 已是本地账号、不存在的账号记为跳过；单条失败不影响其它条目。
func (s *AdminService) BatchConvertUsersToLocal(callerUUID string, uuids []string, newPassword string) ([]BatchUserResult, error) {
	caller, err := s.requireAdmin(callerUUID, "batch_convert_local")
	if err != nil {
		return nil, err
	}
	if len(newPassword) < 8 || len(newPassword) > 128 {
		return nil, newUserIdentityError(http.StatusBadRequest, "新密码长度需在8到128个字符之间")
	}
	users, err := s.loadUsersByUUIDs(uuids)
	if err != nil {
		utils.Error("批量转为本地账号失败: 查询用户错误", utils.Err(err))
		return nil, err
	}

	results := make([]BatchUserResult, 0, len(uuids))
	var okCount, skippedCount, failedCount int
	for _, uuid := range uuids {
		result := BatchUserResult{UUID: uuid, Status: "skipped"}
		user, exists := users[uuid]
		if !exists {
			result.Message = "用户不存在"
			skippedCount++
			results = append(results, result)
			continue
		}
		result.Username = user.Username
		if user.AuthSource == "" || user.AuthSource == models.AuthSourceLocal {
			result.Message = "该用户已是本地账号"
			skippedCount++
			results = append(results, result)
			continue
		}
		if err := s.ConvertUserToLocal(callerUUID, uuid, newPassword); err != nil {
			var identityErr *UserIdentityError
			if errors.As(err, &identityErr) {
				result.Message = identityErr.Message
				if identityErr.Status == http.StatusBadRequest {
					skippedCount++
				} else {
					result.Status = "failed"
					failedCount++
				}
			} else {
				result.Status = "failed"
				result.Message = "转为本地账号失败"
				failedCount++
			}
			results = append(results, result)
			continue
		}
		result.Status = "ok"
		result.Message = "已转为本地账号"
		okCount++
		results = append(results, result)
	}

	utils.Info("管理员批量转为本地账号完成",
		utils.String("admin", caller.Username),
		utils.Int("requested", len(uuids)),
		utils.Int("ok", okCount),
		utils.Int("skipped", skippedCount),
		utils.Int("failed", failedCount))
	return results, nil
}
