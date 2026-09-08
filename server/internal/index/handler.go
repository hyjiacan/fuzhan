package index

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fuzhan/internal/utils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
)

// Handler 文件索引 HTTP 处理器
type Handler struct {
	svc *Service
}

// NewHandler 创建索引处理器
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// ListRecords 分页查询文件记录
// GET /api/v1/admin/index/records
// Query: page, pageSize, query, rootName, status, sort, order
func (h *Handler) ListRecords(c *gin.Context) {
	var query ListRecordsQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.HandleBadRequest(c, "请求参数格式错误", err.Error())
		return
	}

	records, total, err := h.svc.ListRecords(query)
	if err != nil {
		response.HandleInternalServerError(c, "查询失败: "+err.Error())
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"records":  records,
		"total":    total,
		"page":     query.Page,
		"pageSize": query.PageSize,
	})
}

// GetStats 获取文件统计
// GET /api/v1/admin/index/stats
func (h *Handler) GetStats(c *gin.Context) {
	stats, err := h.svc.GetStats()
	if err != nil {
		response.HandleInternalServerError(c, "获取统计失败: "+err.Error())
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", stats)
}

// TriggerScan 触发全量扫描
// POST /api/v1/admin/index/scan?scope=public
// scope: public / temp / private（默认 public）
func (h *Handler) TriggerScan(c *gin.Context) {
	scope := ScanScope(c.DefaultQuery("scope", "public"))

	progress := h.svc.GetScanProgressByScope(scope)
	if progress.Status == ScanStatusRunning {
		response.HandleCustomError(c, http.StatusConflict, response.CodeInternalError,
			string(scope)+" 扫描正在进行中")
		return
	}

	if err := h.svc.StartScanByScope(scope, "manual"); err != nil {
		response.HandleInternalServerError(c, "启动扫描失败: "+err.Error())
		return
	}

	response.HandleSuccess(c, http.StatusOK, string(scope)+" 扫描已启动", nil)
}

// GetScanProgress 获取扫描进度
// GET /api/v1/admin/index/scan/progress?scope=public
func (h *Handler) GetScanProgress(c *gin.Context) {
	scope := ScanScope(c.DefaultQuery("scope", "public"))
	progress := h.svc.GetScanProgressByScope(scope)
	response.HandleSuccess(c, http.StatusOK, "", progress.View())
}

// GetScanStatus 获取扫描状态（供 Footer 状态栏轮询）
// GET /api/v1/admin/index/scan/status
// 无需 admin 权限，前端 Footer 需要公开访问
func (h *Handler) GetScanStatus(c *gin.Context) {
	status := h.svc.GetScanStatus()
	response.HandleSuccess(c, http.StatusOK, "", status)
}

// TriggerFullScan 触发全量扫描（所有 scope）
// POST /api/v1/admin/index/scan/trigger
func (h *Handler) TriggerFullScan(c *gin.Context) {
	// 检查是否有正在进行的扫描
	runningScopes := []string{}
	for _, scope := range []ScanScope{ScanScopePublic, ScanScopeTemp, ScanScopePrivate} {
		progress := h.svc.GetScanProgressByScope(scope)
		if progress.Status == ScanStatusRunning {
			runningScopes = append(runningScopes, string(scope))
		}
	}

	if len(runningScopes) > 0 {
		response.HandleCustomError(c, http.StatusConflict, response.CodeInternalError,
			fmt.Sprintf("扫描正在进行中: %v", runningScopes))
		return
	}

	// 后台异步执行全部 scope（public → temp → private）
	go func() {
		defer func() {
			if r := recover(); r != nil {
				utils.Error("全量扫描异常", utils.String("panic", fmt.Sprintf("%v", r)))
			}
		}()

		scopes := []ScanScope{ScanScopePublic, ScanScopeTemp, ScanScopePrivate}
		for _, scope := range scopes {
			progress := h.svc.GetScanProgressByScope(scope)
			if progress.Status == ScanStatusRunning {
				continue
			}

			if err := h.svc.StartScanByScope(scope, "manual"); err != nil {
				// 扫描启动失败，记录日志，如果返回的是 success 应该被记录
				_ = err
				continue
			}

			// 等待当前 scope 完成
			for {
				p := h.svc.GetScanProgressByScope(scope)
				if p.Status == ScanStatusCompleted || p.Status == ScanStatusFailed {
					break
				}
				time.Sleep(2 * time.Second)
			}
		}
	}()

	response.HandleSuccess(c, http.StatusOK, "全量扫描已启动 (public → temp → private)", nil)
}

// TriggerConsistencyCheck 触发一致性校验
// POST /api/v1/admin/index/check
func (h *Handler) TriggerConsistencyCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 24*time.Hour)
	defer cancel()

	report, err := h.svc.RunConsistencyCheck(ctx)
	if err != nil {
		response.HandleInternalServerError(c, "一致性校验失败: "+err.Error())
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", report)
}

