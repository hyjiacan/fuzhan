package openapi

import (
    "fmt"
    "net/http"
    "net/url"
    "path/filepath"
    "strconv"
    "strings"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
    "fuzhan/internal/index"
    "fuzhan/internal/models"
    "fuzhan/pkg/response"
)

// escapeLike 转义 LIKE 通配符（% _ 及转义符自身），须配合 SQL 的 ESCAPE '\' 使用，
// 避免用户输入中的 %/_ 被当作通配符而放大匹配范围。
func escapeLike(s string) string {
    if !strings.ContainsAny(s, `\%_`) {
        return s
    }
    var b strings.Builder
    for _, r := range s {
        if r == '\\' || r == '%' || r == '_' {
            b.WriteByte('\\')
        }
        b.WriteRune(r)
    }
    return b.String()
}

// Handler Open API 处理器
type Handler struct {
    db        *gorm.DB
    indexSvc  *index.Service
    depSvc    *index.DependencyService
    rootNames map[string]string
}

// NewHandler 创建 Open API 处理器
func NewHandler(db *gorm.DB, indexSvc *index.Service, depSvc *index.DependencyService, rootNames map[string]string) *Handler {
    return &Handler{
        db:        db,
        indexSvc: indexSvc,
        depSvc:    depSvc,
        rootNames: rootNames,
    }
}

// ==================== FR-3.1: 文件列表 API ====================

// ListFilesQuery 文件列表查询参数
type ListFilesQuery struct {
    Path     string `form:"path"`
    Page     int    `form:"page"`
    PageSize int    `form:"pageSize"`
    Sort     string `form:"sort"`
    Order    string `form:"order"`
}

// ListFiles GET /api/open/v1/files/list
func (h *Handler) ListFiles(c *gin.Context) {
    var query ListFilesQuery
    if err := c.ShouldBindQuery(&query); err != nil {
        response.HandleBadRequest(c, "请求参数格式错误", err.Error())
        return
    }

    if query.Path == "" {
        query.Path = "/"
    }
    if query.Page <= 0 {
        query.Page = 1
    }
    if query.PageSize <= 0 || query.PageSize > 100 {
        query.PageSize = 50
    }
    if query.Sort == "" {
        query.Sort = "name"
    }
    if query.Order == "" {
        query.Order = "asc"
    }

    cleanPath := filepath.ToSlash(query.Path)

    dbQuery := h.db.Model(&models.FileRecordPublic{}).
        Where("status = ?", models.FileStatusActive)

    if cleanPath != "/" {
        parts := splitPath(cleanPath)
        if len(parts) > 0 {
            rootName := parts[0]
            subPath := "/" + strings.Join(parts[1:], "/")
            subPath = filepath.ToSlash(subPath)
            if subPath == "/" {
                dbQuery = dbQuery.Where("root_name = ?", rootName)
            } else {
                dbQuery = dbQuery.Where("root_name = ? AND (file_path = ? OR file_path LIKE ? ESCAPE '\\')",
                    rootName, subPath, escapeLike(subPath)+"/%")
            }
        }
    } else {
        dbQuery = dbQuery.Where("root_name IN (?)", h.getRootNameList())
    }

    var total int64
    dbQuery.Count(&total)

    sortField := map[string]string{"name": "file_name", "time": "mod_time", "size": "file_size"}
    orderField := sortField["name"]
    if f, ok := sortField[query.Sort]; ok {
        orderField = f
    }
    orderDir := "ASC"
    if query.Order == "desc" {
        orderDir = "DESC"
    }

    offset := (query.Page - 1) * query.PageSize
    var records []models.FileRecordPublic
    dbQuery.Order(orderField + " " + orderDir).
        Offset(offset).Limit(query.PageSize).Find(&records)

    files := make([]gin.H, 0)
    for _, r := range records {
        files = append(files, gin.H{
            "name":      r.FileName,
            "path":      r.FullPath,
            "full_path": r.FullPath,
            "root":      r.RootName,
            "size":      r.FileSize,
            "is_dir":    r.IsDir,
            "mod_time":  r.ModTime,
            "xxh3_hash": r.Xxh3Hash,
        })
    }

    response.HandleSuccess(c, http.StatusOK, "", gin.H{
        "path":  cleanPath,
        "files": files,
        "pagination": gin.H{
            "page":      query.Page,
            "page_size": query.PageSize,
            "total":     total,
        },
    })
}

