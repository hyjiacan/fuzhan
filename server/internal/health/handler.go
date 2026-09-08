package health

import (
	"net/http"
	"os"
	"runtime"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// HealthHandler 健康检查处理器
type HealthHandler struct {
	db *gorm.DB
}

// NewHealthHandler 创建健康检查处理器实例
func NewHealthHandler(db ...*gorm.DB) *HealthHandler {
	h := &HealthHandler{}
	if len(db) > 0 {
		h.db = db[0]
	}
	return h
}

// CheckResult 单项检查结果
type CheckResult struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// HealthCheckResponse 健康检查响应结构体
type HealthCheckResponse struct {
	Status     string                 `json:"status"`
	Version    string                 `json:"version"`
	GoVersion  string                 `json:"goVersion"`
	Goroutines int                    `json:"goroutines"`
	Checks     map[string]CheckResult `json:"checks"`
	Timestamp  string                 `json:"timestamp"`
}

// Health 健康检查端点
func (hh *HealthHandler) Health(c *gin.Context) {
	checks := make(map[string]CheckResult)
	overallStatus := "healthy"

	// 检查数据库
	if hh.db != nil {
		sqlDB, err := hh.db.DB()
		if err != nil {
			checks["database"] = CheckResult{Status: "error", Message: "获取数据库连接失败"}
			overallStatus = "unhealthy"
			utils.AppLogger().Error("健康检查: 获取数据库连接失败", zap.Error(err))
		} else if err := sqlDB.Ping(); err != nil {
			checks["database"] = CheckResult{Status: "error", Message: "数据库连接失败"}
			overallStatus = "unhealthy"
			utils.AppLogger().Error("健康检查: 数据库Ping失败", zap.Error(err))
		} else {
			checks["database"] = CheckResult{Status: "ok"}
		}
	} else {
		checks["database"] = CheckResult{Status: "skipped", Message: "数据库未初始化"}
		utils.AppLogger().Warn("健康检查: 数据库未初始化")
	}

	// 检查存储目录
	storageChecks := []CheckResult{}
	for _, rootDir := range appconfig.GlobalConfig.Storage.Public.RootDirs {
		dirPath := rootDir.Path
		info, err := os.Stat(dirPath)
		if err != nil {
			if os.IsNotExist(err) {
				storageChecks = append(storageChecks, CheckResult{
					Status:  "error",
					Message: "目录不存在: " + dirPath,
				})
				utils.AppLogger().Error("健康检查: 存储目录不存在",
					zap.String("path", dirPath),
				)
			} else {
				storageChecks = append(storageChecks, CheckResult{
					Status:  "error",
					Message: "无权限访问: " + dirPath,
				})
				utils.AppLogger().Error("健康检查: 存储目录无权限访问",
					zap.String("path", dirPath),
					zap.Error(err),
				)
			}
			if overallStatus == "healthy" {
				overallStatus = "degraded"
			}
		} else if !info.IsDir() {
			storageChecks = append(storageChecks, CheckResult{
				Status:  "error",
				Message: "不是目录: " + dirPath,
			})
			utils.AppLogger().Error("健康检查: 存储路径不是目录",
				zap.String("path", dirPath),
			)
			if overallStatus == "healthy" {
				overallStatus = "degraded"
			}
		} else {
			storageChecks = append(storageChecks, CheckResult{
				Status:  "ok",
				Message: dirPath,
			})
			utils.AppLogger().Debug("健康检查: 存储目录正常",
				zap.String("path", dirPath),
			)
		}
	}
	checks["storage"] = CheckResult{Status: "ok"}
	if len(storageChecks) > 0 {
		hasError := false
		for _, sc := range storageChecks {
			if sc.Status != "ok" {
				hasError = true
				break
			}
		}
		if hasError {
			checks["storage"] = CheckResult{Status: "error", Message: "部分存储目录不可用"}
		}
	}

	resp := HealthCheckResponse{
		Status:     overallStatus,
		Version:    "1.0.0",
		GoVersion:  runtime.Version(),
		Goroutines: runtime.NumGoroutine(),
		Checks:     checks,
		Timestamp:  utils.Now().Format(time.RFC3339),
	}

	response.HandleSuccess(c, http.StatusOK, "服务运行正常", resp)
}

// ReadyResponse 就绪检查响应结构体
type ReadyResponse struct {
	Status  string            `json:"status"`
	Checks  map[string]string `json:"checks"`
	Version string            `json:"version"`
}

// Ready 就绪检查端点
func (hh *HealthHandler) Ready(c *gin.Context) {
	// 检查各种依赖服务的状态
	checks := make(map[string]string)

	// 检查配置是否加载成功
	if appconfig.GlobalConfig.Server.Host != "" && (appconfig.GlobalConfig.Server.HTTP.Port > 0 || appconfig.GlobalConfig.Server.HTTPS.Port > 0) {
		checks["config"] = "ok"
	} else {
		checks["config"] = "failed"
	}

	// 检查根目录是否存在
	if len(appconfig.GlobalConfig.Storage.Public.RootDirs) > 0 {
		checks["rootDirs"] = "ok"
	} else {
		checks["rootDirs"] = "failed"
	}

	// 检查临时目录（如果启用）
	if appconfig.GlobalConfig.Storage.Private.Enabled {
		if appconfig.GlobalConfig.Storage.Private.Path != "" {
			checks["tempDir"] = "ok"
		} else {
			checks["tempDir"] = "failed"
		}
	} else {
		checks["tempDir"] = "skipped"
	}

	// 确定整体状态
	status := "ready"
	for _, checkStatus := range checks {
		if checkStatus == "failed" {
			status = "not ready"
			break
		}
	}

	resp := ReadyResponse{
		Status:  status,
		Checks:  checks,
		Version: "1.0.0",
	}

	response.HandleSuccess(c, http.StatusOK, "就绪检查完成", resp)
}
