package security

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"fuzhan/internal/models"
)

// 认证失败场景判定：单次认证失败不直接构成安全事件（正常输错密码、令牌过期等），
// 仅当失败命中下列场景之一时才写入 security_events：
//   - 频率/规模：同一 IP 或同一账号在窗口内失败达到阈值（暴破）；
//   - 跨源：同一 IP 对多个账号失败（密码喷洒/账号枚举），或同一账号来自多个 IP（撞库）；
//   - 失败后成功：短窗口内多次失败后从不同 IP/UA 成功登录（凭证撞库成功迹象）；
//   - 越过认证边界：未认证访问管理接口、写操作，或 WebDAV/FTP 等非 Web 会话渠道认证失败；
//   - 高价值身份：管理员账号单次失败（中危），连续 2 次升级为高危。
//
// 单次失败仍由调用方写入 login_logs / 审计日志，这里只负责「是否升级为安全事件」。
const (
	authFailWindow          = 15 * time.Minute // 同一 IP 登录失败统计窗口
	authIPFailureLimit      = 10               // 同一 IP 窗口内失败阈值
	authAccountWindow       = 30 * time.Minute // 同一账号失败统计窗口（兼顾失败后成功场景）
	authAccountFailureLimit = 5                // 同一账号窗口内失败阈值
	authDistinctUserLimit   = 3                // 同一 IP 窗口内不同用户名阈值（喷洒/枚举）
	authDistinctIPLimit     = 3                // 同一账号窗口内不同来源 IP 阈值（撞库）
	authSuccessAfterFails   = 3                // 失败后成功判定所需的前置失败次数
	authTrackerMaxEntries   = 10000            // 跟踪记录上限，保证内存有界
)

// AuthEventDecision 认证请求命中安全事件场景后的入库建议。
type AuthEventDecision struct {
	EventType string
	Severity  string
	Reason    string
}

type ipAuthRecord struct {
	count     int
	start     time.Time
	usernames map[string]struct{}
	reported  bool
}

type accountAuthRecord struct {
	count          int
	start          time.Time
	ips            map[string]struct{}
	lastIP         string
	lastUA         string
	lastFailure    time.Time
	reported       bool
	adminEscalated bool
}

type authFailureTracker struct {
	mu       sync.Mutex
	ips      map[string]*ipAuthRecord
	accounts map[string]*accountAuthRecord
}

var authFailures = &authFailureTracker{
	ips:      make(map[string]*ipAuthRecord),
	accounts: make(map[string]*accountAuthRecord),
}

// NoteLoginFailure 记录一次登录/注册/验证码失败，并返回是否命中安全事件场景。
// highValue 用于标记管理员账号等高价值身份（单次失败即中危，连续 2 次升高危）。
// 同一窗口内同一条记录只产生一次聚合事件，避免攻击时刷屏。
func NoteLoginFailure(ip, username, userAgent string, highValue bool) *AuthEventDecision {
	ip = strings.TrimSpace(ip)
	username = strings.TrimSpace(username)
	ua := truncate(userAgent, 512)

	authFailures.mu.Lock()
	defer authFailures.mu.Unlock()
	now := time.Now()
	authFailures.prune(now)

	var ipRec *ipAuthRecord
	if ip != "" {
		rec, ok := authFailures.ips[ip]
		if !ok || now.Sub(rec.start) > authFailWindow {
			rec = &ipAuthRecord{start: now, usernames: make(map[string]struct{})}
			authFailures.ips[ip] = rec
		}
		rec.count++
		if username != "" {
			rec.usernames[strings.ToLower(username)] = struct{}{}
		}
		ipRec = rec
	}

	var acctRec *accountAuthRecord
	if username != "" {
		key := strings.ToLower(username)
		rec, ok := authFailures.accounts[key]
		if !ok || now.Sub(rec.start) > authAccountWindow {
			rec = &accountAuthRecord{start: now, ips: make(map[string]struct{})}
			authFailures.accounts[key] = rec
		}
		rec.count++
		rec.lastIP = ip
		rec.lastUA = ua
		rec.lastFailure = now
		if ip != "" {
			rec.ips[ip] = struct{}{}
		}
		acctRec = rec
	}

	// 高价值身份：管理员账号单次失败中危；连续 2 次升高危（仅各记录一次）
	if acctRec != nil && highValue {
		if acctRec.count >= 2 && !acctRec.adminEscalated {
			acctRec.reported = true
			acctRec.adminEscalated = true
			return &AuthEventDecision{
				EventType: models.EventAuthFailedLogin,
				Severity:  models.SeverityHigh,
				Reason:    "管理员账号连续认证失败（窗口内 2 次以上），疑似定向攻击",
			}
		}
		if acctRec.count == 1 && !acctRec.reported {
			acctRec.reported = true
			return &AuthEventDecision{
				EventType: models.EventAuthFailedLogin,
				Severity:  models.SeverityMedium,
				Reason:    "管理员账号认证失败",
			}
		}
	}

	// 同一 IP 对多个账号失败：密码喷洒 / 账号枚举
	if ipRec != nil && !ipRec.reported && len(ipRec.usernames) >= authDistinctUserLimit {
		ipRec.reported = true
		return &AuthEventDecision{
			EventType: models.EventAuthSpraying,
			Severity:  models.SeverityHigh,
			Reason:    "同一 IP 在窗口内对多个账号认证失败，疑似密码喷洒/账号枚举",
		}
	}

	// 同一账号来自多个 IP：撞库 / 分布式暴破
	if acctRec != nil && !acctRec.reported && len(acctRec.ips) >= authDistinctIPLimit {
		acctRec.reported = true
		return &AuthEventDecision{
			EventType: models.EventAuthSpraying,
			Severity:  models.SeverityHigh,
			Reason:    "同一账号在窗口内来自多个 IP 认证失败，疑似撞库/分布式暴破",
		}
	}

	// 同一账号失败达阈值：单账号暴破（聚合为一条）
	if acctRec != nil && !acctRec.reported && acctRec.count == authAccountFailureLimit {
		acctRec.reported = true
		return &AuthEventDecision{
			EventType: models.EventAuthFailedLogin,
			Severity:  models.SeverityHigh,
			Reason:    "同一账号在窗口内认证失败达阈值，疑似暴力破解",
		}
	}

	// 同一 IP 失败达阈值：跨账号暴破（聚合为一条）
	if ipRec != nil && !ipRec.reported && ipRec.count == authIPFailureLimit {
		ipRec.reported = true
		return &AuthEventDecision{
			EventType: models.EventAuthFailedLogin,
			Severity:  models.SeverityHigh,
			Reason:    "同一 IP 在窗口内认证失败达阈值，疑似暴力破解",
		}
	}

	return nil
}

