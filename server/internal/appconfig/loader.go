package appconfig

import (
	"embed"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"fuzhan/internal/utils"
)

// DefaultConfigFS 嵌入的默认配置文件
var DefaultConfigFS embed.FS

// ConfigFileName 默认配置文件名称
const ConfigFileName = "fuzhan.yaml"

// ConfigPath 配置文件路径
var ConfigPath string

// currentWorkDir 返回启动时的当前工作目录（cwd）
func currentWorkDir() string {
	basePath, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取当前工作目录失败: %v\n", err)
		os.Exit(1)
	}
	abspath, _ := filepath.Abs(basePath)
	return abspath
}

// binaryWorkDir 返回二进制文件所在目录。
// 优先使用 os.Executable 定位运行中的二进制，失败时回退到当前工作目录。
func binaryWorkDir() string {
	exe, err := os.Executable()
	if err == nil {
		dir, derr := filepath.Abs(filepath.Dir(exe))
		if derr == nil {
			return dir
		}
	}
	return currentWorkDir()
}

// resolveDefaultConfigPath 解析未显式指定时的默认配置文件路径。
// 查找顺序：启动目录（cwd）优先，其次二进制所在目录；
// 两处都不存在时回退到启动目录（此时将在该目录创建默认配置）。
func resolveDefaultConfigPath() string {
	for _, d := range []string{currentWorkDir(), binaryWorkDir()} {
		p := filepath.Join(d, ConfigFileName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(currentWorkDir(), ConfigFileName)
}

// applyConfiguredWorkDir 应用配置中的 work_dir。
// 配置指定时使用它（相对路径基于当前工作目录解析）；
// 未指定时保持默认值（配置所在目录）。
func applyConfiguredWorkDir() {
	cfgDir := GlobalConfig.WorkDir
	if cfgDir == "" {
		return
	}
	abs := cfgDir
	if !filepath.IsAbs(cfgDir) {
		cwd, err := os.Getwd()
		if err != nil {
			utils.Fatal("解析 work_dir 失败", utils.Err(err))
		}
		abs = filepath.Join(cwd, cfgDir)
	}
	resolved, err := filepath.Abs(abs)
	if err != nil {
		utils.Fatal("解析 work_dir 失败", utils.Err(err))
	}
	if err := os.MkdirAll(resolved, 0755); err != nil {
		utils.Fatal("创建 work_dir 失败", utils.Err(err))
	}
	WorkDir = resolved
}

// createDataDir 在工作目录下创建 data 目录
func createDataDir() {
	if err := os.MkdirAll(GetDataDir(), 0755); err != nil {
		utils.Fatal("创建数据目录失败", utils.Err(err))
	}
}

// InitLogging 初始化日志系统
func InitLogging() {
	workDir := WorkDir

	// 使用配置中的日志设置，如果没有配置则使用默认值
	logDir := ExpandWorkDirPath(GlobalConfig.Log.Directory)
	if logDir == "" {
		logDir = filepath.Join(workDir, "logs")
	}

	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "创建日志目录失败: %v\n", err)
		os.Exit(1)
	}

	// 确保 LogConfig 有默认值
	if GlobalConfig.Log.Level == "" {
		GlobalConfig.Log.Level = "info"
	}
	if GlobalConfig.Log.MaxSize == 0 {
		GlobalConfig.Log.MaxSize = 100
	}
	if GlobalConfig.Log.MaxBackups == 0 {
		GlobalConfig.Log.MaxBackups = 30
	}
	if GlobalConfig.Log.MaxAge == 0 {
		GlobalConfig.Log.MaxAge = 30
	}

	// 初始化日志系统
	if err := utils.InitLogger(GlobalConfig.Log); err != nil {
		fmt.Printf("初始化日志系统失败: %v\n", err)
		os.Exit(1)
	}

	utils.Info("日志系统初始化完成", utils.String("directory", logDir))
}

// createConfigFile 自动创建配置文件
func createConfigFile() {
	configFile := ConfigPath
	if _, err := os.Stat(configFile); err == nil {
		return
	}
	utils.Info("配置文件不存在，从嵌入资源生成")

	// 从嵌入资源读取默认配置
	defaultConfig, err := DefaultConfigFS.ReadFile("fuzhan.sample.yaml")
	if err != nil {
		utils.Fatal("读取嵌入的默认配置文件失败", utils.Err(err))
	}

	if err := os.WriteFile(configFile, defaultConfig, 0644); err != nil {
		utils.Fatal("创建默认配置文件时出错", utils.Err(err))
	}
	utils.Info("默认配置文件创建成功")
}