// ==================== FR-3.2: 文件搜索 API ====================

// SearchFilesQuery 搜索查询参数
type SearchFilesQuery struct {
    Query    string `form:"query" binding:"required"`
    Root     string `form:"root"`
    Page     int    `form:"page"`
    PageSize int    `form:"pageSize"`
}

// SearchFiles GET /api/open/v1/files/search
func (h *Handler) SearchFiles(c *gin.Context) {
    var query SearchFilesQuery
    if err := c.ShouldBindQuery(&query); err != nil {
        response.HandleBadRequest(c, "缺少搜索关键词 query", nil)
        return
    }

    if query.Page <= 0 {
        query.Page = 1
    }
    if query.PageSize <= 0 || query.PageSize > 100 {
        query.PageSize = 50
    }

    dbQuery := h.db.Model(&models.FileRecordPublic{}).
        Where("status = ?", models.FileStatusActive).
        Where("(file_name LIKE ? ESCAPE '\\' OR notes LIKE ? ESCAPE '\\')",
            "%"+escapeLike(query.Query)+"%", "%"+escapeLike(query.Query)+"%")

    if query.Root != "" {
        dbQuery = dbQuery.Where("root_name = ?", query.Root)
    }

    var total int64
    dbQuery.Count(&total)

    offset := (query.Page - 1) * query.PageSize
    var records []models.FileRecordPublic
    dbQuery.Order("file_name ASC").
        Offset(offset).Limit(query.PageSize).Find(&records)

    files := make([]gin.H, 0)
    for _, r := range records {
        files = append(files, gin.H{
            "id":        r.ID,
            "name":      r.FileName,
            "path":      r.FullPath,
            "full_path": r.FullPath,
            "root":      r.RootName,
            "size":      r.FileSize,
            "is_dir":    r.IsDir,
            "mod_time":  r.ModTime,
            "xxh3_hash": r.Xxh3Hash,
        })
    }

    response.HandleSuccess(c, http.StatusOK, "", gin.H{
        "results": files,
        "pagination": gin.H{
            "page":      query.Page,
            "page_size": query.PageSize,
            "total":     total,
        },
    })
}

// ==================== FR-3.3: 文件下载 API ====================

// DownloadFile GET /api/open/v1/files/download/*path
func (h *Handler) DownloadFile(c *gin.Context) {
    filePath := c.Param("path")
    if filePath == "" {
        response.HandleBadRequest(c, "缺少文件路径", nil)
        return
    }

    // URL 解码，处理中文和特殊字符
    filePath, _ = url.QueryUnescape(filePath)

    disposition := c.DefaultQuery("disposition", "attachment")
    cleanPath := filepath.ToSlash(filePath)
    parts := splitPath(cleanPath)
    if len(parts) < 2 {
        response.HandleBadRequest(c, "路径格式错误: 需要 /rootName/path/to/file", nil)
        return
    }

    rootName := parts[0]
    relPath := "/" + strings.Join(parts[1:], "/")
    relPath = filepath.ToSlash(relPath)

    var record models.FileRecordPublic
    if err := h.db.Where("root_name = ? AND file_path = ? AND status = ?",
        rootName, relPath, models.FileStatusActive).First(&record).Error; err != nil {
        response.HandleNotFound(c, "文件不存在: "+rootName+relPath)
        return
    }

    if record.IsDir {
        response.HandleBadRequest(c, "不能下载目录", nil)
        return
    }

    rootPath, ok := h.rootNames[rootName]
    if !ok {
        response.HandleInternalServerError(c, "根目录配置不存在")
        return
    }

    fullPath := filepath.Join(rootPath, record.FilePath)
    // RFC 5987 文件名编码，兼容非 ASCII 字符
    safeFilename := url.QueryEscape(record.FileName)
    // 清洗 filename="..." 内的引号/反斜杠/控制字符，防 CRLF 注入或畸形响应头。
    // 现代浏览器优先使用已编码的 filename*，此值仅作降级。
    cleanName := strings.Map(func(r rune) rune {
        if r == '"' || r == '\\' || r == '\r' || r == '\n' {
            return '_'
        }
        return r
    }, record.FileName)
    c.Header("Content-Disposition", fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, cleanName, safeFilename))

	// 公共文件下载次数累加
	h.db.Model(&models.FileRecordPublic{}).
		Where("full_path = ?", record.FullPath).
		UpdateColumn("download_count", gorm.Expr("download_count + 1"))

	c.File(fullPath)
}

