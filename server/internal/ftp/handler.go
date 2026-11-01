package ftp

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/afero"

	ftpfs "fuzhan/internal/access/ftp"
	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/security"
	"fuzhan/internal/storage"
	"fuzhan/internal/utils"

	ftpserver "github.com/fclairamb/ftpserverlib"
)

// stripPort 去掉 "host:port" 形式的端口后缀，返回纯 IP。
// FTP 客户端地址带临时端口，直接入库/比对会导致同一 IP 被拆散
// （安全事件按 IP 聚合失效、公开文件上传者 IP 比对永远不匹配）。
func stripPort(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// recordFTPSecurity 记录一条 FTP 渠道安全事件（guard=ftp，无 HTTP 维度字段）
func recordFTPSecurity(clientIP, eventType, severity, operation, identity, target, reason string) {
	security.Record(&models.SecurityEvent{
		EventType: eventType,
		Severity:  severity,
		ClientIP:  clientIP,
		Identity:  identity,
		Operation: operation,
		Target:    target,
		Guard:     models.GuardFTP,
		Reason:    reason,
	})
}

// AuthUserFunc 用户认证函数（由 main.go 注入）
type AuthUserFunc func(username, password string) (string, error)

// FTPHandler FTP/FTPS 服务器处理器。
// FTP（明文，默认 21）与 FTPS（隐式 TLS，默认 990）相互独立：
// 仅在各自配置启用时监听对应端口；FTPS 启用后强制 TLS（ImplicitEncryption），
// 未启用 FTPS 时不会以明文形式在 990 提供 FTP。
type FTPHandler struct {
	servers    []*ftpserver.FtpServer
	drivers    []*ftpDriver
	rootDirs   map[string]string
	privateDir string
	authUser   AuthUserFunc
	recordFn   ftpfs.OperationRecordFunc
	backend    ftpfs.FtpBackend
	storages   map[string]storage.Storage
}

// NewFTPHandler 创建 FTP 处理器
func NewFTPHandler() *FTPHandler {
	return &FTPHandler{}
}

// SetAuthUser 设置用户认证函数
func (h *FTPHandler) SetAuthUser(authUser AuthUserFunc) {
	h.authUser = authUser
}

// SetRecordFunc 设置操作记录回调（由 main.go 注入 recordRepo）
func (h *FTPHandler) SetRecordFunc(fn ftpfs.OperationRecordFunc) {
	h.recordFn = fn
}

// SetBackend 设置服务层后端能力（权限/索引/配额）
func (h *FTPHandler) SetBackend(backend ftpfs.FtpBackend) {
	h.backend = backend
}

// SetStorages 设置远端存储后端映射（rootName → storage.Storage）
func (h *FTPHandler) SetStorages(storages map[string]storage.Storage) {
	h.storages = storages
}

// ftpDriver FTP驱动实现
type ftpDriver struct {
	rootDirs       map[string]string
	privateDir     string
	authUser       AuthUserFunc
	recordFn       ftpfs.OperationRecordFunc
	backend        ftpfs.FtpBackend
	storages       map[string]storage.Storage
	host           string
	port           int
	tlsCfg         *tls.Config
	tlsRequirement ftpserver.TLSRequirement
	passive        *ftpserver.PortRange
	activeConns    int32
	maxConns       int32
	connsMu        sync.Mutex
	failCounts     map[string]*loginFailEntry // IP → failure counter
	failMu         sync.Mutex
	stopCh         chan struct{}
}

type loginFailEntry struct {
	count    int
	lastFail time.Time
}

const (
	maxConnections   = 50              // 最大并发连接数
	maxLoginFailures = 5               // 连续失败次数阈值
	loginFailWindow  = 5 * time.Minute // 失败计数窗口
)

// 登录失败/频率超限的响应延迟（变量便于测试用例置零，避免拖慢测试）
var (
	loginRateLimitDelay = 2 * time.Second // 超过阈值后的延迟
	loginFailDelay      = 1 * time.Second // 单次认证失败后的延迟
)

// passiveRange 解析被动模式数据端口范围（默认 2122-2221）。
func passiveRange(cfg appconfig.FTPConfig) *ftpserver.PortRange {
	start, end := cfg.PassivePortStart, cfg.PassivePortEnd
	if start <= 0 {
		start = 2122
	}
	if end <= 0 {
		end = 2221
	}
	if end < start {
		end = start + 99
	}
	return &ftpserver.PortRange{Start: start, End: end}
}

func (d *ftpDriver) GetSettings() (*ftpserver.Settings, error) {
	addr := fmt.Sprintf("%s:%d", d.host, d.port)
	return &ftpserver.Settings{
		ListenAddr:               addr,
		TLSRequired:              d.tlsRequirement,
		PassiveTransferPortRange: d.passive,
	}, nil
}

func (d *ftpDriver) ClientConnected(cc ftpserver.ClientContext) (string, error) {
	// Connection limit check（CAS 避免并发越过上限）
	if d.maxConns > 0 {
		n := atomic.AddInt32(&d.activeConns, 1)
		if n > d.maxConns {
			atomic.AddInt32(&d.activeConns, -1)
			utils.FTPWarn("FTP连接数已达上限，拒绝新连接",
				utils.Int("active", int(n-1)),
				utils.Int("max", int(d.maxConns)),
				utils.Stringer("addr", cc.RemoteAddr()))
			return "", fmt.Errorf("服务器连接数已达上限，请稍后重试")
		}
	} else {
		atomic.AddInt32(&d.activeConns, 1)
	}
	connID := cc.ID()
	utils.FTPAccess("FTP客户端连接",
		utils.Int("id", int(connID)),
		utils.Stringer("addr", cc.RemoteAddr()),
		utils.Int("activeConns", int(atomic.LoadInt32(&d.activeConns))))
	return "ftpserver", nil
}

func (d *ftpDriver) ClientDisconnected(cc ftpserver.ClientContext) {
	atomic.AddInt32(&d.activeConns, -1)
	utils.FTPAccess("FTP客户端断开",
		utils.Int("id", int(cc.ID())),
		utils.Int("activeConns", int(atomic.LoadInt32(&d.activeConns))))
}

// startFailCleanup 定期清理过期的登录失败计数，防止 IP 条目无限增长。
func (d *ftpDriver) startFailCleanup() {
	d.stopCh = make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				d.failMu.Lock()
				now := time.Now()
				for ip, e := range d.failCounts {
					if now.Sub(e.lastFail) > loginFailWindow {
						delete(d.failCounts, ip)
					}
				}
				d.failMu.Unlock()
			case <-d.stopCh:
				return
			}
		}
	}()
}