// readConfig 读取配置文件
func readConfig() {
	configFile := ConfigPath
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		utils.Fatal("配置文件不存在", utils.String("path", configFile))
	}
	data, err := os.ReadFile(configFile)
	if err != nil {
		utils.Fatal("读取配置文件时出错",
			utils.String("file", configFile),
			utils.Err(err))
	}

	// 首先尝试解析为带有字符串配额的配置
	var tempConfig struct {
		App     APPConfig     `yaml:"app"`
		Server  ServerConfig  `yaml:"server"`
		Account AccountConfig `yaml:"account"`
		Storage struct {
			Public struct {
				RootDirs []DirectoryConfigWithStringQuota `yaml:"root_dirs"`
			} `yaml:"public"`
			Temp              TempFilesConfigWithStringQuota      `yaml:"temp"`
			Private           PrivateStorageConfigWithStringQuota `yaml:"private"`
			AllowedExtensions []string                            `yaml:"allowed_extensions"`
		} `yaml:"storage"`
		Upload struct {
			Enabled     *bool           `yaml:"enabled"` // 指针: nil=未设置, 按默认值 true
			ChunkSize   string          `yaml:"chunk_size,omitempty"`
			MaxFileSize string          `yaml:"max_file_size,omitempty"`
			URLUpload   URLUploadConfig `yaml:"url_upload"`
		} `yaml:"upload"`
		Preview PreviewConfig   `yaml:"preview"`
		Log     utils.LogConfig `yaml:"log"`
		OpenAPI OpenAPIConfig   `yaml:"open_api"`
		Index   IndexConfig     `yaml:"index"`
	}

	if err := yaml.Unmarshal(data, &tempConfig); err != nil {
		utils.Fatal("解析配置文件时出错",
			utils.String("file", configFile),
			utils.String("hint", "请检查 YAML 格式是否正确（缩进需使用空格而非 Tab、字符串含特殊字符需加引号）"),
			utils.Err(err))
	}

	// 转换字符串配额为数字配额
	GlobalConfig.App = tempConfig.App
	GlobalConfig.Server = tempConfig.Server
	GlobalConfig.Account = tempConfig.Account
	GlobalConfig.Storage.AllowedExtensions = tempConfig.Storage.AllowedExtensions

	// 转换根目录配额
	for _, dirConfig := range tempConfig.Storage.Public.RootDirs {
		quota, err := ParseQuotaString(dirConfig.Quota)
		if err != nil {
			utils.Fatal("解析根目录配额时出错",
				utils.String("file", configFile),
				utils.String("path", dirConfig.Path),
				utils.String("quota", dirConfig.Quota),
				utils.Err(err))
		}

		GlobalConfig.Storage.Public.RootDirs = append(GlobalConfig.Storage.Public.RootDirs, DirectoryConfig{
			Path:  dirConfig.Path,
			Quota: quota,
		})
	}

	// 加载索引配置
	GlobalConfig.Index = tempConfig.Index

	// 转换私有文件配额
	var globalQuota, perUserQuota int64

	if tempConfig.Storage.Private.Quota.GlobalQuota != "" {
		parsedQuota, parseErr := ParseQuotaString(tempConfig.Storage.Private.Quota.GlobalQuota)
		if parseErr != nil {
			utils.Fatal("解析全局私有文件配额时出错",
				utils.String("file", configFile),
				utils.String("quota", tempConfig.Storage.Private.Quota.GlobalQuota),
				utils.Err(parseErr))
		}
		globalQuota = parsedQuota
	}

	if tempConfig.Storage.Private.Quota.PerUserQuota != "" {
		parsedQuota, parseErr := ParseQuotaString(tempConfig.Storage.Private.Quota.PerUserQuota)
		if parseErr != nil {
			utils.Fatal("解析每用户私有文件配额时出错",
				utils.String("file", configFile),
				utils.String("quota", tempConfig.Storage.Private.Quota.PerUserQuota),
				utils.Err(parseErr))
		}
		perUserQuota = parsedQuota
	}

	GlobalConfig.Storage.Private = PrivateStorageConfig{
		Enabled: tempConfig.Storage.Private.Enabled,
		Path:    tempConfig.Storage.Private.Path,
		Quota: PrivateQuotaConfig{
			GlobalQuota:  globalQuota,
			PerUserQuota: perUserQuota,
		},
		GlobalQuota:  globalQuota,
		PerUserQuota: perUserQuota,
	}

	// 解析上传配置
	if tempConfig.Upload.ChunkSize != "" {
		chunkSize, err := ParseQuotaString(tempConfig.Upload.ChunkSize)
		if err != nil {
			utils.Fatal("解析分片大小时出错",
				utils.String("file", configFile),
				utils.String("chunk_size", tempConfig.Upload.ChunkSize),
				utils.Err(err))
		}
		GlobalConfig.Upload.ChunkSize = chunkSize
	}

	if tempConfig.Upload.MaxFileSize != "" {
		maxFileSize, err := ParseQuotaString(tempConfig.Upload.MaxFileSize)
		if err != nil {
			utils.Fatal("解析最大文件大小时出错",
				utils.String("file", configFile),
				utils.String("max_file_size", tempConfig.Upload.MaxFileSize),
				utils.Err(err))
		}
		GlobalConfig.Upload.MaxFileSize = maxFileSize
	}

	// 上传开关: 默认开启，YAML 中显式设置 enabled: false 时才关闭
	GlobalConfig.Upload.Enabled = true
	if tempConfig.Upload.Enabled != nil {
		GlobalConfig.Upload.Enabled = *tempConfig.Upload.Enabled
	}
	GlobalConfig.Upload.URLUpload = tempConfig.Upload.URLUpload

	// 解析临时文件配置（基于IP）
	GlobalConfig.Storage.Temp = TempConfig{
		Enabled:           tempConfig.Storage.Temp.Enabled,
		Path:              tempConfig.Storage.Temp.Path,
		Quota:             tempConfig.Storage.Temp.Quota,
		DefaultExpireDays: tempConfig.Storage.Temp.DefaultExpireDays,
		DeleteOnDownload:  tempConfig.Storage.Temp.DeleteOnDownload,
	}

	// 解析预览配置
	GlobalConfig.Preview = PreviewConfig{
		AllowMimes:    tempConfig.Preview.AllowMimes,
		AllowExts:     tempConfig.Preview.AllowExts,
		MaxInlineSize: tempConfig.Preview.MaxInlineSize,
		TextChunkSize: tempConfig.Preview.TextChunkSize,
	}

	// 解析日志配置
	GlobalConfig.Log = tempConfig.Log
	GlobalConfig.OpenAPI = tempConfig.OpenAPI

	// 向后兼容：新 account 字段为空时回退读取旧字段
	var oldConfig struct {
		Server struct {
			WebDAV struct {
				PublicUsername string `yaml:"public_username"`
			} `yaml:"webdav"`
		} `yaml:"server"`
		Security struct {
			ReservedUsernames []string `yaml:"reserved_usernames"`
		} `yaml:"security"`
	}
	if err := yaml.Unmarshal(data, &oldConfig); err == nil {
		if GlobalConfig.Account.Anonymous.Username == "" {
			GlobalConfig.Account.Anonymous.Username = oldConfig.Server.WebDAV.PublicUsername
		}
		if len(GlobalConfig.Account.ReservedUsernames) == 0 {
			GlobalConfig.Account.ReservedUsernames = oldConfig.Security.ReservedUsernames
		}
	}

	// 验证必要配置
	validateConfig()

	utils.Info("配置文件读取成功",
		utils.Int("root_dirs", len(GlobalConfig.Storage.Public.RootDirs)),
		utils.String("log_level", GlobalConfig.Log.Level))
}

