package ftp

import (
	"crypto/tls"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/afero"

	ftpfs "fuzhan/internal/access/ftp"
	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"

	ftpserver "github.com/fclairamb/ftpserverlib"
)

// AuthUserFunc 用户认证函数（由 main.go 注入）
type AuthUserFunc func(username, password string) (string, error)

// FTPHandler FTP 服务器处理器
type FTPHandler struct {
	server     *ftpserver.FtpServer
	rootDirs   map[string]string
	privateDir string
	authUser   AuthUserFunc
	recordFn   ftpfs.OperationRecordFunc
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

// ftpDriver FTP驱动实现
type ftpDriver struct {
	rootDirs    map[string]string
	privateDir  string
	authUser    AuthUserFunc
	recordFn    ftpfs.OperationRecordFunc
	host        string
	port        int
	tlsCfg      *tls.Config
	activeConns int32
	maxConns    int32
	connsMu     sync.Mutex
	failCounts  map[string]*loginFailEntry // IP → failure counter
	failMu      sync.Mutex
}

type loginFailEntry struct {
	count    int
	lastFail time.Time
}

const (
	maxConnections      = 50              // 最大并发连接数
	maxLoginFailures    = 5               // 连续失败次数阈值
	loginFailWindow     = 5 * time.Minute // 失败计数窗口
	loginRateLimitDelay = 2 * time.Second // 超过阈值后的延迟
)

func (d *ftpDriver) GetSettings() (*ftpserver.Settings, error) {
	addr := fmt.Sprintf("%s:%d", d.host, d.port)
	return &ftpserver.Settings{
		ListenAddr: addr,
		PassiveTransferPortRange: &ftpserver.PortRange{
			Start: 2122,
			End:   2221, // 100 passive ports
		},
	}, nil
}

func (d *ftpDriver) ClientConnected(cc ftpserver.ClientContext) (string, error) {
	// Connection limit check
	if d.maxConns > 0 && atomic.LoadInt32(&d.activeConns) >= d.maxConns {
		utils.Warn("FTP连接数已达上限，拒绝新连接",
			utils.Int("active", int(atomic.LoadInt32(&d.activeConns))),
			utils.Int("max", int(d.maxConns)),
			utils.Stringer("addr", cc.RemoteAddr()))
		return "", fmt.Errorf("服务器连接数已达上限，请稍后重试")
	}
	atomic.AddInt32(&d.activeConns, 1)
	connID := cc.ID()
	utils.Info("FTP客户端连接",
		utils.Int("id", int(connID)),
		utils.Stringer("addr", cc.RemoteAddr()),
		utils.Int("activeConns", int(atomic.LoadInt32(&d.activeConns))))
	return "ftpserver", nil
}

func (d *ftpDriver) ClientDisconnected(cc ftpserver.ClientContext) {
	atomic.AddInt32(&d.activeConns, -1)
	utils.Info("FTP客户端断开",
		utils.Int("id", int(cc.ID())),
		utils.Int("activeConns", int(atomic.LoadInt32(&d.activeConns))))
}

func (d *ftpDriver) AuthUser(cc ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	clientIP := cc.RemoteAddr().String()

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
		utils.Warn("FTP登录频率过高，暂时拒绝",
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
		utils.Info("FTP匿名用户登录成功",
			utils.String("user", user),
			utils.String("ip", clientIP))
		multiFs := ftpfs.NewMultiRootFs(d.rootDirs, "", "", clientIP, d.recordFn)
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
		d.failMu.Unlock()

		utils.Warn("FTP登录失败",
			utils.String("user", user),
			utils.String("ip", clientIP),
			utils.Int("failCount", entry.count))

		// Add delay on failure to slow brute force
		time.Sleep(1 * time.Second)
		return nil, fmt.Errorf("认证失败")
	}

	// Reset failure count on success
	d.failMu.Lock()
	delete(d.failCounts, clientIP)
	d.failMu.Unlock()

	utils.Info("FTP用户登录成功",
		utils.String("user", user),
		utils.String("ip", clientIP))
	multiFs := ftpfs.NewMultiRootFs(d.rootDirs, d.privateDir, uuid, clientIP, d.recordFn)
	return &ftpClientDriver{Fs: multiFs.ClientDriver()}, nil
}

func (d *ftpDriver) GetTLSConfig() (*tls.Config, error) {
	return d.tlsCfg, nil
}

// ftpClientDriver 客户端驱动
type ftpClientDriver struct {
	afero.Fs
}

// Start 启动 FTP 服务器
func (h *FTPHandler) Start() error {
	serverCfg := appconfig.GlobalConfig.Server

	// 尝试 FTPS（TLS启用时），否则回退到 FTP
	ftpCfg := serverCfg.FTP
	ftpsCfg := serverCfg.FTPS
	useTLS := ftpsCfg.Enabled
	enabled := ftpCfg.Enabled || ftpsCfg.Enabled

	if !enabled {
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
		h.rootDirs = map[string]string{"files": "."}
	}

	// TLS 配置（共享 server.tls）
	var tlsCfg *tls.Config
	if useTLS && serverCfg.TLS.CertFile != "" && serverCfg.TLS.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(serverCfg.TLS.CertFile, serverCfg.TLS.KeyFile)
		if err != nil {
			// FTPS is explicitly enabled but cert loading failed — refuse to start as plain FTP
			return fmt.Errorf("FTPS已启用但TLS证书加载失败: %w", err)
		}
		tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}}
	} else if useTLS {
		// FTPS enabled but no cert configured
		return fmt.Errorf("FTPS已启用但未配置TLS证书文件 (server.tls.cert_file / server.tls.key_file)")
	}

	// 确定端口和协议
	host := serverCfg.Host
	if host == "" {
		host = "0.0.0.0"
	}
	port := ftpCfg.Port
	if port <= 0 {
		port = 21
	}

	// Check if port is already in use before starting
	if appconfig.IsPortInUse(port) {
		return fmt.Errorf("FTP端口 %d 已被占用", port)
	}

	driver := &ftpDriver{
		rootDirs:   h.rootDirs,
		privateDir: h.privateDir,
		authUser:   h.authUser,
		recordFn:   h.recordFn,
		host:       host,
		port:       port,
		tlsCfg:     tlsCfg,
		maxConns:   maxConnections,
		failCounts: make(map[string]*loginFailEntry),
	}

	// 创建 FTP 服务器
	h.server = ftpserver.NewFtpServer(driver)

	// 启动服务
	proto := "FTP"
	if useTLS {
		proto = "FTPS"
	}
	utils.Info(proto+"服务器启动中", utils.Int("port", port), utils.String("host", host), utils.Bool("tls", useTLS))
	go func() {
		if err := h.server.ListenAndServe(); err != nil {
			utils.Error(proto+"服务器已停止", utils.Err(err))
		}
	}()

	return nil
}

// Stop 停止 FTP 服务器
func (h *FTPHandler) Stop() {
	if h.server != nil {
		h.server.Stop()
		h.server = nil
	}
	utils.Info("FTP服务器已停止")
}
