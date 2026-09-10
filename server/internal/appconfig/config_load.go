package appconfig

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"fuzhan/internal/utils"
)

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
