package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// ldapCredentialVerifier LDAP 凭据校验能力（仅验证目录凭据，不触发 JIT 建号）。
// 抽成接口便于测试替换为假实现。
type ldapCredentialVerifier interface {
	IsEnabled() bool
	VerifyCredentials(username, password string) (dn, displayName string, err error)
}

// 身份确认相关错误文案（对外保持通用，不泄露认证源细节）
const (
	identityConfirmExpiredMessage = "确认已过期，请重新登录"
	identityConfirmPasswordError  = "本地账号密码错误"
	identityConfirmAdminMessage   = "管理员账号请联系管理员处理"
)

// errIdentityBindConflict 绑定事务中的可预期冲突（用状态码 + 文案返回）
var errIdentityBindConflict = errors.New("身份绑定冲突")

// issueIdentityConfirmTicket 在 OAuth2 同名冲突且允许自助处理时签发一次性票据并跳转确认页。
// 管理员账号、已禁用账号、非本地归属账号不签发票据，保持原有拒绝语义。
// 返回 true 表示已接管响应。
func (h *Handler) issueIdentityConfirmTicket(c *gin.Context, conflict *services.IdentityConflictError) bool {
	if conflict == nil || h.identityConfirm == nil {
		return false
	}
	if conflict.AuthType != models.AuthSourceOAuth2 {
		return false
	}
	if conflict.LocalDisabled || conflict.LocalAuthSource != models.AuthSourceLocal || conflict.LocalRole == "admin" {
		return false
	}
	if appconfig.GetConfig().Auth.EffectiveIdentityConflictMode() != appconfig.IdentityConflictModeSelfService {
		return false
	}

	providerID := strings.TrimSpace(conflict.ProviderID)
	if providerID == "" {
		providerID = models.AuthSourceOAuth2
	}
	externalName := conflict.ExternalName
	if externalName == "" {
		externalName = conflict.DisplayName
	}
	externalUsername := conflict.ExternalUsername
	if externalUsername == "" {
		externalUsername = conflict.Username
	}
	token, err := h.identityConfirm.Issue(services.IdentityConfirmTicket{
		UserID:           conflict.LocalUserID,
		Username:         conflict.Username,
		Role:             conflict.LocalRole,
		AuthType:         models.AuthSourceOAuth2,
		ProviderID:       providerID,
		ProviderName:     conflict.ProviderName,
		ExternalID:       conflict.ExternalID,
		ExternalUsername: externalUsername,
		ExternalName:     externalName,
		DisplayName:      conflict.DisplayName,
	})
	if err != nil {
		utils.Warn("签发身份确认票据失败", utils.String("username", conflict.Username), utils.Err(err))
		return false
	}

	middleware.SetIdentityConfirmCookie(c, token)
	h.recordLoginLog(c, "", conflict.Username, services.LoginMethodOAuth2, "failed", "检测到同名本地账号，等待用户确认")
	// 票据只放 HttpOnly Cookie，不放 URL，避免残留在浏览器历史/脚本可读的 location。
	c.Redirect(http.StatusFound, "/#/oauth2/callback?conflict=1")
	return true
}

// identityConfirmTicket 读取当前请求的确认票据；无效时清理 Cookie 并返回 false。
func (h *Handler) identityConfirmTicket(c *gin.Context) (string, services.IdentityConfirmTicket, bool) {
	if h.identityConfirm == nil {
		return "", services.IdentityConfirmTicket{}, false
	}
	token := middleware.IdentityConfirmTokenFromCookie(c)
	if token == "" {
		return "", services.IdentityConfirmTicket{}, false
	}
	ticket, ok := h.identityConfirm.Lookup(token)
	if !ok {
		middleware.ClearIdentityConfirmCookie(c)
		return "", services.IdentityConfirmTicket{}, false
	}
	return token, ticket, true
}