// ==================== FR-3.5: 重复文件查询 API ====================

// ListDuplicates GET /api/open/v1/files/duplicates
func (h *Handler) ListDuplicates(c *gin.Context) {
    minSize, _ := strconv.ParseInt(c.DefaultQuery("minSize", "0"), 10, 64)
    page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
    pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))

    // 对分页参数做边界约束，避免传超大 pageSize 造成资源占用（与其他 openapi 接口保持一致）
    if minSize < 0 {
        minSize = 0
    }
    if page < 1 {
        page = 1
    }
    if pageSize < 1 || pageSize > 100 {
        pageSize = 50
    }

    query := index.DuplicateQuery{
        MinSize:  minSize,
        Page:     page,
        PageSize: pageSize,
    }

    groups, total, err := h.indexSvc.ListDuplicateGroups(query)
    if err != nil {
        response.HandleInternalServerError(c, "查询重复文件失败: "+err.Error())
        return
    }

    response.HandleSuccess(c, http.StatusOK, "", gin.H{
        "groups": groups,
        "pagination": gin.H{
            "page":      page,
            "page_size": pageSize,
            "total":     total,
        },
    })
}

// ==================== FR-3.6: 文件备注查询 API ====================

// GetNotes GET /api/open/v1/files/notes
func (h *Handler) GetNotes(c *gin.Context) {
    path := c.Query("path")
    if path == "" {
        response.HandleBadRequest(c, "缺少文件路径", nil)
        return
    }

    // URL 解码，处理中文和特殊字符
    path, _ = url.QueryUnescape(path)

    cleanPath := filepath.ToSlash(path)
    parts := splitPath(cleanPath)
    if len(parts) < 2 {
        response.HandleBadRequest(c, "路径格式错误", nil)
        return
    }

    rootName := parts[0]
    relPath := "/" + strings.Join(parts[1:], "/")
    relPath = filepath.ToSlash(relPath)

    var record models.FileRecordPublic
    if err := h.db.Where("root_name = ? AND file_path = ? AND status = ?",
        rootName, relPath, models.FileStatusActive).First(&record).Error; err != nil {
        response.HandleNotFound(c, "文件不存在: "+rootName+relPath)
        return
    }

    response.HandleSuccess(c, http.StatusOK, "", gin.H{
        "path":  cleanPath,
        "notes": record.Notes,
    })
}

// GetDependencyTree GET /api/open/v1/files/{id}/depends
func (h *Handler) GetDependencyTree(c *gin.Context) {
    idStr := c.Param("id")
    id, err := strconv.ParseUint(idStr, 10, 64)
    if err != nil || id == 0 {
        response.HandleBadRequest(c, "无效的记录ID", nil)
        return
    }

    // 先检查文件记录是否存在
    _, err = h.indexSvc.GetRecord(uint(id))
    if err != nil {
        response.HandleNotFound(c, "文件记录不存在")
        return
    }

    tree, err := h.depSvc.GetDependencyTree(uint(id))
    if err != nil {
        response.HandleBadRequest(c, "查询依赖树失败", nil)
        return
    }

    response.HandleSuccess(c, http.StatusOK, "", gin.H{
        "root": tree,
    })
}

// ==================== FR-3.10: OpenAPI 文档 ====================

// GetOpenAPISpec GET /api/open/v1/openapi.json
func (h *Handler) GetOpenAPISpec(c *gin.Context) {
    spec := GenerateOpenAPISpec()
    c.JSON(http.StatusOK, spec)
}

// GetDocsPage GET /api/open/v1/docs
func (h *Handler) GetDocsPage(c *gin.Context) {
    c.Redirect(http.StatusMovedPermanently, "/scalar.html")
}

// ==================== 辅助方法 ====================

func (h *Handler) getRootNameList() []string {
    names := make([]string, 0, len(h.rootNames))
    for name := range h.rootNames {
        names = append(names, name)
    }
    return names
}

func splitPath(p string) []string {
    parts := make([]string, 0)
    for _, part := range strings.Split(p, "/") {
        if part != "" {
            parts = append(parts, part)
        }
    }
    return parts
}