func (d *ftpDriver) AuthUser(cc ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	clientIP := stripPort(cc.RemoteAddr().String())

	// Check rate limiting for failed logins
	d.failMu.Lock()
	entry, exists := d.failCounts[clientIP]
	now := time.Now()
	if exists {
		if now.Sub(entry.lastFail) > loginFailWindow {
			// Reset if window expired
			entry.count = 0
		}
	} else {
		entry = &loginFailEntry{}
		d.failCounts[clientIP] = entry
	}

	if entry.count >= maxLoginFailures {
		d.failMu.Unlock()
		utils.FTPWarn("FTP登录频率过高，暂时拒绝",
			utils.String("user", user),
			utils.String("ip", clientIP))
		time.Sleep(loginRateLimitDelay)
		return nil, fmt.Errorf("登录尝试过于频繁，请稍后重试")
	}
	d.failMu.Unlock()

	// 匿名用户：允许任意密码的 anonymous 或配置的匿名用户名登录
	cfg := appconfig.GlobalConfig
	anonUser := cfg.Account.Anonymous.Username
	if anonUser == "" {
		anonUser = "public"
	}
	if user == "anonymous" || user == anonUser {
		// 强制登录开启时，FTP 匿名登录必须禁用，与 Web/WebDAV 行为一致
		if cfg.Account.RequireLogin {
			utils.FTPWarn("强制登录已开启，拒绝匿名FTP登录",
				utils.String("user", user),
				utils.String("ip", clientIP))
			return nil, fmt.Errorf("强制登录已开启，匿名访问已禁用，请使用账号密码登录")
		}
		utils.FTPAccess("FTP匿名用户登录成功",
			utils.String("user", user),
			utils.String("ip", clientIP))
		multiFs := ftpfs.NewMultiRootFs(d.rootDirs, "", "", clientIP, d.recordFn, d.backend)
		multiFs.SetStorages(d.storages)
		return &ftpClientDriver{Fs: multiFs.ClientDriver()}, nil
	}

	// 认证用户：查用户表
	uuid, err := d.authUser(user, pass)
	if err != nil {
		// Record failure
		d.failMu.Lock()
		entry, ok := d.failCounts[clientIP]
		if !ok || time.Since(entry.lastFail) > loginFailWindow {
			entry = &loginFailEntry{}
			d.failCounts[clientIP] = entry
		}
		entry.count++
		entry.lastFail = time.Now()
		crossed := entry.count == maxLoginFailures
		d.failMu.Unlock()

		utils.FTPWarn("FTP登录失败",
			utils.String("user", user),
			utils.String("ip", clientIP),
			utils.Int("failCount", entry.count))

		// 每次认证失败记一条中危事件；仅在跨越阈值那一次记锁定事件（与 Web 侧 login_limit 去重语义一致）
		recordFTPSecurity(clientIP, models.EventAuthFailedLogin, models.SeverityMedium,
			"auth.login", user, user, "FTP 登录认证失败")
		if crossed {
			recordFTPSecurity(clientIP, models.EventAuthLocked, models.SeverityHigh,
				"auth.login", user, user, fmt.Sprintf("FTP 登录失败达阈值（%d 次），已暂时拒绝该 IP 的登录", maxLoginFailures))
		}

		// Add delay on failure to slow brute force
		time.Sleep(loginFailDelay)
		return nil, fmt.Errorf("认证失败")
	}

	// Reset failure count on success
	d.failMu.Lock()
	delete(d.failCounts, clientIP)
	d.failMu.Unlock()

	utils.FTPAccess("FTP用户登录成功",
		utils.String("user", user),
		utils.String("ip", clientIP))
	multiFs := ftpfs.NewMultiRootFs(d.rootDirs, d.privateDir, uuid, clientIP, d.recordFn, d.backend)
	multiFs.SetStorages(d.storages)
	return &ftpClientDriver{Fs: multiFs.ClientDriver()}, nil
}