// IdentityConfirmStatus 查询待确认的外部身份（匿名，凭 HttpOnly 票据 Cookie）。
func (h *Handler) IdentityConfirmStatus(c *gin.Context) {
	_, ticket, ok := h.identityConfirmTicket(c)
	if !ok {
		response.HandleSuccess(c, http.StatusOK, "", gin.H{"pending": false})
		return
	}
	// provider_name 用于页面展示：优先使用配置里的提供方名称，缺省回退 provider 标识；
	// 绑定所需的 provider key（oauth2:<provider_id>）只保留在票据内部。
	providerDisplay := strings.TrimSpace(appconfig.GetConfig().Auth.OAuth2.ProviderName)
	if providerDisplay == "" {
		providerDisplay = ticket.ProviderID
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"pending":       true,
		"username":      ticket.Username,
		"auth_type":     ticket.AuthType,
		"provider_id":   ticket.ProviderID,
		"provider_name": providerDisplay,
		"external_name": ticket.ExternalName,
		"expires_at":    ticket.ExpiresAt.UTC().Format(time.RFC3339),
	})
}

// BindIdentityConfirm 输入本地账号密码完成确认：同时证明本地与外部身份，绑定后建立会话。
func (h *Handler) BindIdentityConfirm(c *gin.Context) {
	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}

	token, _, ok := h.identityConfirmTicket(c)
	if !ok {
		response.HandleBadRequest(c, identityConfirmExpiredMessage, nil)
		return
	}
	// 票据签发后管理员可能已把处理方式切回 admin：确认入口需按当前配置复核。
	if appconfig.GetConfig().Auth.EffectiveIdentityConflictMode() != appconfig.IdentityConflictModeSelfService {
		middleware.ClearIdentityConfirmCookie(c)
		h.identityConfirm.Consume(token)
		response.HandleBadRequest(c, identityConfirmExpiredMessage, nil)
		return
	}
	// Begin 原子占用票据：并发提交只有第一个请求能进入，避免同一票据被重复使用。
	ticket, ok := h.identityConfirm.Begin(token)
	if !ok {
		response.HandleBadRequest(c, identityConfirmExpiredMessage, nil)
		return
	}

	user, err := h.loadConfirmTargetUser(ticket)
	if err != nil || user == nil {
		middleware.ClearIdentityConfirmCookie(c)
		h.identityConfirm.Consume(token)
		response.HandleBadRequest(c, identityConfirmExpiredMessage, nil)
		return
	}
	if user.Role == "admin" {
		middleware.ClearIdentityConfirmCookie(c)
		h.identityConfirm.Consume(token)
		response.HandleForbidden(c, identityConfirmAdminMessage)
		return
	}
	if user.Disabled || user.AuthSource != models.AuthSourceLocal || user.PasswordHash == "" {
		middleware.ClearIdentityConfirmCookie(c)
		h.identityConfirm.Consume(token)
		response.HandleBadRequest(c, identityConfirmExpiredMessage, nil)
		return
	}

	ip := utils.GetClientIP(c)
	if isLoginLockedByUsername(ip, user.Username) {
		h.identityConfirm.End(token)
		middleware.LogOperation(c, "auth.identity_confirm", user.Username, fmt.Errorf("确认被频率限制锁定"))
		recordAuthEvent(c, models.EventAuthLocked, "auth.identity_confirm", user.Username, models.SeverityHigh, "尝试过于频繁，确认被频率限制锁定")
		handleLoginBlocked(c)
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		registerLoginFailure(ip)
		registerLoginFailureByUsername(ip, user.Username)
		h.recordAuthFailure(c, "auth.identity_confirm", user.Username, false)
		h.recordLoginLog(c, "", user.Username, services.LoginMethodOAuth2, "failed", identityConfirmPasswordError)
		if _, valid := h.identityConfirm.RecordFailure(token); valid {
			h.identityConfirm.End(token)
		} else {
			middleware.ClearIdentityConfirmCookie(c)
		}
		response.HandleUnauthorized(c, identityConfirmPasswordError)
		return
	}

	status, message, berr := h.bindTicketToUser(ticket, user)
	if berr != nil {
		h.identityConfirm.End(token)
		utils.Warn("身份确认绑定失败",
			utils.String("username", user.Username), utils.Err(berr))
		if status != 0 {
			response.HandleCustomError(c, status, response.CodeBadRequest, message)
			return
		}
		response.HandleInternalServerError(c, "绑定失败，请稍后重试")
		return
	}

	resp, err := h.authService.LoginByUser(user)
	if err != nil {
		// 绑定已落库（本地密码已清空、认证来源已切换），票据不再可用；
		// 外部身份已绑定成功，用户重新走一次外部认证即可登录。
		utils.Warn("身份确认后登录失败", utils.String("username", user.Username), utils.Err(err))
		h.recordLoginLog(c, "", user.Username, services.LoginMethodOAuth2, "failed", err.Error())
		h.identityConfirm.Consume(token)
		middleware.ClearIdentityConfirmCookie(c)
		response.HandleUnauthorized(c, "身份已绑定成功，请重新使用外部认证登录")
		return
	}

	h.identityConfirm.Consume(token)
	middleware.ClearIdentityConfirmCookie(c)
	resetLoginFailures(ip)
	resetLoginFailuresByUsername(ip, user.Username)
	middleware.LogOperation(c, "auth.identity_confirm.bind", user.Username, nil)
	recordAuthSuccess(c, resp.Username)
	h.recordLoginLog(c, resp.UUID, resp.Username, services.LoginMethodOAuth2, "success", "")
	middleware.SetAuthCookie(c, resp.Token)
	response.HandleSuccess(c, http.StatusOK, "绑定成功", authSessionResponse(resp))
}

