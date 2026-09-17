package lite

import (
	"fuzhan/internal/search"
	"fuzhan/internal/services"

	"github.com/gin-gonic/gin"
)

// Handlers 轻量版页面处理器（纯后端服务器渲染，兼容老旧浏览器）
type Handlers struct {
	SearchService *services.SearchService
	SearchIndex   *search.SearchIndex
}

// NewHandlers 创建轻量版页面处理器实例
func NewHandlers(searchService *services.SearchService) *Handlers {
	return &Handlers{
		SearchService: searchService,
	}
}

// Handle 处理 /lite 服务器渲染浏览/检索请求
func (h *Handlers) Handle(c *gin.Context) {
	Handle(c.Writer, c.Request, h.SearchService, h.SearchIndex)
}