// validateConfig 验证配置完整性
func validateConfig() {
	if GlobalConfig.Upload.ChunkSize <= 0 {
		utils.Fatal("配置错误: upload.chunk_size 必须大于 0",
			utils.String("file", ConfigPath))
	}

	// 默认允许上传
	if !GlobalConfig.Upload.Enabled {
		utils.Info("配置: 公共目录上传已关闭")
	}
	// 配额为 0 表示无限制
	if GlobalConfig.Storage.Temp.DefaultExpireDays < 0 {
		utils.Fatal("配置错误: storage.temp.default_expire_days 不能为负数",
			utils.String("file", ConfigPath))
	}

	// Open API 配置默认值
	if GlobalConfig.OpenAPI.RequestsPerMinute <= 0 {
		GlobalConfig.OpenAPI.RequestsPerMinute = 60
	}
	if GlobalConfig.OpenAPI.BurstSize <= 0 {
		GlobalConfig.OpenAPI.BurstSize = 20
	}
	if GlobalConfig.OpenAPI.IPAccessMode == "" {
		GlobalConfig.OpenAPI.IPAccessMode = "disable"
	}
}

// initAppName 初始化应用名称（HTML 模板现在由路由层处理）
func initAppName(appInfo APPConfig) {
	appName := "浮栈"
	if name := appInfo.Name; name != "" {
		appName = name
	}
	utils.Info("应用名称初始化完成", utils.String("app_name", appName))
}