// CreateAccountIdentityConfirm 使用独立账号完成外部登录：
// 用户名 = 外部用户名 + @<provider_id 或 oauth2>，冲突时追加序号。
func (h *Handler) CreateAccountIdentityConfirm(c *gin.Context) {
	token, _, ok := h.identityConfirmTicket(c)
	if !ok {
		response.HandleBadRequest(c, identityConfirmExpiredMessage, nil)
		return
	}
	// 同 BindIdentityConfirm：按当前配置复核，并用 Begin 原子占用票据防并发重复建号。
	if appconfig.GetConfig().Auth.EffectiveIdentityConflictMode() != appconfig.IdentityConflictModeSelfService {
		middleware.ClearIdentityConfirmCookie(c)
		h.identityConfirm.Consume(token)
		response.HandleBadRequest(c, identityConfirmExpiredMessage, nil)
		return
	}
	ticket, ok := h.identityConfirm.Begin(token)
	if !ok {
		response.HandleBadRequest(c, identityConfirmExpiredMessage, nil)
		return
	}
	if ticket.Role == "admin" {
		middleware.ClearIdentityConfirmCookie(c)
		h.identityConfirm.Consume(token)
		response.HandleForbidden(c, identityConfirmAdminMessage)
		return
	}

	user, status, message, err := h.createOAuthIdentityAccount(ticket)
	if err != nil {
		utils.Warn("创建独立外部账号失败", utils.String("external_id", ticket.ExternalID), utils.Err(err))
		middleware.ClearIdentityConfirmCookie(c)
		h.identityConfirm.Consume(token)
		if status != 0 {
			response.HandleCustomError(c, status, response.CodeBadRequest, message)
			return
		}
		response.HandleInternalServerError(c, "创建账号失败，请稍后重试")
		return
	}

	resp, err := h.authService.LoginByUser(user)
	if err != nil {
		// 账号与绑定已落库，票据不再可用；用户重新走一次外部认证即可正常登录。
		utils.Warn("独立外部账号登录失败", utils.String("username", user.Username), utils.Err(err))
		h.recordLoginLog(c, "", user.Username, services.LoginMethodOAuth2, "failed", err.Error())
		h.identityConfirm.Consume(token)
		middleware.ClearIdentityConfirmCookie(c)
		response.HandleUnauthorized(c, "账号已创建，请重新使用外部认证登录")
		return
	}

	h.identityConfirm.Consume(token)
	middleware.ClearIdentityConfirmCookie(c)
	// 建号流程没有校验任何密码，不重置登录失败计数，避免被用来绕过本机/本账号的尝试锁定。
	middleware.LogOperation(c, "auth.identity_confirm.create_account", user.Username, nil)
	h.recordLoginLog(c, resp.UUID, resp.Username, services.LoginMethodOAuth2, "success", "")
	middleware.SetAuthCookie(c, resp.Token)
	response.HandleSuccess(c, http.StatusOK, "账号已创建并登录", authSessionResponse(resp))
}