func (d *ftpDriver) GetTLSConfig() (*tls.Config, error) {
	return d.tlsCfg, nil
}

// ftpClientDriver 客户端驱动
type ftpClientDriver struct {
	afero.Fs
}

// Start 启动 FTP/FTPS 服务器。
// FTP 与 FTPS 独立监听各自端口：FTPS 启用时强制隐式 TLS（990 默认），
// FTP 保持明文；仅当对应配置启用时才启动。
func (h *FTPHandler) Start() error {
	serverCfg := appconfig.GlobalConfig.Server
	ftpCfg := serverCfg.FTP
	ftpsCfg := serverCfg.FTPS
	ftpEnabled := ftpCfg.Enabled
	ftpsEnabled := ftpsCfg.Enabled

	if !ftpEnabled && !ftpsEnabled {
		utils.Info("FTP/FTPS服务未启用")
		return nil
	}

	// 保存根目录映射和私有存储路径
	h.rootDirs = appconfig.RootNames
	h.privateDir = appconfig.GlobalConfig.Storage.Private.Path
	if !appconfig.GlobalConfig.Storage.Private.Enabled {
		h.privateDir = ""
	}

	if len(h.rootDirs) == 0 {
		// 正常启动路径 initRootNames 已保证根目录非空；此处兜底避免暴露进程工作目录
		utils.Warn("未配置共享根目录，FTP/FTPS服务不启动")
		return nil
	}

	host := serverCfg.Host
	if host == "" {
		host = "0.0.0.0"
	}

	// H5 安全加固：与 HTTP 同源逻辑——系统未初始化时仅监听本机（FTP/FTPS），
	// 防止 setup 首启窗口期被公网/内网抢先完成初始化；初始化保存后热重启自动放开。
	effectiveHost, restricted := appconfig.LocalhostWhenSetup(host)
	if restricted {
		utils.Warn("系统未初始化，已限制 FTP/FTPS 仅监听本机 127.0.0.1，请在本地完成管理员初始化后自动放开对外访问",
			utils.String("configured_host", serverCfg.Host))
	}
	host = effectiveHost

	// FTPS 必须配置 TLS 证书，否则拒绝启动
	var tlsCfg *tls.Config
	if ftpsEnabled {
		if serverCfg.TLS.CertFile == "" || serverCfg.TLS.KeyFile == "" {
			return fmt.Errorf("FTPS已启用但未配置TLS证书文件 (server.tls.cert_file / server.tls.key_file)")
		}
		cert, err := tls.LoadX509KeyPair(serverCfg.TLS.CertFile, serverCfg.TLS.KeyFile)
		if err != nil {
			return fmt.Errorf("FTPS已启用但TLS证书加载失败: %w", err)
		}
		tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}}
	}

	if ftpEnabled {
		port := ftpCfg.Port
		if port <= 0 {
			port = 21
		}
		if appconfig.IsPortInUse(port) {
			return fmt.Errorf("FTP端口 %d 已被占用", port)
		}
		if err := h.startOne(host, port, ftpCfg, tlsCfg, ftpserver.ClearOrEncrypted, "FTP"); err != nil {
			return err
		}
	}

	if ftpsEnabled {
		port := ftpsCfg.Port
		if port <= 0 {
			port = 990
		}
		if appconfig.IsPortInUse(port) {
			return fmt.Errorf("FTPS端口 %d 已被占用", port)
		}
		if err := h.startOne(host, port, ftpCfg, tlsCfg, ftpserver.ImplicitEncryption, "FTPS"); err != nil {
			return err
		}
	}

	return nil
}

