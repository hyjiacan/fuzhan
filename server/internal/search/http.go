package search

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"fuzhan/internal/utils"
)

// Handler 基于检索索引的 HTTP 处理器（自动补全 / 拼写纠错）。
// 供主界面搜索框下拉联想与纠错提示调用。
type Handler struct {
	idx *SearchIndex
}

// NewHandler 创建检索 HTTP 处理器
func NewHandler(idx *SearchIndex) *Handler {
	return &Handler{idx: idx}
}

// parseLimit 解析 limit 查询参数，保证在 [1, max] 内，非法时回退默认值
func parseLimit(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

// Autocomplete 自动补全建议。
// GET /api/v1/search-suggest?q=前缀&limit=数量
// 返回 data: []string（最常见的关键词，而非完整文件名）。
func (h *Handler) Autocomplete(c *gin.Context) {
	q := c.Query("q")
	if q == "" || h.idx == nil {
		utils.HandleSuccess(c, http.StatusOK, "", []string{})
		return
	}
	names, err := h.idx.SuggestKeywords(q, parseLimit(c.Query("limit"), 10, 30))
	if err != nil {
		utils.HandleError(c, http.StatusInternalServerError, 500, "自动补全失败", err.Error())
		return
	}
	if names == nil {
		names = []string{}
	}
	utils.HandleSuccess(c, http.StatusOK, "", names)
}

// SpellCheck 拼写纠错建议。
// GET /api/v1/search-spellcheck?q=单词&limit=数量
// 返回 data: []string（最相似的关键词候选，而非完整文件名）。
func (h *Handler) SpellCheck(c *gin.Context) {
	q := c.Query("q")
	if q == "" || h.idx == nil {
		utils.HandleSuccess(c, http.StatusOK, "", []string{})
		return
	}
	names, err := h.idx.SuggestCorrectKeywords(q, parseLimit(c.Query("limit"), 5, 10))
	if err != nil {
		utils.HandleError(c, http.StatusInternalServerError, 500, "拼写纠错失败", err.Error())
		return
	}
	if names == nil {
		names = []string{}
	}
	utils.HandleSuccess(c, http.StatusOK, "", names)
}
