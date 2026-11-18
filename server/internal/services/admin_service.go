package services

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"github.com/zeebo/xxh3"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// UserListResult 用户列表结果
type UserListResult struct {
	Users []UserItem `json:"users"`
	Total int64      `json:"total"`
}

// UserItem 用户列表项
type UserItem struct {
	UUID          string    `json:"uuid"`
	Username      string    `json:"username"`
	DisplayName   string    `json:"display_name"`
	Nickname      string    `json:"nickname"`
	Role          string    `json:"role"`
	Disabled      bool      `json:"disabled"`
	AuthSource    string    `json:"auth_source"`
	CreatedVia    string    `json:"created_via"`
	IdentityState string    `json:"identity_state"`
	ProviderName  string    `json:"provider_name"`
	ExternalID    string    `json:"external_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	UsedStorage   int64     `json:"used_storage"`
	Quota         int64     `json:"quota"`
}

// UserListFilter 管理端用户列表过滤条件（均已由 handler 做白名单校验）
type UserListFilter struct {
	Query         string // 用户名 / UUID / 姓名 / 昵称模糊匹配
	Source        string // local / ldap / oauth2
	CreatedVia    string // self_register / admin_create / ldap_jit / ldap_sync / oauth2_jit / legacy
	IdentityState string // ok / orphaned / provider_mismatch / inconsistent
}

// SessionListResult 会话列表结果
type SessionListResult struct {
	Sessions []SessionItem `json:"sessions"`
	Total    int64         `json:"total"`
	Stats    SessionStats  `json:"stats"`
}

// SessionStats 会话的全局聚合统计（与分页无关，用于统计卡片）
type SessionStats struct {
	TotalSessions  int64 `json:"total_sessions"`
	ZombieSessions int64 `json:"zombie_sessions"`
	TotalSize      int64 `json:"total_size"`
}

// SessionItem 会话列表项
type SessionItem struct {
	ID           uint   `json:"id"`
	FileName     string `json:"file_name"`
	FileSize     int64  `json:"file_size"`
	UploadedSize int64  `json:"uploaded_size"`
	Status       string `json:"status"`
	TargetPath   string `json:"target_path"`
	TargetRoot   string `json:"target_root"`
	CreatedAt    string `json:"created_at"`
	ExpiredAt    string `json:"expired_at"`
}

// UserIdentityError 管理端用户身份操作的可预期错误。
// Handler 依据 Status 映射 HTTP 状态码，避免用字符串匹配区分 400/403/409。
type UserIdentityError struct {
	Status  int
	Message string
}

// Error 实现 error 接口
func (e *UserIdentityError) Error() string {
	return e.Message
}

// newUserIdentityError 构造带建议状态码的业务错误
func newUserIdentityError(status int, message string) error {
	return &UserIdentityError{Status: status, Message: message}
}

// AdminService 管理员服务
type AdminService struct {
	db *gorm.DB
}

// NewAdminService 创建管理员服务
func NewAdminService(db *gorm.DB) *AdminService {
	return &AdminService{db: db}
}

// ListUsers 获取用户列表
func (s *AdminService) ListUsers(page, pageSize int, filter UserListFilter) (*UserListResult, error) {
	var users []models.User
	var total int64

	dbQuery := s.db.Model(&models.User{}).Where("role != ?", "admin")
	if filter.Query != "" {
		likeQuery := "%" + filter.Query + "%"
		dbQuery = dbQuery.Where("username LIKE ? OR uuid LIKE ? OR display_name LIKE ? OR nickname LIKE ?", likeQuery, likeQuery, likeQuery, likeQuery)
	}
	if filter.Source != "" {
		dbQuery = dbQuery.Where("auth_source = ?", filter.Source)
	}
	if filter.CreatedVia != "" {
		dbQuery = dbQuery.Where("created_via = ?", filter.CreatedVia)
	}

	// 一次性读取认证配置，用 SQL 条件表达 identity_state 过滤（避免全表拉取后内存过滤）
	policy := loadIdentityPolicy()
	if filter.IdentityState != "" {
		cond := policy.filterCondition(filter.IdentityState)
		dbQuery = dbQuery.Where(cond.sql, cond.args...)
	}

	dbQuery.Count(&total)

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	if err := dbQuery.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&users).Error; err != nil {
		utils.Error("获取用户列表失败", utils.Int("page", page), utils.Int("page_size", pageSize), utils.Err(err))
		return nil, fmt.Errorf("获取用户列表失败: %w", err)
	}

	items := make([]UserItem, len(users))
	perUserQuota := appconfig.GlobalConfig.Storage.Private.Quota.PerUserQuota

	// 一次性聚合本页所有用户的私有存储用量，避免每用户一条查询（N+1）
	usageByOwner := make(map[string]int64)
	// 一次性批量查询本页用户的认证绑定，避免逐行查库（N+1）
	bindingsByUser := make(map[uint][]models.AuthRecord)
	if len(users) > 0 {
		uuids := make([]any, len(users))
		userIDs := make([]uint, len(users))
		for i, u := range users {
			uuids[i] = u.UUID
			userIDs[i] = u.ID
		}
		var usages []struct {
			OwnerID string
			Sum     int64
		}
		if err := s.db.Model(&models.FileRecordPrivate{}).
			Select("owner_id, COALESCE(SUM(file_size), 0) AS sum").
			Where("owner_id IN ? AND status = ? AND is_dir = ?", uuids, models.FileStatusActive, false).
			Group("owner_id").
			Scan(&usages).Error; err != nil {
			utils.Error("查询用户存储用量失败", utils.Err(err))
		}
		for _, u := range usages {
			usageByOwner[u.OwnerID] = u.Sum
		}

		var records []models.AuthRecord
		if err := s.db.Where("user_id IN ?", userIDs).Order("id ASC").Find(&records).Error; err != nil {
			utils.Warn("查询用户认证绑定失败", utils.Err(err))
		} else {
			for _, r := range records {
				bindingsByUser[r.UserID] = append(bindingsByUser[r.UserID], r)
			}
		}
	}

	for i, u := range users {
		binding, hasBinding := pickUserBinding(bindingsByUser[u.ID], u.AuthSource)
		items[i] = UserItem{
			UUID:          u.UUID,
			Username:      u.Username,
			DisplayName:   u.DisplayName,
			Nickname:      u.Nickname,
			Role:          u.Role,
			Disabled:      u.Disabled,
			AuthSource:    u.AuthSource,
			CreatedVia:    u.CreatedVia,
			IdentityState: policy.stateFor(u.AuthSource, binding.ProviderName, hasBinding, u.PasswordHash == ""),
			ProviderName:  binding.ProviderName,
			ExternalID:    binding.ExternalID,
			CreatedAt:     u.CreatedAt,
			UpdatedAt:     u.UpdatedAt,
			UsedStorage:   usageByOwner[u.UUID],
			Quota:         perUserQuota,
		}
	}

	utils.Info("获取用户列表成功", utils.Int("page", page), utils.Int("page_size", pageSize), utils.Int64("total", total))
	return &UserListResult{Users: items, Total: total}, nil
}

// pickUserBinding 从用户的全部绑定中选出用于展示的一条：
// 优先与 auth_source 类型一致的绑定，否则取 id 最小的一条（调用方已按 id 升序）。
func pickUserBinding(records []models.AuthRecord, authSource string) (models.AuthRecord, bool) {
	if len(records) == 0 {
		return models.AuthRecord{}, false
	}
	for _, r := range records {
		if r.AuthType == authSource {
			return r, true
		}
	}
	return records[0], true
}

// identityPolicy 身份状态派生所需的认证源可用性与 provider 标识（每次列表读取一次配置）
type identityPolicy struct {
	ldapReady      bool
	oauth2Ready    bool
	oauth2Provider string
}

// loadIdentityPolicy 读取一次配置，计算认证源可用性与 provider 稳定标识。
func loadIdentityPolicy() identityPolicy {
	cfg := appconfig.GetConfig()
	ldapCfg := cfg.Auth.LDAP
	oauth2Cfg := cfg.Auth.OAuth2
	return identityPolicy{
		// 与 LDAPService.IsEnabled 保持一致：地址/BaseDN 缺失视为配置不全
		ldapReady: ldapCfg.Enabled && ldapCfg.Host != "" && ldapCfg.BaseDN != "",
		// 与 OAuth2Service.IsEnabled 保持一致：客户端凭据/端点缺失视为配置不全
		oauth2Ready:    oauth2Cfg.Enabled && oauth2Cfg.ClientID != "" && oauth2Cfg.ClientSecret != "" && (oauth2Cfg.AuthURL != "" || oauth2Cfg.Issuer != ""),
		oauth2Provider: oauth2ProviderKey(oauth2Cfg),
	}
}

// oauth2ProviderKey 计算 OAuth2 绑定的 provider 稳定标识（oauth2:<provider_id>）。
func oauth2ProviderKey(cfg appconfig.OAuth2Config) string {
	return "oauth2:" + strings.TrimSpace(cfg.ProviderID)
}

// expectedProvider 返回指定认证源期望的 provider 标识（本地账号无绑定，返回空）
func (p identityPolicy) expectedProvider(authSource string) string {
	switch authSource {
	case models.AuthSourceLDAP:
		return models.AuthSourceLDAP
	case models.AuthSourceOAuth2:
		return p.oauth2Provider
	}
	return ""
}

// sourceReady 判断认证源当前是否可用（配置完整且已启用）；local 恒可用
func (p identityPolicy) sourceReady(authSource string) bool {
	switch authSource {
	case models.AuthSourceLDAP:
		return p.ldapReady
	case models.AuthSourceOAuth2:
		return p.oauth2Ready
	}
	return true
}

// stateFor 派生 identity_state，优先级：inconsistent > provider_mismatch > orphaned > ok。
func (p identityPolicy) stateFor(authSource, providerName string, hasBinding, passwordEmpty bool) string {
	switch authSource {
	case models.AuthSourceLDAP:
		if !hasBinding {
			return models.IdentityStateInconsistent
		}
	case models.AuthSourceOAuth2:
		// 外部账号必须无本地密码（不变量 4）：有绑定且密码为空才可能正常
		if !hasBinding || !passwordEmpty {
			return models.IdentityStateInconsistent
		}
	default: // local（含历史空值）
		if hasBinding || passwordEmpty {
			return models.IdentityStateInconsistent
		}
	}
	if hasBinding && providerName != p.expectedProvider(authSource) {
		return models.IdentityStateProviderMismatch
	}
	if !p.sourceReady(authSource) {
		return models.IdentityStateOrphaned
	}
	return models.IdentityStateOK
}

// identitySQLCondition SQL 条件片段及其参数
type identitySQLCondition struct {
	sql  string
	args []any
}

func (c identitySQLCondition) and(other identitySQLCondition) identitySQLCondition {
	return identitySQLCondition{
		sql:  "(" + c.sql + ") AND (" + other.sql + ")",
		args: append(append([]any{}, c.args...), other.args...),
	}
}

func (c identitySQLCondition) or(other identitySQLCondition) identitySQLCondition {
	return identitySQLCondition{
		sql:  "(" + c.sql + ") OR (" + other.sql + ")",
		args: append(append([]any{}, c.args...), other.args...),
	}
}

func (c identitySQLCondition) not() identitySQLCondition {
	return identitySQLCondition{sql: "NOT (" + c.sql + ")", args: c.args}
}

// filterCondition 把 identity_state 转换成 SQL 条件（与 stateFor 语义一致，
// 优先级 inconsistent > provider_mismatch > orphaned > ok 通过 NOT 组合表达）。
func (p identityPolicy) filterCondition(state string) identitySQLCondition {
	anyBinding := identitySQLCondition{sql: "EXISTS (SELECT 1 FROM auth_records r WHERE r.user_id = users.id)"}
	ldapBinding := identitySQLCondition{sql: "EXISTS (SELECT 1 FROM auth_records r WHERE r.user_id = users.id AND r.auth_type = 'ldap')"}
	ldapProviderMatch := identitySQLCondition{sql: "EXISTS (SELECT 1 FROM auth_records r WHERE r.user_id = users.id AND r.auth_type = 'ldap' AND r.provider_name = 'ldap')"}
	oauth2ProviderMatch := identitySQLCondition{
		sql:  "EXISTS (SELECT 1 FROM auth_records r WHERE r.user_id = users.id AND r.auth_type = 'oauth2' AND r.provider_name = ?)",
		args: []any{p.oauth2Provider},
	}

	inconsistent := identitySQLCondition{sql: "users.auth_source = 'local' AND (" + anyBinding.sql + " OR users.password_hash = '')"}.
		or(identitySQLCondition{sql: "users.auth_source = 'ldap' AND NOT " + ldapBinding.sql}).
		or(identitySQLCondition{sql: "users.auth_source = 'oauth2' AND (NOT " + anyBinding.sql + " OR users.password_hash <> '')"})

	mismatch := identitySQLCondition{sql: "users.auth_source = 'ldap' AND NOT " + ldapProviderMatch.sql}.
		or(identitySQLCondition{
			sql:  "users.auth_source = 'oauth2' AND " + anyBinding.sql + " AND NOT " + oauth2ProviderMatch.sql,
			args: oauth2ProviderMatch.args,
		})

	orphaned := identitySQLCondition{sql: "1 = 0"}
	if !p.ldapReady {
		orphaned = orphaned.or(identitySQLCondition{sql: "users.auth_source = 'ldap'"})
	}
	if !p.oauth2Ready {
		orphaned = orphaned.or(identitySQLCondition{sql: "users.auth_source = 'oauth2'"})
	}

	switch state {
	case models.IdentityStateInconsistent:
		return inconsistent
	case models.IdentityStateProviderMismatch:
		return inconsistent.not().and(mismatch)
	case models.IdentityStateOrphaned:
		return inconsistent.not().and(mismatch.not()).and(orphaned)
	default: // ok
		return inconsistent.not().and(mismatch.not()).and(orphaned.not())
	}
}

// CreateUser 管理员主动创建用户
func (s *AdminService) CreateUser(callerUUID, username, displayName, password, role string) (*models.User, error) {
	// 校验调用者是管理员
	var caller models.User
	if err := s.db.Where("uuid = ? AND role = ?", callerUUID, "admin").First(&caller).Error; err != nil {
		utils.Warn("创建用户失败: 无权操作", utils.String("caller_uuid", callerUUID))
		return nil, fmt.Errorf("无权操作：仅管理员可创建用户")
	}

	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 32 {
		return nil, fmt.Errorf("用户名长度需在3到32个字符之间")
	}
	displayName = strings.TrimSpace(displayName)
	if len([]rune(displayName)) > 64 {
		return nil, fmt.Errorf("姓名过长")
	}
	if len(password) < 8 || len(password) > 128 {
		return nil, fmt.Errorf("密码长度需在8到128个字符之间")
	}
	switch role {
	case "", "user":
		role = "user"
	case "admin":
		role = "admin"
	default:
		return nil, fmt.Errorf("角色不合法")
	}

	for _, reserved := range appconfig.GetReservedUsernames() {
		if strings.EqualFold(username, reserved) {
			return nil, fmt.Errorf("该用户名不允许使用")
		}
	}

	var existingUser models.User
	if err := s.db.Where("username = ?", username).First(&existingUser).Error; err == nil {
		return nil, fmt.Errorf("用户名已存在")
	}

	uuid, err := newUserUUID()
	if err != nil {
		utils.Error("创建用户失败: 生成UUID错误", utils.Err(err))
		return nil, fmt.Errorf("生成UUID失败: %w", err)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		utils.Error("创建用户失败: 密码加密错误", utils.Err(err))
		return nil, fmt.Errorf("密码加密失败: %w", err)
	}

	user := &models.User{
		UUID:         uuid,
		Username:     username,
		DisplayName:  displayName,
		PasswordHash: string(hashedPassword),
		Role:         role,
	}

	if err := s.db.Create(user).Error; err != nil {
		utils.Error("创建用户失败: 保存用户错误", utils.Err(err))
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}

	utils.Info("管理员创建用户成功", utils.String("admin", caller.Username), utils.String("new_user", username), utils.String("role", role))
	return user, nil
}

// newUserUUID 生成用户 UUID v4
func newUserUUID() (string, error) {
	uuid := make([]byte, 16)
	if _, err := rand.Read(uuid); err != nil {
		return "", err
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:]), nil
}

// ResetPassword 重置用户密码
func (s *AdminService) ResetPassword(callerUUID, targetUUID, newPassword string) error {
	// 校验调用者是管理员
	var caller models.User
	if err := s.db.Where("uuid = ? AND role = ?", callerUUID, "admin").First(&caller).Error; err != nil {
		utils.Warn("重置密码失败: 无权操作", utils.String("caller_uuid", callerUUID), utils.String("target_uuid", targetUUID))
		return fmt.Errorf("无权操作：仅管理员可重置密码")
	}

	var user models.User
	if err := s.db.Where("uuid = ?", targetUUID).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.Warn("重置密码失败: 用户不存在", utils.String("target_uuid", targetUUID))
			return fmt.Errorf("用户不存在")
		}
		utils.Error("重置密码失败: 查询用户错误", utils.String("target_uuid", targetUUID), utils.Err(err))
		return fmt.Errorf("查找用户失败: %w", err)
	}

	// 外部认证账号不持有本地密码，必须显式转为本地账号后才能重置
	if user.AuthSource != "" && user.AuthSource != models.AuthSourceLocal {
		utils.Warn("重置密码失败: 外部认证账号不允许设置本地密码",
			utils.String("target_uuid", targetUUID), utils.String("auth_source", user.AuthSource))
		return newUserIdentityError(http.StatusBadRequest, "请先将该用户转为本地账号")
	}

	// 管理员设密也需遵循最少 8 位的新密码强度要求
	if len(newPassword) < 8 || len(newPassword) > 128 {
		return fmt.Errorf("新密码长度需在8到128个字符之间")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		utils.Error("重置密码失败: 密码加密错误", utils.String("target_uuid", targetUUID), utils.Err(err))
		return fmt.Errorf("密码加密失败: %w", err)
	}

	user.PasswordHash = string(hashedPassword)
	if err := s.db.Save(&user).Error; err != nil {
		utils.Error("重置密码失败: 保存用户错误", utils.String("target_uuid", targetUUID), utils.Err(err))
		return fmt.Errorf("更新密码失败: %w", err)
	}

	utils.Info("管理员重置用户密码成功", utils.String("admin", caller.Username), utils.String("target_user", user.Username))
	return nil
}

// BindExternalIdentity 将外部身份绑定到指定用户：写绑定、切换 auth_source、清空本地密码。
// provider 由当前启用的认证配置决定，不接受客户端自定义。
func (s *AdminService) BindExternalIdentity(callerUUID, targetUUID, authType, externalID, externalName string) error {
	var caller models.User
	if err := s.db.Where("uuid = ? AND role = ?", callerUUID, "admin").First(&caller).Error; err != nil {
		utils.Warn("绑定外部身份失败: 无权操作", utils.String("caller_uuid", callerUUID))
		return newUserIdentityError(http.StatusForbidden, "无权操作：仅管理员可绑定外部身份")
	}

	authType = strings.TrimSpace(authType)
	externalID = strings.TrimSpace(externalID)
	externalName = strings.TrimSpace(externalName)
	if authType != models.AuthSourceLDAP && authType != models.AuthSourceOAuth2 {
		return newUserIdentityError(http.StatusBadRequest, "认证类型无效，可选值：ldap、oauth2")
	}
	if externalID == "" {
		return newUserIdentityError(http.StatusBadRequest, "外部身份标识不能为空")
	}
	if len(externalID) > 256 {
		return newUserIdentityError(http.StatusBadRequest, "外部身份标识过长")
	}
	if len([]rune(externalName)) > 128 {
		return newUserIdentityError(http.StatusBadRequest, "外部身份显示名过长")
	}

	// provider 由服务端按当前配置计算
	policy := loadIdentityPolicy()
	if !policy.sourceReady(authType) {
		return newUserIdentityError(http.StatusBadRequest, "认证源未启用或配置不完整")
	}
	provider := policy.expectedProvider(authType)

	var target models.User
	if err := s.db.Where("uuid = ?", targetUUID).First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return newUserIdentityError(http.StatusBadRequest, "用户不存在")
		}
		utils.Error("绑定外部身份失败: 查询用户错误", utils.String("target_uuid", targetUUID), utils.Err(err))
		return fmt.Errorf("查找用户失败: %w", err)
	}

	originalSource := target.AuthSource
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 同一外部身份全局唯一：已绑定其他用户时拒绝
		var existing models.AuthRecord
		err := tx.Where("auth_type = ? AND provider_name = ? AND external_id = ?", authType, provider, externalID).
			First(&existing).Error
		switch {
		case err == nil:
			if existing.UserID == target.ID {
				return newUserIdentityError(http.StatusConflict, "该用户已绑定该外部身份")
			}
			return newUserIdentityError(http.StatusConflict, "该外部身份已绑定其他用户")
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return fmt.Errorf("检查外部身份绑定失败: %w", err)
		}

		// 不支持裸解绑/换绑：目标用户已有绑定时需先转为本地账号
		var bindingCount int64
		if err := tx.Model(&models.AuthRecord{}).Where("user_id = ?", target.ID).Count(&bindingCount).Error; err != nil {
			return fmt.Errorf("检查用户绑定失败: %w", err)
		}
		if bindingCount > 0 {
			return newUserIdentityError(http.StatusConflict, "该用户已绑定其他认证来源，请先转为本地账号")
		}

		record := &models.AuthRecord{
			UserID:       target.ID,
			AuthType:     authType,
			ExternalID:   externalID,
			ExternalName: externalName,
			ProviderName: provider,
		}
		if err := tx.Create(record).Error; err != nil {
			// 并发写入时由唯一索引兜底
			if isUniqueViolation(err) {
				return newUserIdentityError(http.StatusConflict, "该外部身份已绑定其他用户")
			}
			return fmt.Errorf("保存外部身份绑定失败: %w", err)
		}

		if err := tx.Model(&models.User{}).Where("id = ?", target.ID).Updates(map[string]interface{}{
			"auth_source":   authType,
			"password_hash": "",
		}).Error; err != nil {
			return fmt.Errorf("更新用户认证来源失败: %w", err)
		}
		return nil
	})
	if err != nil {
		utils.Warn("绑定外部身份失败", utils.String("target_uuid", targetUUID), utils.Err(err))
		return err
	}

	utils.Info("管理员绑定外部身份成功",
		utils.String("admin", caller.Username),
		utils.String("target_user", target.Username),
		utils.String("original_auth_source", originalSource),
		utils.String("provider", provider))
	return nil
}

// ConvertUserToLocal 将外部认证账号转为本地账号：写入新密码、auth_source=local、删除全部绑定。
func (s *AdminService) ConvertUserToLocal(callerUUID, targetUUID, newPassword string) error {
	var caller models.User
	if err := s.db.Where("uuid = ? AND role = ?", callerUUID, "admin").First(&caller).Error; err != nil {
		utils.Warn("转为本地账号失败: 无权操作", utils.String("caller_uuid", callerUUID))
		return newUserIdentityError(http.StatusForbidden, "无权操作：仅管理员可执行该操作")
	}

	var user models.User
	if err := s.db.Where("uuid = ?", targetUUID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return newUserIdentityError(http.StatusBadRequest, "用户不存在")
		}
		utils.Error("转为本地账号失败: 查询用户错误", utils.String("target_uuid", targetUUID), utils.Err(err))
		return fmt.Errorf("查找用户失败: %w", err)
	}
	if user.AuthSource == "" || user.AuthSource == models.AuthSourceLocal {
		return newUserIdentityError(http.StatusBadRequest, "该用户已是本地账号")
	}
	if len(newPassword) < 8 || len(newPassword) > 128 {
		return newUserIdentityError(http.StatusBadRequest, "新密码长度需在8到128个字符之间")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		utils.Error("转为本地账号失败: 密码加密错误", utils.String("target_uuid", targetUUID), utils.Err(err))
		return fmt.Errorf("密码加密失败: %w", err)
	}

	originalSource := user.AuthSource
	var providers []string
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var records []models.AuthRecord
		if err := tx.Where("user_id = ?", user.ID).Find(&records).Error; err != nil {
			return fmt.Errorf("查询用户绑定失败: %w", err)
		}
		for _, r := range records {
			providers = append(providers, r.ProviderName)
		}
		if err := tx.Where("user_id = ?", user.ID).Delete(&models.AuthRecord{}).Error; err != nil {
			return fmt.Errorf("删除用户认证绑定失败: %w", err)
		}
		if err := tx.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]interface{}{
			"password_hash": string(hashedPassword),
			"auth_source":   models.AuthSourceLocal,
		}).Error; err != nil {
			return fmt.Errorf("更新用户认证来源失败: %w", err)
		}
		return nil
	})
	if err != nil {
		utils.Warn("转为本地账号失败", utils.String("target_uuid", targetUUID), utils.Err(err))
		return err
	}

	utils.Info("管理员将用户转为本地账号成功",
		utils.String("admin", caller.Username),
		utils.String("target_user", user.Username),
		utils.String("original_auth_source", originalSource),
		utils.String("provider", strings.Join(providers, ",")))
	return nil
}

// isUniqueViolation 判断数据库错误是否为唯一索引冲突（兼容 SQLite/MySQL 驱动文案）
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") ||
		strings.Contains(msg, "duplicate entry") ||
		strings.Contains(msg, "unique violation")
}

// ToggleUserStatus 启用/禁用用户
func (s *AdminService) ToggleUserStatus(uuid string, disabled bool) error {
	var user models.User
	if err := s.db.Where("uuid = ?", uuid).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.Warn("切换用户状态失败: 用户不存在", utils.String("uuid", uuid))
			return fmt.Errorf("用户不存在")
		}
		utils.Error("切换用户状态失败: 查询用户错误", utils.String("uuid", uuid), utils.Err(err))
		return fmt.Errorf("查找用户失败: %w", err)
	}

	if user.Role == "admin" {
		utils.Warn("切换用户状态失败: 不能禁用管理员账号", utils.String("uuid", uuid))
		return fmt.Errorf("不能禁用管理员账号")
	}

	user.Disabled = disabled
	if err := s.db.Save(&user).Error; err != nil {
		utils.Error("切换用户状态失败: 保存用户错误", utils.String("uuid", uuid), utils.Err(err))
		return fmt.Errorf("更新用户状态失败: %w", err)
	}

	action := "启用"
	if disabled {
		action = "禁用"
	}
	utils.Info("切换用户状态成功", utils.String("user", user.Username), utils.String("action", action))
	return nil
}

// DeleteUser 删除用户
func (s *AdminService) DeleteUser(uuid string) error {
	var user models.User
	if err := s.db.Where("uuid = ?", uuid).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			utils.Warn("删除用户失败: 用户不存在", utils.String("uuid", uuid))
			return fmt.Errorf("用户不存在")
		}
		utils.Error("删除用户失败: 查询用户错误", utils.String("uuid", uuid), utils.Err(err))
		return fmt.Errorf("查找用户失败: %w", err)
	}

	if user.Role == "admin" {
		utils.Warn("删除用户失败: 不能删除管理员账号", utils.String("uuid", uuid))
		return fmt.Errorf("不能删除管理员账号")
	}

	username := user.Username
	// 先清理该用户的私有/临时文件与关联数据，再删除用户行
	if err := CleanupUserAssets(s.db, &user); err != nil {
		utils.Error("删除用户失败: 清理用户文件失败", utils.String("uuid", uuid), utils.Err(err))
		return fmt.Errorf("清理用户文件失败: %w", err)
	}
	if err := s.db.Delete(&user).Error; err != nil {
		utils.Error("删除用户失败: 数据库错误", utils.String("uuid", uuid), utils.Err(err))
		return fmt.Errorf("删除用户失败: %w", err)
	}

	utils.Info("删除用户成功", utils.String("username", username), utils.String("uuid", uuid))
	return nil
}

// ListSessions 获取会话列表
func (s *AdminService) ListSessions(page, pageSize int) (*SessionListResult, error) {
	var sessions []models.UploadSession
	var total int64

	s.db.Model(&models.UploadSession{}).Count(&total)

	// 全局聚合统计（与分页无关）：总会话数、僵尸会话数、已占空间
	stats := SessionStats{TotalSessions: total}
	var zombieCount int64
	s.db.Model(&models.UploadSession{}).Where(
		"status IN ?", []string{
			string(models.UploadStatusPending),
			string(models.UploadStatusInProgress),
			string(models.UploadStatusExpired),
		},
	).Count(&zombieCount)
	stats.ZombieSessions = zombieCount
	if err := s.db.Model(&models.UploadedChunk{}).
		Select("COALESCE(SUM(chunk_size), 0)").
		Scan(&stats.TotalSize).Error; err != nil {
		utils.Error("统计会话占用空间失败", utils.Err(err))
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	if err := s.db.Order("created_at DESC").Preload("Chunks").Offset(offset).Limit(pageSize).Find(&sessions).Error; err != nil {
		utils.Error("获取会话列表失败", utils.Int("page", page), utils.Int("page_size", pageSize), utils.Err(err))
		return nil, fmt.Errorf("获取会话列表失败: %w", err)
	}

	items := make([]SessionItem, len(sessions))
	for i, sess := range sessions {
		var uploadedSize int64
		for _, chunk := range sess.Chunks {
			uploadedSize += chunk.ChunkSize
		}

		items[i] = SessionItem{
			ID:           sess.ID,
			FileName:     sess.FileName,
			FileSize:     sess.FileSize,
			UploadedSize: uploadedSize,
			Status:       string(sess.Status),
			TargetPath:   sess.TargetPath,
			TargetRoot:   sess.TargetRoot,
			CreatedAt:    sess.CreatedAt.Format("2006-01-02 15:04:05"),
			ExpiredAt:    sess.ExpiredAt.Format("2006-01-02 15:04:05"),
		}
	}

	utils.Info("获取会话列表成功", utils.Int("page", page), utils.Int("page_size", pageSize), utils.Int64("total", total))
	return &SessionListResult{Sessions: items, Total: total, Stats: stats}, nil
}

// CleanupSessions 清理指定的会话（含分片文件、数据库记录及残留的落盘上传中文件）
func (s *AdminService) CleanupSessions(sessionIDs []uint) (int, error) {
	cleanedCount := 0
	var errs []error
	for _, sessionID := range sessionIDs {
		var session models.UploadSession
		if err := s.db.First(&session, sessionID).Error; err != nil {
			continue
		}

		// 清理落盘的 uploadin 残留文件（按 targetType 推导真实路径）
		if uploadingPath, perr := resolveUploadingPath(&session); perr == nil {
			if rmErr := os.Remove(uploadingPath); rmErr != nil && !os.IsNotExist(rmErr) {
				errs = append(errs, fmt.Errorf("删除上传中残留文件失败(session=%d): %w", sessionID, rmErr))
			}
		}

		// 删除分片记录
		if err := s.db.Where("session_id = ?", sessionID).Delete(&models.UploadedChunk{}).Error; err != nil {
			errs = append(errs, fmt.Errorf("删除分片记录失败(session=%d): %w", sessionID, err))
		}

		// 删除会话记录
		if err := s.db.Delete(&models.UploadSession{}, sessionID).Error; err != nil {
			errs = append(errs, fmt.Errorf("删除会话记录失败(session=%d): %w", sessionID, err))
		}

		cleanedCount++
	}

	if len(errs) > 0 {
		utils.Error("清理会话完成（部分失败）",
			utils.Int("cleaned", cleanedCount),
			utils.Int("errors", len(errs)),
			utils.Any("error_details", errs))
		return cleanedCount, fmt.Errorf("清理会话时发生 %d 个错误: %v", len(errs), errs)
	}

	utils.Info("清理会话完成", utils.Int("count", cleanedCount), utils.Int("session_ids", len(sessionIDs)))
	return cleanedCount, nil
}

// resolveUploadingPath 按会话的上传类型推导 uploadin 残留文件的落盘路径。
// 与各存储策略的命名规则一致：公开/私有为目标文件同目录下的 `.{basename}.uploading`，
// 临时为 `.{码哈希}.uploading`。路径推导失败时返回 error（由调用方决定是否忽略）。
func resolveUploadingPath(session *models.UploadSession) (string, error) {
	switch session.TargetType {
	case models.TargetTypePrivate:
		basePath := appconfig.GlobalConfig.Storage.Private.Path
		if basePath == "" {
			return "", fmt.Errorf("私有存储未配置")
		}
		userDir := filepath.Join(basePath, "users", session.UserID)
		dir := joinUnderRoot(userDir, session.TargetPath)
		if dir == "" {
			return "", fmt.Errorf("私有路径越界")
		}
		return filepath.Join(dir, "."+session.FileName+".uploading"), nil
	case models.TargetTypeTemp:
		basePath := appconfig.GlobalConfig.Storage.Temp.Path
		if basePath == "" || session.Code == "" {
			return "", fmt.Errorf("临时存储未配置或缺少访问码")
		}
		dir := joinUnderRoot(basePath, session.TargetPath)
		if dir == "" {
			return "", fmt.Errorf("临时路径越界")
		}
		hash := xxh3.Hash([]byte(session.Code + "fuzhan-secret"))
		safeFilename := fmt.Sprintf("%016x", hash)
		return filepath.Join(dir, "."+safeFilename+".uploading"), nil
	default: // 公开（包含历史无 target_type 的遗留会话）
		_, uploadingPath, perr := (PublicStorage{}).Paths(session)
		if perr != nil {
			return "", perr
		}
		return uploadingPath, nil
	}
}

// joinUnderRoot 将会话的相对子目录安全拼接到根目录下，返回空字符串表示越界/非法。
// 拒绝绝对路径、上级跳转及包含路径分隔符陷阱的相对片段。
func joinUnderRoot(root, rel string) string {
	rel = strings.TrimSpace(rel)
	rel = strings.Trim(rel, `/\`)
	cleaned := filepath.Clean(rel)
	if cleaned == "." || cleaned == "" {
		return root
	}
	if filepath.IsAbs(cleaned) || cleaned == ".." ||
		strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) ||
		strings.Contains(cleaned, ".."+string(os.PathSeparator)) {
		return ""
	}
	joined := filepath.Join(root, cleaned)
	absRoot, _ := filepath.Abs(root)
	absJoined, _ := filepath.Abs(joined)
	if absRoot == "" || absJoined == "" ||
		(absJoined != absRoot && !strings.HasPrefix(absJoined, absRoot+string(os.PathSeparator))) {
		return ""
	}
	return joined
}

// CleanupAllZombieSessions 后台重新查询全部僵尸（过期/待处理/上传中）会话并清理。
// 与按 ID 清理不同，由后端决定具体清理哪些，前端无需关心分页。
func (s *AdminService) CleanupAllZombieSessions() (int, error) {
	var ids []uint
	if err := s.db.Model(&models.UploadSession{}).
		Where("status IN ?", []string{
			string(models.UploadStatusPending),
			string(models.UploadStatusInProgress),
			string(models.UploadStatusExpired),
		}).
		Pluck("id", &ids).Error; err != nil {
		utils.Error("查询僵尸会话失败", utils.Err(err))
		return 0, fmt.Errorf("查询僵尸会话失败: %w", err)
	}
	if len(ids) == 0 {
		utils.Info("一键清理：没有需要清理的僵尸会话")
		return 0, nil
	}

	cleaned, err := s.CleanupSessions(ids)
	if err != nil {
		utils.Error("一键清理僵尸会话失败",
			utils.Int("matched", len(ids)),
			utils.Int("cleaned", cleaned),
			utils.Err(err))
		return cleaned, err
	}
	return cleaned, nil
}