// BindLDAPIdentity 个人设置自助绑定 LDAP：用当前登录用户名去目录校验密码，
// 成功后写绑定、切换 auth_source=ldap 并清空本地密码。
func (h *Handler) BindLDAPIdentity(c *gin.Context) {
	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}
	if !appconfig.GetConfig().Auth.LDAP.Enabled {
		response.HandleBadRequest(c, "LDAP 认证未启用", nil)
		return
	}
	if h.ldapService == nil || !h.ldapService.IsEnabled() {
		response.HandleBadRequest(c, "LDAP 认证未启用", nil)
		return
	}

	user, err := h.authService.GetCurrentUser(c)
	if err != nil {
		response.HandleUnauthorized(c, "用户未认证")
		return
	}
	if user.AuthSource != models.AuthSourceLocal {
		response.HandleBadRequest(c, "仅本地账号可绑定 LDAP", nil)
		return
	}
	if user.Role == "admin" {
		response.HandleBadRequest(c, "管理员账号不支持自助绑定认证来源", nil)
		return
	}

	ip := utils.GetClientIP(c)
	if isLoginLockedByUsername(ip, user.Username) {
		middleware.LogOperation(c, "auth.bind.ldap", user.Username, fmt.Errorf("尝试过于频繁，绑定被频率限制锁定"))
		recordAuthEvent(c, models.EventAuthLocked, "auth.bind.ldap", user.Username, models.SeverityHigh, "尝试过于频繁，绑定被频率限制锁定")
		handleLoginBlocked(c)
		return
	}

	dn, displayName, err := h.ldapService.VerifyCredentials(user.Username, req.Password)
	if err != nil {
		if errors.Is(err, services.ErrLDAPInvalidCredentials) {
			registerLoginFailure(ip)
			registerLoginFailureByUsername(ip, user.Username)
			h.recordAuthFailure(c, "auth.bind.ldap", user.Username, false)
			h.recordLoginLog(c, "", user.Username, services.LoginMethodLDAP, "failed", "目录账号密码错误")
			response.HandleUnauthorized(c, "目录账号密码错误")
			return
		}
		utils.Warn("LDAP 自助绑定目录校验失败", utils.String("username", user.Username), utils.Err(err))
		response.HandleBadRequest(c, "认证源暂时不可用，请稍后重试", nil)
		return
	}

	status, message, err := h.bindLDAPToUser(user, dn, displayName)
	if err != nil {
		utils.Warn("LDAP 自助绑定失败", utils.String("username", user.Username), utils.Err(err))
		if status != 0 {
			response.HandleCustomError(c, status, response.CodeBadRequest, message)
			return
		}
		response.HandleInternalServerError(c, "绑定失败，请稍后重试")
		return
	}

	resetLoginFailures(ip)
	resetLoginFailuresByUsername(ip, user.Username)
	middleware.LogOperation(c, "auth.bind.ldap", user.Username, nil)
	response.HandleSuccess(c, http.StatusOK, "绑定成功，本地密码已移除，请使用目录账号登录", nil)
}

