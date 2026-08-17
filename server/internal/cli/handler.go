package cli

import (
	"path/filepath"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/services"

	"github.com/gin-gonic/gin"
)

// CLIHandlers CLI 处理器
type CLIHandlers struct {
	FileService   *services.FileService
	SearchService *services.SearchService
}

// NewCLIHandlers 创建 CLI 处理器实例
func NewCLIHandlers(fileService *services.FileService, searchService *services.SearchService) *CLIHandlers {
	return &CLIHandlers{
		FileService:   fileService,
		SearchService: searchService,
	}
}

// HandleCli 处理CLI请求
func (ch *CLIHandlers) HandleCli(c *gin.Context) {
	HandleCli(c.Writer, c.Request)
}

// CliSearch 处理CLI搜索请求
func (ch *CLIHandlers) CliSearch(c *gin.Context) {
	hashMap := loadHashMap(ch.SearchService)
	CliSearch(c.Writer, c.Request, hashMap)
}

// CliList 处理CLI列表请求
func (ch *CLIHandlers) CliList(c *gin.Context) {
	hashMap := loadHashMap(ch.SearchService)
	CliList(c.Writer, c.Request, hashMap)
}

// InstallScript 生成 shell 脚本
func (ch *CLIHandlers) InstallScript(c *gin.Context) {
	HandleInstallScript(c.Writer, c.Request)
}

// loadHashMap 从数据库加载所有文件的 xxh3 哈希映射
func loadHashMap(svc *services.SearchService) map[string]string {
	if svc == nil {
		return nil
	}
	combined := make(map[string]string)
	for _, rootDir := range appconfig.GlobalConfig.Storage.Public.RootDirs {
		rootName := filepath.Base(rootDir.Path)
		m, err := svc.LoadHashMap(rootName)
		if err == nil {
			for k, v := range m {
				combined[k] = v
			}
		}
	}
	if len(combined) == 0 {
		return nil
	}
	return combined
}