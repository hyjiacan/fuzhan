package appconfig

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

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