// startOne 启动单个 FTP/FTPS 监听实例。
func (h *FTPHandler) startOne(host string, port int, ftpCfg appconfig.FTPConfig, tlsCfg *tls.Config, tlsRequired ftpserver.TLSRequirement, proto string) error {
	driver := &ftpDriver{
		rootDirs:       h.rootDirs,
		privateDir:     h.privateDir,
		authUser:       h.authUser,
		recordFn:       h.recordFn,
		backend:        h.backend,
		storages:       h.storages,
		host:           host,
		port:           port,
		tlsCfg:         tlsCfg,
		tlsRequirement: tlsRequired,
		passive:        passiveRange(ftpCfg),
		maxConns:       maxConnections,
		failCounts:     make(map[string]*loginFailEntry),
	}

	server := ftpserver.NewFtpServer(driver)
	driver.startFailCleanup()
	h.servers = append(h.servers, server)
	h.drivers = append(h.drivers, driver)

	utils.Info(proto+"服务器启动中",
		utils.Int("port", port),
		utils.String("host", host),
		utils.Bool("tls", tlsCfg != nil))
	go func() {
		if err := server.ListenAndServe(); err != nil {
			utils.Error(proto+"服务器已停止", utils.Err(err))
		}
	}()

	return nil
}

// Stop 停止 FTP/FTPS 服务器
func (h *FTPHandler) Stop() {
	for _, server := range h.servers {
		server.Stop()
	}
	for _, d := range h.drivers {
		if d.stopCh != nil {
			close(d.stopCh)
		}
	}
	h.servers = nil
	h.drivers = nil
	utils.Info("FTP/FTPS服务器已停止")
}