// NoteLoginSuccess 登录成功后检查「失败后成功」场景：同一账号在窗口内多次失败，
// 随后从不同 IP/UA 成功登录，疑似凭证撞库成功。无论是否命中都会清理该账号与来源 IP
// 的失败记录（成功即证明凭证有效，避免误锁与重复告警）。
func NoteLoginSuccess(ip, username, userAgent string) *AuthEventDecision {
	ip = strings.TrimSpace(ip)
	username = strings.TrimSpace(username)
	ua := truncate(userAgent, 512)

	authFailures.mu.Lock()
	defer authFailures.mu.Unlock()
	now := time.Now()

	var decision *AuthEventDecision
	if username != "" {
		key := strings.ToLower(username)
		if rec, ok := authFailures.accounts[key]; ok {
			if rec.count >= authSuccessAfterFails && now.Sub(rec.lastFailure) <= authAccountWindow &&
				(rec.lastIP != ip || rec.lastUA != ua) {
				decision = &AuthEventDecision{
					EventType: models.EventAuthSuspiciousLogin,
					Severity:  models.SeverityHigh,
					Reason:    "同一账号在窗口内多次认证失败后从不同 IP/UA 成功登录，疑似凭证撞库成功",
				}
			}
			delete(authFailures.accounts, key)
		}
	}
	if ip != "" {
		delete(authFailures.ips, ip)
	}
	return decision
}

// EvaluateUnauthorized 判定一个 401 请求是否构成安全事件。
// 普通读取请求的 401（令牌过期、未登录浏览）不构成事件；
// 仅在越过认证边界时成立：未认证访问管理接口、未认证写操作（会话维护接口除外），
// 或 WebDAV/FTP 等非 Web 会话渠道认证失败。
func EvaluateUnauthorized(method, path, guard string) (bool, string) {
	if guard != "" && guard != models.GuardAuth {
		return true, "非 Web 会话渠道认证失败"
	}
	if isAdminPath(path) {
		return true, "未认证访问管理接口"
	}
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		if isSessionMaintenancePath(path) {
			return false, ""
		}
		return true, "未认证的写操作请求"
	}
	return false, ""
}

// isAdminPath 判断是否为管理接口路径。
func isAdminPath(path string) bool {
	return strings.Contains(path, "/admin/") || strings.HasSuffix(path, "/admin")
}

// isSessionMaintenancePath 判断是否为会话维护类接口：其 401 属于令牌过期，
// 不代表越过认证边界，不升级为安全事件。
func isSessionMaintenancePath(path string) bool {
	return strings.HasSuffix(path, "/auth/refresh") || strings.HasSuffix(path, "/auth/logout")
}

// prune 清理过期记录，并在条目总数超限时整体重置，保证内存有界。
func (s *authFailureTracker) prune(now time.Time) {
	for ip, rec := range s.ips {
		if now.Sub(rec.start) > authFailWindow {
			delete(s.ips, ip)
		}
	}
	for key, rec := range s.accounts {
		if now.Sub(rec.start) > authAccountWindow {
			delete(s.accounts, key)
		}
	}
	if len(s.ips)+len(s.accounts) > authTrackerMaxEntries {
		s.ips = make(map[string]*ipAuthRecord)
		s.accounts = make(map[string]*accountAuthRecord)
	}
}

// ResetAuthFailureState 清空认证失败跟踪状态（供测试隔离使用）。
func ResetAuthFailureState() {
	authFailures.mu.Lock()
	defer authFailures.mu.Unlock()
	authFailures.ips = make(map[string]*ipAuthRecord)
	authFailures.accounts = make(map[string]*accountAuthRecord)
}