// loadConfirmTargetUser 读取票据指向的同名本地账号。
func (h *Handler) loadConfirmTargetUser(ticket services.IdentityConfirmTicket) (*models.User, error) {
	var user models.User
	if err := h.db.Where("id = ?", ticket.UserID).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// bindTicketToUser 事务内把外部身份绑定到本地账号：
// 写绑定、auth_source=oauth2、清空本地密码；同一外部身份或已有其它绑定均视为冲突。
func (h *Handler) bindTicketToUser(ticket services.IdentityConfirmTicket, user *models.User) (int, string, error) {
	var status int
	var message string
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var existing models.AuthRecord
		err := tx.Where("auth_type = ? AND provider_name = ? AND external_id = ?",
			ticket.AuthType, ticket.ProviderName, ticket.ExternalID).First(&existing).Error
		switch {
		case err == nil:
			if existing.UserID == user.ID {
				status, message = http.StatusConflict, "该账号已绑定该外部身份"
			} else {
				status, message = http.StatusConflict, "该外部身份已绑定其他账号"
			}
			return errIdentityBindConflict
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return fmt.Errorf("检查外部身份绑定失败: %w", err)
		}

		var bindingCount int64
		if err := tx.Model(&models.AuthRecord{}).Where("user_id = ?", user.ID).Count(&bindingCount).Error; err != nil {
			return fmt.Errorf("检查账号绑定失败: %w", err)
		}
		if bindingCount > 0 {
			status, message = http.StatusConflict, "该账号已绑定其他认证来源"
			return errIdentityBindConflict
		}

		if err := tx.Create(&models.AuthRecord{
			UserID:       user.ID,
			AuthType:     ticket.AuthType,
			ExternalID:   ticket.ExternalID,
			ExternalName: ticket.ExternalName,
			ProviderName: ticket.ProviderName,
		}).Error; err != nil {
			return fmt.Errorf("保存外部身份绑定失败: %w", err)
		}

		if err := tx.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
			"auth_source":   ticket.AuthType,
			"password_hash": "",
		}).Error; err != nil {
			return fmt.Errorf("更新账号认证来源失败: %w", err)
		}
		user.AuthSource = ticket.AuthType
		user.PasswordHash = ""
		return nil
	})
	return status, message, err
}

// createOAuthIdentityAccount 创建独立外部账号并写绑定（同事务）。
// 返回的用户名已排除占用；并发抢占由用户名唯一索引兜底，冲突时重试下一个序号。
func (h *Handler) createOAuthIdentityAccount(ticket services.IdentityConfirmTicket) (*models.User, int, string, error) {
	var existing models.AuthRecord
	err := h.db.Where("auth_type = ? AND provider_name = ? AND external_id = ?",
		ticket.AuthType, ticket.ProviderName, ticket.ExternalID).First(&existing).Error
	switch {
	case err == nil:
		return nil, http.StatusConflict, "该外部身份已绑定其他账号", nil
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, http.StatusInternalServerError, "创建账号失败，请稍后重试", err
	}

	uuid, err := generateOAuthUUID()
	if err != nil {
		return nil, http.StatusInternalServerError, "创建账号失败，请稍后重试", err
	}
	user := models.User{
		UUID:         uuid,
		DisplayName:  ticket.DisplayName,
		PasswordHash: "",
		Role:         "user",
		AuthSource:   models.AuthSourceOAuth2,
		CreatedVia:   models.CreatedViaOAuth2JIT,
	}
	// 用户名占用检查与插入在同一事务内完成，避免并发建号撞唯一索引。
	err = h.db.Transaction(func(tx *gorm.DB) error {
		username, uerr := h.availableOAuthAccountUsername(tx, ticket.ExternalUsername, ticket.ProviderID)
		if uerr != nil {
			return uerr
		}
		user.Username = username
		if err := tx.Create(&user).Error; err != nil {
			return fmt.Errorf("创建本地账号失败: %w", err)
		}
		return tx.Create(&models.AuthRecord{
			UserID:       user.ID,
			AuthType:     ticket.AuthType,
			ExternalID:   ticket.ExternalID,
			ExternalName: ticket.ExternalName,
			ProviderName: ticket.ProviderName,
		}).Error
	})
	if err != nil {
		return nil, 0, "", err
	}
	return &user, 0, "", nil
}

