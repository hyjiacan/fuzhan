package setup

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// NetworkInterface 网络接口信息
type NetworkInterface struct {
	IP   string `json:"ip"`
	Name string `json:"name"`
}

// SetupHandler 配置引导处理器
type SetupHandler struct{}

// NewSetupHandler 创建配置引导处理器实例
func NewSetupHandler() *SetupHandler {
	return &SetupHandler{}
}

// IsInitialized 检查是否已初始化
func (h *SetupHandler) IsInitialized(c *gin.Context) {
	rootDirs := make([]gin.H, len(appconfig.GlobalConfig.Storage.Public.RootDirs))
	for i, dir := range appconfig.GlobalConfig.Storage.Public.RootDirs {
		// 从路径提取最后一段作为显示名称
		name := dir.Path
		if idx := strings.LastIndexAny(name, "/\\"); idx >= 0 {
			name = name[idx+1:]
		}
		rootDirs[i] = gin.H{
			"name": name,
		}
	}
	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"initialized": appconfig.GlobalConfig.App.Initialized,
		"rootDirs":    rootDirs,
	})
}

// SetupConfigRequest 配置保存请求
type SetupConfigRequest struct {
	AppName       string                      `json:"appName" binding:"max=64"`
	Host          string                      `json:"host" binding:"max=255"`
	Port          int                         `json:"port"`
	HTTPSEnabled  bool                        `json:"httpsEnabled"`
	HTTPSPort     int                         `json:"httpsPort"`
	TLSCertFile   string                      `json:"tlsCertFile"`
	TLSKeyFile    string                      `json:"tlsKeyFile"`
	AdminUsername string                      `json:"adminUsername" binding:"max=64"`
	AdminPassword string                      `json:"adminPassword" binding:"max=128"`
	Database      appconfig.DatabaseConfig    `json:"database"`
	RootDirs      []appconfig.DirectoryConfig `json:"rootDirs"`
	Initialized   bool                        `json:"initialized"`
}

// SaveConfig 保存配置（仅在未初始化时可用）
func (h *SetupHandler) SaveConfig(c *gin.Context) {
	// 初始化后禁止访问
	if appconfig.GlobalConfig.App.Initialized {
		utils.HandleForbidden(c, "系统已初始化，禁止修改配置")
		return
	}

	var req SetupConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
		return
	}

	// 验证必需字段
	if len(req.RootDirs) == 0 {
		utils.HandleBadRequest(c, "至少需要配置一个共享目录", nil)
		return
	}
	if req.AdminPassword == "" {
		utils.HandleBadRequest(c, "请设置管理员密码", nil)
		return
	}

	// 更新配置
	appconfig.GlobalConfig.App.Name = req.AppName
	appconfig.GlobalConfig.App.Initialized = true
	appconfig.GlobalConfig.Server.Host = req.Host
	appconfig.GlobalConfig.Server.HTTP.Port = req.Port
	if !appconfig.GlobalConfig.Server.HTTP.Enabled {
		appconfig.GlobalConfig.Server.HTTP.Enabled = true
	}
	appconfig.GlobalConfig.Server.HTTPS.Enabled = req.HTTPSEnabled
	if req.HTTPSPort > 0 {
		appconfig.GlobalConfig.Server.HTTPS.Port = req.HTTPSPort
	}
	if req.TLSCertFile != "" {
		appconfig.GlobalConfig.Server.TLS.CertFile = req.TLSCertFile
	}
	if req.TLSKeyFile != "" {
		appconfig.GlobalConfig.Server.TLS.KeyFile = req.TLSKeyFile
	}
	appconfig.GlobalConfig.Database = req.Database
	appconfig.GlobalConfig.Storage.Public.RootDirs = req.RootDirs

	// 重建 RootNames 映射（确保新配置的根目录名可被后续请求识别）
	if len(req.RootDirs) > 0 {
		newRootNames := map[string]string{}
		for _, dir := range req.RootDirs {
			dirName := filepath.Base(dir.Path)
			if dirName != "" {
				newRootNames[dirName] = dir.Path
			}
		}
		appconfig.RootNames = newRootNames
	}

	// 创建管理员用户
	if req.AdminUsername != "" && req.AdminPassword != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.AdminPassword), bcrypt.DefaultCost)
		if err != nil {
			utils.HandleErrorCompat(c, http.StatusInternalServerError, "密码加密失败: "+err.Error(), nil)
			return
		}

		user := models.User{
			Username:     req.AdminUsername,
			PasswordHash: string(hashedPassword),
			Role:         "admin",
		}

		// 检查是否已存在，不存在则创建
		var existing models.User
		if err := appconfig.GetDB().Where("username = ?", req.AdminUsername).First(&existing).Error; err != nil {
			if err := appconfig.GetDB().Create(&user).Error; err != nil {
				utils.HandleErrorCompat(c, http.StatusInternalServerError, "创建管理员用户失败: "+err.Error(), nil)
				return
			}
		} else {
			// 更新已存在的用户为管理员
			existing.PasswordHash = string(hashedPassword)
			existing.Role = "admin"
			appconfig.GetDB().Save(&existing)
		}
	}

	// 写回配置文件（使用启动时解析的绝对路径，避免工作目录不同导致的问题）
	configPath := appconfig.ConfigPath
	data, err := yaml.Marshal(appconfig.GlobalConfig)
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "序列化配置失败: "+err.Error(), nil)
		return
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "保存配置失败: "+err.Error(), nil)
		return
	}

	// 触发热更新钩子
	appconfig.RunOnSaveHooks()

	utils.HandleSuccess(c, http.StatusOK, "配置保存成功，配置已热生效", nil)
}

