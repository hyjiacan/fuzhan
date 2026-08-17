package index

import (
    "errors"
    "net/http"
    "strconv"

    "github.com/gin-gonic/gin"
    "fuzhan/pkg/response"
)

// DependencyHandler 文件依赖 HTTP 处理器
type DependencyHandler struct {
    svc *DependencyService
}

// NewDependencyHandler 创建依赖处理器
func NewDependencyHandler(svc *DependencyService) *DependencyHandler {
    return &DependencyHandler{svc: svc}
}

// CreateDependency 创建依赖关系
// POST /api/v1/admin/index/dependencies
func (h *DependencyHandler) CreateDependency(c *gin.Context) {
    var req struct {
        FileRecordID uint   `json:"fileRecordId" binding:"required"`
        DependsOnID  uint   `json:"dependsOnId" binding:"required"`
        Relation     string `json:"relation" binding:"required"`
        Description  string `json:"description"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        response.HandleBadRequest(c, "请求数据格式错误", err.Error())
        return
    }

    if err := h.svc.CreateDependency(req.FileRecordID, req.DependsOnID, req.Relation, req.Description); err != nil {
        response.HandleBadRequest(c, err.Error(), nil)
        return
    }

    response.HandleSuccess(c, http.StatusCreated, "依赖关系已创建", nil)
}

// ListDependencies 分页查询所有依赖关系
// GET /api/v1/admin/index/dependencies
func (h *DependencyHandler) ListDependencies(c *gin.Context) {
    var query DependencyListQuery
    if err := c.ShouldBindQuery(&query); err != nil {
        response.HandleBadRequest(c, "请求参数格式错误", err.Error())
        return
    }

    deps, total, err := h.svc.ListDependencies(query)
    if err != nil {
        response.HandleInternalServerError(c, "查询失败: "+err.Error())
        return
    }

    response.HandleSuccess(c, http.StatusOK, "", gin.H{
        "dependencies": deps,
        "total":        total,
        "page":         query.Page,
        "pageSize":     query.PageSize,
    })
}

// GetDependenciesByRecord 查询指定文件的所有依赖
// GET /api/v1/admin/index/records/:id/dependencies
func (h *DependencyHandler) GetDependenciesByRecord(c *gin.Context) {
    idStr := c.Param("id")
    id, err := strconv.ParseUint(idStr, 10, 64)
    if err != nil || id == 0 {
        response.HandleBadRequest(c, "无效的记录ID", nil)
        return
    }

    upstream, downstream, err := h.svc.ListDependenciesByRecord(uint(id))
    if err != nil {
        response.HandleInternalServerError(c, "查询失败: "+err.Error())
        return
    }

    response.HandleSuccess(c, http.StatusOK, "", gin.H{
        "upstream":   upstream,
        "downstream": downstream,
    })
}

// GetDependencyTree 获取文件依赖树
// GET /api/v1/files/depends/:id
func (h *DependencyHandler) GetDependencyTree(c *gin.Context) {
    idStr := c.Param("id")
    id, err := strconv.ParseUint(idStr, 10, 64)
    if err != nil || id == 0 {
        response.HandleBadRequest(c, "无效的记录ID", nil)
        return
    }

    tree, err := h.svc.GetDependencyTree(uint(id))
    if err != nil {
        if errors.Is(err, ErrFileRecordNotFound) {
            response.HandleNotFound(c, "文件记录不存在")
        } else {
            response.HandleBadRequest(c, "查询依赖树失败", nil)
        }
        return
    }

    response.HandleSuccess(c, http.StatusOK, "", gin.H{
        "root": tree,
    })
}

// CreateDependencyPublic 公开创建依赖关系（无需 admin）
// POST /api/v1/files/dependencies
func (h *DependencyHandler) CreateDependencyPublic(c *gin.Context) {
    var req struct {
        FileRecordID uint   `json:"fileRecordId" binding:"required"`
        DependsOnID  uint   `json:"dependsOnId" binding:"required"`
        Relation     string `json:"relation" binding:"required"`
        Description  string `json:"description"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        response.HandleBadRequest(c, "请求数据格式错误", err.Error())
        return
    }

    if err := h.svc.CreateDependency(req.FileRecordID, req.DependsOnID, req.Relation, req.Description); err != nil {
        response.HandleBadRequest(c, err.Error(), nil)
        return
    }

    response.HandleSuccess(c, http.StatusCreated, "依赖关系已创建", nil)
}

// DeleteDependencyPublic 公开删除依赖关系（无需 admin）
// DELETE /api/v1/files/dependencies/:id
func (h *DependencyHandler) DeleteDependencyPublic(c *gin.Context) {
    idStr := c.Param("id")
    id, err := strconv.ParseUint(idStr, 10, 64)
    if err != nil || id == 0 {
        response.HandleBadRequest(c, "无效的依赖ID", nil)
        return
    }

    if err := h.svc.DeleteDependency(uint(id)); err != nil {
        response.HandleBadRequest(c, err.Error(), nil)
        return
    }

    response.HandleSuccess(c, http.StatusOK, "依赖关系已删除", nil)
}

// DeleteDependency 删除依赖关系
// DELETE /api/v1/admin/index/dependencies/:id
func (h *DependencyHandler) DeleteDependency(c *gin.Context) {
    idStr := c.Param("id")
    id, err := strconv.ParseUint(idStr, 10, 64)
    if err != nil || id == 0 {
        response.HandleBadRequest(c, "无效的依赖ID", nil)
        return
    }

    if err := h.svc.DeleteDependency(uint(id)); err != nil {
        response.HandleBadRequest(c, err.Error(), nil)
        return
    }

    response.HandleSuccess(c, http.StatusOK, "依赖关系已删除", nil)
}