// ListDuplicates 查询重复文件分组
// GET /api/v1/admin/index/duplicates
// Query: minSize, page, pageSize, sort, order
func (h *Handler) ListDuplicates(c *gin.Context) {
	var query DuplicateQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		response.HandleBadRequest(c, "请求参数格式错误", err.Error())
		return
	}

	groups, total, err := h.svc.ListDuplicateGroups(query)
	if err != nil {
		response.HandleInternalServerError(c, "查询重复文件失败: "+err.Error())
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"groups":   groups,
		"total":    total,
		"page":     query.Page,
		"pageSize": query.PageSize,
	})
}

// UpdateNotes 更新文件备注
// PUT /api/v1/admin/index/records/:id/notes
func (h *Handler) UpdateNotes(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		response.HandleBadRequest(c, "无效的记录ID", nil)
		return
	}

	var req UpdateNotesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}

	if err := h.svc.UpdateNotes(uint(id), req.Notes); err != nil {
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}

	response.HandleSuccess(c, http.StatusOK, "备注已更新", nil)
}

// FindFileRecord 根据路径查找文件记录
// GET /api/v1/files/record?fileName=xxx&rootName=xxx&filePath=xxx
func (h *Handler) FindFileRecord(c *gin.Context) {
	fileName := c.Query("fileName")
	rootName := c.Query("rootName")
	filePath := c.Query("filePath")
	// URL 解码，处理中文和特殊字符
	filePath, _ = url.QueryUnescape(filePath)

	record, err := h.svc.FindRecordByPath(fileName, rootName, filePath)
	if err != nil {
		response.HandleInternalServerError(c, "查询失败: "+err.Error())
		return
	}
	if record == nil {
		response.HandleSuccess(c, http.StatusOK, "", gin.H{"record": nil})
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"record": record,
	})
}

// SearchFileRecords 搜索文件记录（用于 autocomplete）
// GET /api/v1/files/search-records?q=xxx
func (h *Handler) SearchFileRecords(c *gin.Context) {
	query := c.Query("q")
	// 限制搜索词长度
	if len(query) > 200 {
		query = strings.ToValidUTF8(query[:200], "")
	}
	if query == "" {
		response.HandleBadRequest(c, "搜索关键词不能为空", nil)
		return
	}

	records, err := h.svc.SearchFileRecords(query, 20)
	if err != nil {
		response.HandleInternalServerError(c, "搜索失败: "+err.Error())
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"records": records,
	})
}

// PublicUpdateNotes 公开更新文件备注（无需 admin）
// PUT /api/v1/files/records/:id/notes
func (h *Handler) PublicUpdateNotes(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		response.HandleBadRequest(c, "无效的记录ID", nil)
		return
	}

	var req UpdateNotesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}

	if err := h.svc.UpdateNotes(uint(id), req.Notes); err != nil {
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}

	response.HandleSuccess(c, http.StatusOK, "备注已更新", nil)
}

// PublicGetNotes 公开获取文件备注
// GET /api/v1/files/records/:id/notes
func (h *Handler) PublicGetNotes(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		response.HandleBadRequest(c, "无效的记录ID", nil)
		return
	}

	record, err := h.svc.GetRecord(uint(id))
	if err != nil {
		response.HandleNotFound(c, "文件记录不存在")
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"id":    record.ID,
		"notes": record.Notes,
	})
}

// DeleteRecord 删除索引记录
// DELETE /api/v1/admin/index/records/:id
func (h *Handler) DeleteRecord(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		response.HandleBadRequest(c, "无效的记录ID", nil)
		return
	}

	if err := h.svc.DeleteRecord(uint(id)); err != nil {
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}

	response.HandleSuccess(c, http.StatusOK, "记录已删除", nil)
}

// KeepDuplicate 保留指定文件，自动删除同哈希的其他重复文件
// POST /api/v1/admin/index/duplicates/:id/keep
func (h *Handler) KeepDuplicate(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		response.HandleBadRequest(c, "无效的记录ID", nil)
		return
	}

	result, err := h.svc.KeepDuplicate(uint(id))
	if err != nil {
		response.HandleInternalServerError(c, "保留失败: "+err.Error())
		return
	}

	response.HandleSuccess(c, http.StatusOK, fmt.Sprintf("已保留文件，删除了 %d 个重复文件", len(result.DeletedFiles)), result)
}