// GetWebDir 获取前端构建目录
func GetWebDir() string {
	return filepath.Join(WorkDir, "web")
}

// initRootNames 获取根目录名称映射
// isSetup 表示是否处于初始化引导阶段，setup模式下某些检查会降级为警告
func initRootNames(isSetup bool) {
	if len(GlobalConfig.Storage.Public.RootDirs) == 0 {
		utils.Fatal("根目录配置为空", utils.String("reason", "必须配置至少一个根目录"))
	}
	for i, rootDirConfig := range GlobalConfig.Storage.Public.RootDirs {
		// 展开 ${work_dir} 占位符，并将展开后的绝对路径回写配置（DTO/运行时保持一致）
		expanded := ExpandWorkDirPath(rootDirConfig.Path)
		GlobalConfig.Storage.Public.RootDirs[i].Path = expanded
		rootDirConfig.Path = expanded

		// 检查根目录是否存在
		if _, err := os.Stat(rootDirConfig.Path); os.IsNotExist(err) {
			if isSetup {
				// 初始化引导阶段，记录警告但不阻止启动
				utils.Warn("配置的根目录不存在（将在设置引导中配置）",
					utils.String("path", rootDirConfig.Path))
			} else {
				utils.Fatal("配置的根目录不存在", utils.String("path", rootDirConfig.Path), utils.String("reason", "请检查配置文件"))
			}
			continue
		}
		dirName := filepath.Base(rootDirConfig.Path)
		if dirName == "." || dirName == "" || dirName == string(os.PathSeparator) || dirName == ".." || dirName == "/" {
			utils.Fatal("无效的根目录名称（路径最后一段为空）",
				utils.String("path", rootDirConfig.Path))
			continue
		}
		if _, exists := RootNames[dirName]; exists {
			if isSetup {
				utils.Warn("重复的根目录名称（将在设置引导中修正）", utils.String("name", dirName))
			} else {
				utils.Fatal("重复的根目录名称", utils.String("name", dirName))
			}
			continue
		}
		RootNames[dirName] = rootDirConfig.Path
	}
	utils.Info("根目录名称映射创建成功", utils.Int("count", len(RootNames)))
}

// initTempDirectories 初始化临时文件目录
// isSetup 表示是否处于初始化引导阶段，setup模式下失败时记录警告而非终止
func initTempDirectories(isSetup bool) {
	if !GlobalConfig.Storage.Private.Enabled {
		return
	}

	// 展开 ${work_dir} 占位符
	GlobalConfig.Storage.Private.Path = ExpandWorkDirPath(GlobalConfig.Storage.Private.Path)
	GlobalConfig.Storage.Temp.Path = ExpandWorkDirPath(GlobalConfig.Storage.Temp.Path)

	// 确保临时文件根目录存在
	if err := os.MkdirAll(GlobalConfig.Storage.Private.Path, 0755); err != nil {
		if isSetup {
			utils.Warn("创建私有文件根目录失败（将在设置引导中配置）",
				utils.String("path", GlobalConfig.Storage.Private.Path), utils.Err(err))
		} else {
			utils.Fatal("创建私有文件根目录失败", utils.Err(err))
		}
		return
	}

	// 创建必要的子目录
	directories := []string{
		filepath.Join(GlobalConfig.Storage.Private.Path, "files"),    // 存储完整文件
		filepath.Join(GlobalConfig.Storage.Private.Path, "users"),    // 用户目录
		filepath.Join(GlobalConfig.Storage.Private.Path, "metadata"), // 元数据目录
	}

	for _, dir := range directories {
		if err := os.MkdirAll(dir, 0755); err != nil {
			if isSetup {
				utils.Warn("创建私有文件子目录失败（将在设置引导中配置）",
					utils.String("path", dir), utils.Err(err))
			} else {
				utils.Fatal("创建私有文件子目录失败", utils.String("path", dir), utils.Err(err))
			}
		}
	}

	utils.Info("私有文件目录初始化成功", utils.String("path", GlobalConfig.Storage.Private.Path))
}

