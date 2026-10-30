package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/ftp"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"

	"github.com/gin-gonic/gin"
)

// newRouter 创建 Gin Engine 与 FTP Handler（热重启时重新创建）
func (rt *runCtx) newRouter() (*gin.Engine, *ftp.FTPHandler) {
	// 创建 FTP 处理器（每次热重启时重建）
	ftpHandler := ftp.NewFTPHandler()
	ftpHandler.SetAuthUser(ftp.AuthUserFunc(func(username, password string) (string, error) {
		user, err := rt.authService.LoginByPassword(username, password)
		if err != nil {
			return "", err
		}
		return user.UUID, nil
	}))
	// 设置 FTP 操作录制回调（走全局有界记录器，避免每次操作起新协程）
	ftpHandler.SetRecordFunc(func(action, filePath, fileName, rootName, fileTypeTag, clientIP, userUUID string, fileSize int64) {
		// FTP 操作访问日志（独立写入 ftp.log，便于跟踪与分析）
		utils.FTPAccess("FTP操作",
			utils.String("action", action),
			utils.String("root", rootName),
			utils.String("path", filePath),
			utils.String("file", fileName),
			utils.Int64("size", fileSize),
			utils.String("client_ip", clientIP),
			utils.String("user", userUUID),
		)
		rec := &models.OperationRecord{
			FileName: fileName,
			FilePath: filePath,
			// FullPath = /RootName + FilePath
			FullPath:   "/" + rootName + filePath,
			RootName:   rootName,
			FileSize:   fileSize,
			ClientIP:   clientIP,
			UserID:     userUUID,
			Action:     action,
			UploadTime: utils.Now(),
		}
		if !services.SubmitRecord(rec) {
			if err := rt.recordRepo.Create(rec); err != nil {
				utils.Debug("FTP操作记录失败",
					utils.String("action", action),
					utils.String("file", filePath),
					utils.Err(err))
			}
		}
	})

	// 注入服务层后端能力（权限/索引/配额），使 FTP 与 Web 上传体系共用同一套规则
	privatePath := ""
	if appconfig.GlobalConfig.Storage.Private.Enabled {
		privatePath = appconfig.GlobalConfig.Storage.Private.Path
	}
	ftpHandler.SetBackend(ftp.NewFtpBackend(rt.db, rt.indexService, privatePath))
	// 注入公开多根目录的存储后端映射：远端后端（s3/minio/oss/webdav）经 Storage
	// 接口访问对象存储，本地后端不注入（保持物理路径语义）
	ftpHandler.SetStorages(rt.publicStorages)

	// 创建 Gin 路由
	r := gin.New()

	// 添加监控中间件
	r.Use(middleware.MonitoringMiddleware())

	// 静态资源压缩与长缓存（仅对 /assets、/scalar/assets 生效）
	r.Use(middleware.StaticAssetsMiddleware())

	// 添加安全头中间件
	r.Use(middleware.SecurityHeadersMiddleware())

	// 添加跨域中间件
	r.Use(middleware.CORSMiddleware())

	// 添加请求ID中间件
	r.Use(middleware.RequestIDMiddleware())

	// 添加审计日志中间件（在 Auth 之前，捕获请求基础信息）
	r.Use(middleware.AuditMiddleware())

	// 添加自定义恢复中间件（使用结构化日志记录 panic 信息）
	r.Use(gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		utils.Error("请求处理异常",
			utils.String("path", c.Request.URL.Path),
			utils.String("method", c.Request.Method),
			utils.Any("panic", recovered),
		)
		c.AbortWithStatus(http.StatusInternalServerError)
	}))

	// 获取前端构建目录
	webDir := appconfig.GetWebDir()
	// scalar（OpenAPI 文档页）独立构建目录，与 web 同级
	scalarDir := filepath.Join(filepath.Dir(webDir), "scalar")

	// 检查是否存在磁盘上的前端构建
	useDiskAssets := false
	if _, err := os.Stat(webDir); err == nil {
		useDiskAssets = true
		utils.Info("使用磁盘上的前端资源", utils.String("dir", webDir))
	}

	// SPA 首页路由
	if useDiskAssets {
		// 磁盘资源
		assetsDir := filepath.Join(webDir, "assets")
		r.GET("/", func(c *gin.Context) {
			if isCLIRequest(c) {
				rt.cliHandlers.HandleCli(c)
				return
			}
			// IE 浏览器自动跳转到 /lite 轻量版页面
			if redirectIEToLite(c) {
				return
			}
			c.File(filepath.Join(webDir, "index.html"))
		})
		r.Static("/assets", assetsDir)
		// scalar（OpenAPI 文档页）独立构建
		r.Static("/scalar/assets", filepath.Join(scalarDir, "assets"))
		// 独立页面
		r.GET("/scalar.html", func(c *gin.Context) {
			c.File(filepath.Join(scalarDir, "scalar.html"))
		})
	} else {
		// 嵌入资源
		r.GET("/", func(c *gin.Context) {
			if isCLIRequest(c) {
				rt.cliHandlers.HandleCli(c)
				return
			}
			// IE 浏览器自动跳转到 /lite 轻量版页面
			if redirectIEToLite(c) {
				return
			}
			data, err := webAssets.ReadFile("web/index.html")
			if err != nil {
				utils.HandleNotFound(c, "index.html not found")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
		})
		r.StaticFS("/assets", http.FS(assetsFS))
		// scalar（OpenAPI 文档页）独立构建
		r.StaticFS("/scalar/assets", http.FS(scalarAssetsFS))
		// 独立页面
		r.GET("/scalar.html", func(c *gin.Context) {
			data, err := webAssets.ReadFile("scalar/scalar.html")
			if err != nil {
				utils.HandleNotFound(c, "scalar.html not found")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
		})
	}

	// SPA fallback: 支持 Vue Router 的 /* 路径
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		// favicon 处理
		if path == "/favicon.ico" {
			c.Status(http.StatusNotFound)
			return
		}
		// API 路径返回 404
		if strings.HasPrefix(path, "/api/") {
			utils.HandleNotFound(c, "API 路由未找到")
			return
		}
		// 其他路径返回 index.html（SPA fallback）
		// IE 浏览器自动跳转到 /lite 轻量版页面
		if redirectIEToLite(c) {
			return
		}
		c.File(filepath.Join(webDir, "index.html"))
	})

	// API 路由组：/api/v1 下按作用域划分，路径本身即可说明调用方
	//   /api/v1/web     —— Web 界面（含管理后台）直接调用
	//   /api/v1/cli     —— 命令行工具
	//   /api/v1/webdav  —— WebDAV 端点
	//   /api/v1/web/ready —— 就绪探针（经 LoginGate，强制登录后需登录可见）
	api := r.Group("/api/v1")
	web := api.Group("/web")
	// 「强制登录」闸门：开启（account.require_login）后仅登录用户可访问 Web 接口，
	// 白名单内的路由（登录/注册、初始化引导、临时分享等）保持匿名例外；开关关闭时全放行。
	web.Use(rt.authMiddleware.LoginGate(loginGateWhiteList()...))
	rt.registerApiRoutes(r, api, web)

	// 顶层快捷入口与 Open API 路由组
	rt.registerTopLevelRoutes(r)

	return r, ftpHandler
}
