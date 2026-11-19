package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// 身份确认票据有效期与本地密码错误上限。
// 票据为一次性短期凭证，仅用于 OAuth2 同名冲突时让用户确认本地身份。
const (
	IdentityConfirmTicketTTL          = 5 * time.Minute
	IdentityConfirmMaxPasswordFailure = 5
)

// IdentityConfirmTicket 待确认的外部身份与同名本地账号信息。
// 服务端内存保存，不落库；与外部身份强绑定，不能用于其它账号。
type IdentityConfirmTicket struct {
	UserID           uint      // 同名本地账号 ID
	Username         string    // 同名本地账号用户名
	Role             string    // 同名本地账号角色（admin 不开放自助处理）
	AuthType         string    // 外部认证类型（oauth2）
	ProviderID       string    // provider 稳定标识（用于展示与独立账号命名）
	ProviderName     string    // provider key（oauth2:<provider_id>）
	ExternalID       string    // 外部身份标识（OIDC sub）
	ExternalUsername string    // 外部用户名（已规整为本地允许的字符集）
	ExternalName     string    // 外部显示名
	DisplayName      string    // 外部姓名/显示名（建号时写入）
	ExpiresAt        time.Time // 过期时间（UTC）

	failures int  // 本地密码错误次数
	inFlight bool // 是否有确认请求正在处理（防并发重复提交）
}

// IdentityConfirmService 内存票据服务：随机 32 字节 hex，一次性使用，过期自动清理。
type IdentityConfirmService struct {
	mu      sync.Mutex
	tickets map[string]*IdentityConfirmTicket
}

// NewIdentityConfirmService 创建票据服务。
// 过期票据在 Issue/Lookup/Begin 等操作时惰性清理，不启动常驻协程，避免实例无法回收。
func NewIdentityConfirmService() *IdentityConfirmService {
	return &IdentityConfirmService{tickets: make(map[string]*IdentityConfirmTicket)}
}

// Issue 保存票据并返回一次性 token（32 字节随机数的 hex）。
func (s *IdentityConfirmService) Issue(ticket IdentityConfirmTicket) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成确认票据失败: %w", err)
	}
	token := hex.EncodeToString(buf)
	now := time.Now().UTC()
	if ticket.ExpiresAt.IsZero() {
		ticket.ExpiresAt = now.Add(IdentityConfirmTicketTTL)
	}
	ticket.failures = 0
	s.mu.Lock()
	if s.tickets == nil {
		s.tickets = make(map[string]*IdentityConfirmTicket)
	}
	s.tickets[token] = &ticket
	s.cleanupExpiredLocked(now)
	s.mu.Unlock()
	return token, nil
}

// Lookup 读取票据（不消费）。不存在或已过期返回 (零值, false)。
func (s *IdentityConfirmService) Lookup(token string) (IdentityConfirmTicket, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.cleanupExpiredLocked(now)
	ticket, ok := s.tickets[token]
	if !ok {
		return IdentityConfirmTicket{}, false
	}
	return *ticket, true
}

// Consume 读取并删除票据（一次性使用）。
func (s *IdentityConfirmService) Consume(token string) (IdentityConfirmTicket, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(time.Now().UTC())
	ticket, ok := s.tickets[token]
	if !ok {
		return IdentityConfirmTicket{}, false
	}
	delete(s.tickets, token)
	return *ticket, true
}

// RecordFailure 记录一次本地密码错误；达到上限后票据立即失效。
// 返回值 valid=false 表示票据已失效（达到错误上限或票据不存在）。
func (s *IdentityConfirmService) RecordFailure(token string) (remaining int, valid bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	s.cleanupExpiredLocked(now)
	ticket, ok := s.tickets[token]
	if !ok {
		return 0, false
	}
	ticket.failures++
	if ticket.failures >= IdentityConfirmMaxPasswordFailure {
		delete(s.tickets, token)
		return 0, false
	}
	return IdentityConfirmMaxPasswordFailure - ticket.failures, true
}

// Begin 原子占用票据，防止同一票据被并发重复提交：
// 同一时刻只允许一个确认请求在处理，其余请求直接失败，保证「一次性」不被并发绕过。
// 处理失败时调用 End 释放占用以便用户重试；处理成功时调用 Consume 删除票据。
func (s *IdentityConfirmService) Begin(token string) (IdentityConfirmTicket, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(time.Now().UTC())
	ticket, ok := s.tickets[token]
	if !ok || ticket.inFlight {
		return IdentityConfirmTicket{}, false
	}
	ticket.inFlight = true
	return *ticket, true
}

// End 释放 Begin 占用的票据（本地密码错误等可重试场景）。
func (s *IdentityConfirmService) End(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ticket, ok := s.tickets[token]; ok {
		ticket.inFlight = false
	}
}

// cleanupExpiredLocked 删除已过期票据（调用方需持有锁）。
func (s *IdentityConfirmService) cleanupExpiredLocked(now time.Time) {
	for token, ticket := range s.tickets {
		if now.After(ticket.ExpiresAt) {
			delete(s.tickets, token)
		}
	}
}

// IdentityConflictError 外部认证成功但与本地同名账号冲突。
// 供 OAuth2 回调区分「同名冲突」与其它失败：同名冲突可根据配置引导用户自助确认。
// 对外错误文案保持通用，不泄露认证源细节。
type IdentityConflictError struct {
	Username         string // 同名本地账号用户名
	LocalUserID      uint   // 同名本地账号 ID
	LocalRole        string // 同名本地账号角色
	LocalDisabled    bool   // 同名本地账号是否被禁用
	LocalAuthSource  string // 同名本地账号认证归属
	AuthType         string // 外部认证类型（oauth2）
	ProviderID       string // provider 稳定标识
	ProviderName     string // provider key（oauth2:<provider_id>）
	ExternalID       string // 外部身份标识
	ExternalUsername string // 外部用户名（已规整）
	ExternalName     string // 外部显示名
	DisplayName      string // 外部姓名/显示名
	RejectedSource   string // 被拒绝的本地认证归属
}

// Error 返回通用提示，避免泄露账号是否存在或认证源细节。
func (e *IdentityConflictError) Error() string {
	return ErrAccountUnavailable.Error()
}

// Is 让 errors.Is(err, ErrAccountUnavailable) 继续成立，保持既有调用方语义。
func (e *IdentityConflictError) Is(target error) bool {
	return target == ErrAccountUnavailable
}