// DoInitWithConfig 初始化操作（支持自定义配置路径）
func DoInitWithConfig(webAssets embed.FS, configPath string) Config {
	EmbedAssets = webAssets
	DefaultConfigFS = webAssets
	// 配置文件定位：显式指定则用指定路径，否则启动目录优先、二进制所在目录次之
	if configPath != "" {
		ConfigPath = configPath
	} else {
		ConfigPath = resolveDefaultConfigPath()
	}

	// 默认以配置所在目录作为工作目录；
	// 若配置内显式指定了 work_dir，applyConfiguredWorkDir 会进一步覆盖它
	WorkDir = filepath.Dir(ConfigPath)

	createConfigFile()
	readConfig()
	// 读取配置后，若配置了 work_dir 则以配置覆盖默认值
	applyConfiguredWorkDir()
	// 在工作目录下创建 data 目录（容纳默认 sqlite、搜索索引等数据）
	createDataDir()
	InitLogging()
	initAppName(GlobalConfig.App)

	// 根据是否已初始化决定检查级别
	isSetup := !GlobalConfig.App.Initialized
	if isSetup {
		utils.Info("系统未初始化，进入设置引导模式")
	}
	initRootNames(isSetup)
	initTempDirectories(isSetup)
	utils.Info("程序初始化完成", utils.String("work_dir", WorkDir), utils.String("config", ConfigPath))
	return GlobalConfig
}

// GetConfig 获取当前配置的副本（线程安全）
func GetConfig() Config {
	configMu.RLock()
	defer configMu.RUnlock()
	return GlobalConfig
}

// IsPortInUse 检查端口是否被占用
func IsPortInUse(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return true
	}
	defer listener.Close()
	return false
}

// ParseQuotaString 解析配额字符串为字节数
func ParseQuotaString(quotaStr string) (int64, error) {
	if quotaStr == "" {
		return 0, nil
	}

	// 转换为大写以便处理
	quotaStr = strings.ToUpper(strings.TrimSpace(quotaStr))

	// 定义单位映射
	units := map[string]int64{
		"B":  1,
		"K":  1024,
		"KB": 1024,
		"M":  1024 * 1024,
		"MB": 1024 * 1024,
		"G":  1024 * 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
		"T":  1024 * 1024 * 1024 * 1024,
		"TB": 1024 * 1024 * 1024 * 1024,
	}

	// 查找单位
	var numberStr string
	var unitStr string

	// 从字符串末尾开始查找单位
	for i := len(quotaStr) - 1; i >= 0; i-- {
		char := quotaStr[i]
		if char >= '0' && char <= '9' || char == '.' {
			numberStr = quotaStr[:i+1]
			unitStr = quotaStr[i+1:]
			break
		}
	}

	// 如果没有找到数字部分，说明整个字符串都是数字
	if numberStr == "" {
		numberStr = quotaStr
	}

	// 解析数字部分
	number, err := parseFloat(numberStr)
	if err != nil {
		return 0, fmt.Errorf("invalid quota format: %s", quotaStr)
	}

	// 获取单位乘数
	multiplier := int64(1)
	if unitStr != "" {
		var exists bool
		multiplier, exists = units[unitStr]
		if !exists {
			return 0, fmt.Errorf("unsupported quota unit: %s", unitStr)
		}
	}

	// 计算最终字节数
	result := int64(number * float64(multiplier))
	return result, nil
}

// parseFloat 解析浮点数字符串
func parseFloat(s string) (float64, error) {
	var result float64
	_, err := fmt.Sscanf(s, "%f", &result)
	return result, err
}

var onSaveHooks []func()

// AddOnSaveHook 注册配置保存后的钩子
func AddOnSaveHook(hook func()) {
	onSaveHooks = append(onSaveHooks, hook)
}

// RunOnSaveHooks 触发热更新钩子
func RunOnSaveHooks() {
	for _, hook := range onSaveHooks {
		hook()
	}
}