// ValidateDirectory 验证目录
func (h *SetupHandler) ValidateDirectory(c *gin.Context) {
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleBadRequest(c, "请求数据格式错误", nil)
		return
	}

	if req.Path == "" {
		utils.HandleBadRequest(c, "目录路径不能为空", nil)
		return
	}

	// 标准化路径：将正斜杠转为反斜杠（Windows兼容）
	path := strings.ReplaceAll(req.Path, "/", string(os.PathSeparator))
	if os.PathSeparator != '/' {
		path = strings.ReplaceAll(path, "\\", string(os.PathSeparator))
	}

	// 检查目录是否存在
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		// 尝试创建目录
		if err := os.MkdirAll(path, 0755); err != nil {
			utils.HandleErrorCompat(c, http.StatusBadRequest, "无法创建目录: "+err.Error(), nil)
			return
		}
		utils.HandleSuccess(c, http.StatusOK, "目录已创建", gin.H{
			"created": true,
			"exists":  true,
		})
		return
	}

	if err != nil {
		utils.HandleErrorCompat(c, http.StatusBadRequest, "无法访问路径: "+err.Error(), nil)
		return
	}

	if !info.IsDir() {
		utils.HandleErrorCompat(c, http.StatusBadRequest, "路径不是目录", nil)
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "目录有效", gin.H{
		"exists": true,
	})
}

// GetNetworkInterfaces 获取本机网络接口列表
func (h *SetupHandler) GetNetworkInterfaces(c *gin.Context) {
	interfaces, err := net.Interfaces()
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "获取网络接口失败: "+err.Error(), nil)
		return
	}

	// 始终添加 0.0.0.0 选项
	result := []NetworkInterface{
		{IP: "0.0.0.0", Name: "所有接口"},
		{IP: "::", Name: "所有接口 (IPv6)"},
	}

	for _, iface := range interfaces {
		// 跳过禁用的接口
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagRunning == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP
			// 只处理 IPv4 且非回环
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				result = append(result, NetworkInterface{
					IP:   ip.String(),
					Name: iface.Name,
				})
			}
		}
	}

	utils.HandleSuccess(c, http.StatusOK, "", result)
}

// GetDefaultConfig 获取默认配置（从嵌入的示例配置读取）
func (h *SetupHandler) GetDefaultConfig(c *gin.Context) {
	// 仅在未初始化时可用
	if appconfig.GlobalConfig.App.Initialized {
		utils.HandleForbidden(c, "系统已初始化，禁止访问默认配置")
		return
	}

	data, err := appconfig.DefaultConfigFS.ReadFile("fuzhan.sample.yaml")
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "读取默认配置失败: "+err.Error(), nil)
		return
	}

	// 解析 YAML 获取默认值
	var defaultConfig struct {
		App struct {
			Name string `yaml:"name"`
		} `yaml:"app"`
		Server struct {
			Host string `yaml:"host"`
			HTTP struct {
				Port int `yaml:"port"`
			} `yaml:"http"`
		} `yaml:"server"`
		Database struct {
			Driver string `yaml:"driver"`
			DSN    string `yaml:"dsn"`
		} `yaml:"database"`
	}

	if err := yaml.Unmarshal(data, &defaultConfig); err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, "解析默认配置失败: "+err.Error(), nil)
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"appName":  defaultConfig.App.Name,
		"host":     defaultConfig.Server.Host,
		"port":     defaultConfig.Server.HTTP.Port,
		"dbDriver": defaultConfig.Database.Driver,
		"dsn":      defaultConfig.Database.DSN,
	})
}