// availableOAuthAccountUsername 生成符合本地约束（3-64、字符白名单）且未被占用的用户名。
// 命名规则：外部用户名 + @<provider_id 或 oauth2>，冲突时追加 _2、_3...
// 查询失败时返回错误而不是放行未校验的候选名，避免落库时撞唯一索引返回 500。
func (h *Handler) availableOAuthAccountUsername(tx *gorm.DB, external, providerID string) (string, error) {
	base := sanitizeUsername(external)
	suffix := sanitizeProviderSuffix(providerID)
	for i := 1; i <= 100; i++ {
		candidate := composeOAuthAccountUsername(base, suffix, i)
		user, err := findLocalUserByUsername(tx, candidate)
		if err != nil {
			return "", fmt.Errorf("检查账号名是否可用失败: %w", err)
		}
		if user == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("同名账号过多，无法生成独立账号名")
}

// composeOAuthAccountUsername 拼接用户名并保证长度不超过 64 字节。
func composeOAuthAccountUsername(base, suffix string, seq int) string {
	seqPart := ""
	if seq > 1 {
		seqPart = "_" + strconv.Itoa(seq)
	}
	tail := "@" + suffix + seqPart
	maxBase := 64 - len(tail)
	if maxBase < 3 {
		maxBase = 3
	}
	base = truncateUsername(base, maxBase)
	if base == "" {
		base = "user"
	}
	return base + tail
}

// sanitizeProviderSuffix 将 provider 标识规整为用户名允许的字符集（字母/数字/._-）。
func sanitizeProviderSuffix(providerID string) string {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return models.AuthSourceOAuth2
	}
	providerID = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, providerID)
	providerID = truncateUsername(providerID, 32)
	if providerID == "" {
		return models.AuthSourceOAuth2
	}
	return providerID
}

// bindLDAPToUser 事务内绑定 LDAP 身份：写绑定、auth_source=ldap、清空本地密码。
func (h *Handler) bindLDAPToUser(user *models.User, dn, displayName string) (int, string, error) {
	var status int
	var message string
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var existing models.AuthRecord
		err := tx.Where("auth_type = ? AND provider_name = ? AND external_id = ?",
			models.AuthSourceLDAP, models.AuthSourceLDAP, dn).First(&existing).Error
		switch {
		case err == nil:
			if existing.UserID == user.ID {
				status, message = http.StatusConflict, "该账号已绑定该外部身份"
			} else {
				status, message = http.StatusConflict, "该外部身份已绑定其他账号"
			}
			return errIdentityBindConflict
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return fmt.Errorf("检查外部身份绑定失败: %w", err)
		}

		var bindingCount int64
		if err := tx.Model(&models.AuthRecord{}).Where("user_id = ?", user.ID).Count(&bindingCount).Error; err != nil {
			return fmt.Errorf("检查账号绑定失败: %w", err)
		}
		if bindingCount > 0 {
			status, message = http.StatusConflict, "该账号已绑定其他认证来源"
			return errIdentityBindConflict
		}

		if err := tx.Create(&models.AuthRecord{
			UserID:       user.ID,
			AuthType:     models.AuthSourceLDAP,
			ExternalID:   dn,
			ExternalName: displayName,
			ProviderName: models.AuthSourceLDAP,
		}).Error; err != nil {
			return fmt.Errorf("保存 LDAP 绑定失败: %w", err)
		}

		if err := tx.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
			"auth_source":   models.AuthSourceLDAP,
			"password_hash": "",
		}).Error; err != nil {
			return fmt.Errorf("更新账号认证来源失败: %w", err)
		}
		user.AuthSource = models.AuthSourceLDAP
		user.PasswordHash = ""
		return nil
	})
	return status, message, err
}
