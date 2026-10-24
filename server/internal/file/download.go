package file

import (
	"net/http"
	"net/url"
	"strings"

	"fuzhan/internal/downloadticket"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// DownloadHandlers 下载相关处理器
type DownloadHandlers struct {
	DownloadService *services.DownloadService
}

// NewDownloadHandlers 创建下载处理器实例
func NewDownloadHandlers(downloadService *services.DownloadService) *DownloadHandlers {
	return &DownloadHandlers{
		DownloadService: downloadService,
	}
}

// DownloadFile 下载文件
func (dh *DownloadHandlers) DownloadFile(c *gin.Context) {
	dh.DownloadService.DownloadFile(c.Writer, c.Request)
}

// AdminDownloadFile 管理页面下载文件（不记录历史）
func (dh *DownloadHandlers) AdminDownloadFile(c *gin.Context) {
	dh.DownloadService.AdminDownloadFile(c.Writer, c.Request)
}

// DownloadTicketRequest 换取一次性下载票据的请求。
// 登录凭证只走 Authorization 请求头；换得的票据（URL 上的 t=）供浏览器原生标签/
// 新窗口/外部下载工具直连下载与内联预览时使用。
type DownloadTicketRequest struct {
	// Path 下载/预览的逻辑路径：/download/根目录/子路径（或 /download/<16位哈希>），
	// 或管理页下载 /api/v1/web/admin/download/根目录/子路径。使用未编码形式。
	Path string `json:"path" binding:"required"`
	// Mode 票据用途：download（默认）/ preview
	Mode string `json:"mode"`
}

// IssueTicket 以当前登录会话签发一枚一次性下载票据，返回可直接打开的 URL。
func (dh *DownloadHandlers) IssueTicket(c *gin.Context) {
	var req DownloadTicketRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
		return
	}
	path := strings.TrimSpace(req.Path)
	if !strings.HasPrefix(path, "/download/") && !strings.HasPrefix(path, "/api/v1/web/admin/download/") {
		response.HandleBadRequest(c, "无效的下载路径", nil)
		return
	}

	mode := downloadticket.ModeDownload
	if strings.EqualFold(req.Mode, "preview") {
		mode = downloadticket.ModePreview
	}

	ticket, err := signFileTicket(c, path, mode)
	if err != nil {
		utils.HandleInternalServerError(c, "签发下载票据失败")
		return
	}

	// preview 模式：URL 携带 preview=true，使下载服务按内联预览处理
	// （不设置 attachment Content-Disposition、不累加下载计数、不写下载历史）。
	urlStr := path + "?" + downloadticket.TicketParamName + "=" + url.QueryEscape(ticket)
	if mode == downloadticket.ModePreview {
		urlStr += "&preview=true"
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"url":    urlStr,
		"ticket": ticket,
	})
}
