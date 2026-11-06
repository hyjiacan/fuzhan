package lite

import (
	"net/http"

	"fuzhan/internal/auth"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/search"
	"fuzhan/internal/services"
	"fuzhan/pkg/jwt"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Handlers 轻量版页面处理器（纯后端服务器渲染，兼容老旧浏览器）
type Handlers struct {
	SearchService *services.SearchService
	SearchIndex   *search.SearchIndex
	AuthService   *services.AuthService
	DB            *gorm.DB
}

// NewHandlers 创建轻量版页面处理器实例
func NewHandlers(searchService *services.SearchService, authService *services.AuthService, db *gorm.DB) *Handlers {
	return &Handlers{
		SearchService: searchService,
		AuthService:   authService,
		DB:            db,
	}
}

// recordLoginLog 写入登录/注册日志（成功与失败均记录），与 web 端共享同一逻辑。
func (h *Handlers) recordLoginLog(c *gin.Context, userID, username, method, status, failReason string) {
	auth.RecordLoginLog(h.DB, c, userID, username, method, status, failReason)
}

// Handle 处理 /lite 服务器渲染浏览/检索请求
func (h *Handlers) Handle(c *gin.Context) {
	Handle(c.Writer, h.withDisplayName(c.Request), h.SearchService, h.SearchIndex)
}

// withDisplayName 解析当前登录用户的展示名（昵称 > 姓名 > 用户名）并注入请求上下文，
// 保证轻量版页头与 Vue 端显示一致。
func (h *Handlers) withDisplayName(r *http.Request) *http.Request {
	claims, err := jwt.ParseJWT(middleware.TokenFromRequest(r))
	if err != nil || h.DB == nil {
		return r
	}
	var user models.User
	if err := h.DB.Where("uuid = ?", claims.UUID).First(&user).Error; err != nil {
		return r
	}
	return WithDisplayName(r, user.DisplayNameLabel())
}

// HandleAll 是 /lite 组唯一的 catch-all 入口。gin 不允许同一前缀下同时存在
// 静态段与 catch-all（/lite/login 与 /lite/*path 会触发路由树 panic），
// 因此登录/注册/登出与浏览检索统一经 /*path 接收，此处按 path+method 手动分发。
// 鉴权仍由 LoginGate（按 URL path 判定的 LiteAuthWhiteList）在中间件层完成。
func (h *Handlers) HandleAll(c *gin.Context) {
	p := c.Request.URL.Path
	switch {
	case p == "/lite/login" && c.Request.Method == http.MethodGet:
		h.HandleLoginPage(c)
	case p == "/lite/login" && c.Request.Method == http.MethodPost:
		h.HandleLoginSubmit(c)
	case p == "/lite/register" && c.Request.Method == http.MethodGet:
		h.HandleRegisterPage(c)
	case p == "/lite/register" && c.Request.Method == http.MethodPost:
		h.HandleRegisterSubmit(c)
	case p == "/lite/logout" && c.Request.Method == http.MethodPost:
		h.HandleLogout(c)
	default:
		h.Handle(c)
	}
}
