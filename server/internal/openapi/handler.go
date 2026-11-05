package openapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/constants"
	"fuzhan/internal/index"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/search"
	"fuzhan/internal/services"
	"fuzhan/internal/storage"
	"fuzhan/internal/utils"
	"fuzhan/pkg/pathutils"
	"fuzhan/pkg/response"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GetScopeCatalog 返回可授予的 OpenAPI 接口权限目录（管理端按分组渲染）
// GET /api/v1/web/admin/open-api/scopes
func (h *Handler) GetScopeCatalog(c *gin.Context) {
	response.HandleSuccess(c, http.StatusOK, "", gin.H{"scopes": models.OpenAPIScopeCatalog()})
}

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
	// storages 公开多根目录的远程存储后端映射（rootName → Storage）；未注册时回退本地磁盘
	storages map[string]storage.Storage
	// uploadSvc 分片上传会话服务（与 Web 端公共上传共用会话/分片/落盘流程）
	uploadSvc *services.UploadSessionService
	// fileSvc 文件重命名/移动/删除服务（与 Web 端复用同一套落盘逻辑）
	fileSvc *services.FileService
	// searchIdx 文件名检索索引，用于搜索建议词（逻辑与 Web 端一致）
	searchIdx *search.SearchIndex
	// chunkSize 分片大小（字节），由配置 upload.chunk_size 决定
	chunkSize int64
}

// NewHandler 创建 Open API 处理器
func NewHandler(db *gorm.DB, indexSvc *index.Service, depSvc *index.DependencyService,
	rootNames map[string]string, storages map[string]storage.Storage,
	uploadSvc *services.UploadSessionService, fileSvc *services.FileService,
	searchIdx *search.SearchIndex, chunkSize int64) *Handler {
	return &Handler{
		db:        db,
		indexSvc:  indexSvc,
		depSvc:    depSvc,
		rootNames: rootNames,
		storages:  storages,
		uploadSvc: uploadSvc,
		fileSvc:   fileSvc,
		searchIdx: searchIdx,
		chunkSize: chunkSize,
	}
}

// hasContextScope 判断当前鉴权上下文是否包含指定 scope。
func hasContextScope(c *gin.Context, required string) bool {
	v, ok := c.Get(scopesContextKey)
	if !ok {
		return false
	}
	switch scopes := v.(type) {
	case string:
		return models.ScopeContains(scopes, required)
	case []string:
		return models.ScopeContains(strings.Join(scopes, ","), required)
	default:
		return false
	}
}

// optionalIntQuery 读取可选的整数查询参数。
// 未提供（或为空）时返回默认值；一旦提供但非法（非整数或超出范围）则写入 400 响应并返回 ok=false，
// 不做静默回退，避免调用方拿到与入参不符的结果。
func optionalIntQuery(c *gin.Context, name string, def, min, max int64) (int64, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return def, true
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		response.HandleBadRequest(c, name+" 必须为整数", nil)
		return 0, false
	}
	if v < min || (max > 0 && v > max) {
		if max > 0 {
			response.HandleBadRequest(c, fmt.Sprintf("%s 取值范围为 %d-%d", name, min, max), nil)
		} else {
			response.HandleBadRequest(c, fmt.Sprintf("%s 不能小于 %d", name, min), nil)
		}
		return 0, false
	}
	return v, true
}

// optionalEnumQuery 读取可选的枚举查询参数：未提供时返回默认值，提供非法值时写入 400 响应。
func optionalEnumQuery(c *gin.Context, name, def string, allowed ...string) (string, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return def, true
	}
	for _, a := range allowed {
		if raw == a {
			return raw, true
		}
	}
	response.HandleBadRequest(c, fmt.Sprintf("%s 仅支持 %s", name, strings.Join(allowed, " / ")), nil)
	return "", false
}

// parseBoolForm 解析可选布尔表单字段：缺省为 false，提供非法值时返回错误。
func parseBoolForm(c *gin.Context, name string) (bool, error) {
	raw := strings.TrimSpace(c.PostForm(name))
	if raw == "" {
		return false, nil
	}
	return strconv.ParseBool(raw)
}

// normalizeOpenAPIPath 校验并规范化 /rootName[/sub] 路径，拒绝 . 与 .. 片段。
func normalizeOpenAPIPath(raw string) (string, error) {
	p := filepath.ToSlash(strings.TrimSpace(raw))
	if p == "" {
		return "", errors.New("路径不能为空")
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	parts := splitPath(p)
	for _, part := range parts {
		if part == "." || part == ".." {
			return "", errors.New("路径不能包含 . 或 ..")
		}
	}
	if len(parts) == 0 {
		return "/", nil
	}
	return "/" + strings.Join(parts, "/"), nil
}

// handleFileServiceError 把文件服务的错误映射为对应的 HTTP 状态码：
// 资源不存在 404、目标冲突 409、访问/落盘失败 500、其余视为参数错误 400。
func handleFileServiceError(c *gin.Context, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "不存在"):
		response.HandleNotFound(c, msg)
	case strings.Contains(msg, "已存在"):
		response.HandleErrorCompat(c, http.StatusConflict, msg, nil)
	case strings.Contains(msg, "无法访问"), strings.Contains(msg, "失败"):
		response.HandleInternalServerError(c, msg)
	default:
		response.HandleBadRequest(c, msg, nil)
	}
}

// validHexHash 判断是否为 16 位十六进制字符串（xxh3 64 位哈希的标准输出格式）。
func validHexHash(s string) bool {
	if len(s) != 16 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// validChunkChecksum 校验分片校验值：可选；提供时必须是 1-16 位十六进制字符串。
func validChunkChecksum(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 16 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// handleUploadChunkError 把分片上传服务错误映射为对应 HTTP 状态码。
func handleUploadChunkError(c *gin.Context, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "会话不存在"):
		response.HandleNotFound(c, msg)
	case strings.Contains(msg, "会话已过期"):
		response.HandleErrorCompat(c, http.StatusConflict, msg+", 请重新 init", nil)
	case strings.Contains(msg, "无效的分片索引"), strings.Contains(msg, "无效的分片大小"),
		strings.Contains(msg, "xxh3校验"):
		response.HandleBadRequest(c, msg, nil)
	case strings.Contains(msg, "超过限制"):
		response.HandleErrorCompat(c, http.StatusRequestEntityTooLarge, msg, nil)
	default:
		response.HandleInternalServerError(c, msg)
	}
}

// ==================== FR-3.1: 文件列表 API ====================

// ListFiles GET /api/open/v1/files/list
func (h *Handler) ListFiles(c *gin.Context) {
	cleanPath := "/"
	if raw := strings.TrimSpace(c.Query("path")); raw != "" {
		var err error
		if cleanPath, err = normalizeOpenAPIPath(raw); err != nil {
			response.HandleBadRequest(c, "path 参数非法: "+err.Error(), nil)
			return
		}
	}

	page, ok := optionalIntQuery(c, "page", 1, 1, 0)
	if !ok {
		return
	}
	pageSize, ok := optionalIntQuery(c, "pageSize", 50, 1, 100)
	if !ok {
		return
	}
	sortBy, ok := optionalEnumQuery(c, "sort", "name", "name", "time", "size")
	if !ok {
		return
	}
	orderBy, ok := optionalEnumQuery(c, "order", "asc", "asc", "desc")
	if !ok {
		return
	}

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
	if err := dbQuery.Count(&total).Error; err != nil {
		response.HandleInternalServerError(c, "查询文件列表失败: "+err.Error())
		return
	}

	sortField := map[string]string{"name": "file_name", "time": "mod_time", "size": "file_size"}
	orderField := sortField[sortBy]
	orderDir := "ASC"
	if orderBy == "desc" {
		orderDir = "DESC"
	}

	offset := (page - 1) * pageSize
	var records []models.FileRecordPublic
	if err := dbQuery.Order(orderField + " " + orderDir).
		Offset(int(offset)).Limit(int(pageSize)).Find(&records).Error; err != nil {
		response.HandleInternalServerError(c, "查询文件列表失败: "+err.Error())
		return
	}

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
			"page":      page,
			"page_size": pageSize,
			"total":     total,
		},
	})
}

// ==================== FR-3.2: 文件搜索 API ====================

// SearchFiles GET /api/open/v1/files/search
// 支持多关键词模糊匹配（空格分隔，全部命中）、扩展名过滤，并返回与 Web 端一致的搜索建议词。
func (h *Handler) SearchFiles(c *gin.Context) {
	rawQuery := c.Query("query")
	keywords := strings.Fields(rawQuery)
	if len(keywords) == 0 {
		response.HandleBadRequest(c, "缺少搜索关键词 query", nil)
		return
	}

	page, ok := optionalIntQuery(c, "page", 1, 1, 0)
	if !ok {
		return
	}
	pageSize, ok := optionalIntQuery(c, "pageSize", 50, 1, 100)
	if !ok {
		return
	}
	exts, err := parseSearchExtensions(c.Query("ext"))
	if err != nil {
		response.HandleBadRequest(c, "ext 参数非法: "+err.Error(), nil)
		return
	}

	dbQuery := h.db.Model(&models.FileRecordPublic{}).
		Where("status = ?", models.FileStatusActive)

	// 逐个关键词 AND 匹配文件名/备注，形成模糊搜索语义
	for _, kw := range keywords {
		pattern := "%" + escapeLike(kw) + "%"
		dbQuery = dbQuery.Where("(file_name LIKE ? ESCAPE '\\' OR notes LIKE ? ESCAPE '\\')", pattern, pattern)
	}

	if len(exts) > 0 {
		conditions := make([]string, 0, len(exts))
		args := make([]interface{}, 0, len(exts))
		for _, ext := range exts {
			conditions = append(conditions, "LOWER(file_name) LIKE ? ESCAPE '\\'")
			args = append(args, "%."+escapeLike(ext))
		}
		dbQuery = dbQuery.Where("is_dir = ?", false).
			Where("("+strings.Join(conditions, " OR ")+")", args...)
	}

	if root := strings.TrimSpace(c.Query("root")); root != "" {
		dbQuery = dbQuery.Where("root_name = ?", root)
	}

	var total int64
	if err := dbQuery.Count(&total).Error; err != nil {
		response.HandleInternalServerError(c, "查询文件失败: "+err.Error())
		return
	}

	offset := (page - 1) * pageSize
	var records []models.FileRecordPublic
	if err := dbQuery.Order("file_name ASC").
		Offset(int(offset)).Limit(int(pageSize)).Find(&records).Error; err != nil {
		response.HandleInternalServerError(c, "查询文件失败: "+err.Error())
		return
	}

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
		"results":     files,
		"suggestions": h.buildSearchSuggestions(rawQuery, 10),
		"pagination": gin.H{
			"page":      page,
			"page_size": pageSize,
			"total":     total,
		},
	})
}

// parseSearchExtensions 规范化扩展名过滤参数：去点、转小写、去重，最多 10 个。
// 提供非法扩展名（空项、超长或含非字母数字字符）时返回错误，由调用方返回 400。
func parseSearchExtensions(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	// 用 Split 而非 FieldsFunc：需要识别 "pdf,,docx" 这类空项，避免把非法输入静默忽略。
	parts := strings.Split(strings.ReplaceAll(raw, ";", ","), ",")
	seen := make(map[string]bool, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(p), "."))
		if ext == "" {
			return nil, errors.New("扩展名不能为空")
		}
		if len(ext) > 20 {
			return nil, fmt.Errorf("扩展名过长: %s", ext)
		}
		for _, r := range ext {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				return nil, fmt.Errorf("扩展名包含非法字符: %s", ext)
			}
		}
		if seen[ext] {
			continue
		}
		seen[ext] = true
		out = append(out, ext)
		if len(out) > 10 {
			return nil, errors.New("最多支持 10 个扩展名")
		}
	}
	return out, nil
}

// buildSearchSuggestions 返回与 Web 端一致的搜索建议词：
// 前缀补全 + 多词相关词 + 拼写纠错，去重后最多 limit 个。
func (h *Handler) buildSearchSuggestions(q string, limit int) []string {
	if h.searchIdx == nil || strings.TrimSpace(q) == "" || limit <= 0 {
		return []string{}
	}
	seen := make(map[string]bool)
	out := make([]string, 0, limit)
	add := func(names []string) {
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if key == strings.ToLower(strings.TrimSpace(q)) || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, name)
			if len(out) >= limit {
				return
			}
		}
	}
	if names, err := h.searchIdx.SuggestKeywords(strings.TrimSpace(q), limit); err == nil {
		add(names)
	}
	if strings.Contains(strings.TrimSpace(q), " ") {
		if names, err := h.searchIdx.SuggestRelatedKeywords(strings.TrimSpace(q), limit); err == nil {
			add(names)
		}
	}
	if len(out) < limit {
		if names, err := h.searchIdx.SuggestCorrectKeywords(strings.TrimSpace(q), limit); err == nil {
			add(names)
		}
	}
	return out
}

// ==================== FR-3.3: 文件下载 API ====================

// DownloadFile GET /api/open/v1/files/download/*path
func (h *Handler) DownloadFile(c *gin.Context) {
	filePath := c.Param("path")
	if filePath == "" {
		response.HandleBadRequest(c, "缺少文件路径", nil)
		return
	}

	// URL 解码，处理中文和特殊字符；非法转义直接拒绝，不能沿用未解码值。
	decoded, err := url.QueryUnescape(filePath)
	if err != nil {
		response.HandleBadRequest(c, "路径包含非法的 URL 转义", nil)
		return
	}
	cleanPath, err := normalizeOpenAPIPath(decoded)
	if err != nil {
		response.HandleBadRequest(c, "路径格式错误: "+err.Error(), nil)
		return
	}

	disposition, ok := optionalEnumQuery(c, "disposition", "attachment", "attachment", "inline")
	if !ok {
		return
	}
	parts := splitPath(cleanPath)
	if len(parts) < 2 {
		response.HandleBadRequest(c, "路径格式错误: 需要 /rootName/path/to/file", nil)
		return
	}

	rootName := parts[0]
	relPath := "/" + strings.Join(parts[1:], "/")

	var record models.FileRecordPublic
	if err := h.db.Where("root_name = ? AND file_path = ? AND status = ?",
		rootName, relPath, models.FileStatusActive).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.HandleNotFound(c, "文件不存在: "+rootName+relPath)
			return
		}
		response.HandleInternalServerError(c, "查询文件失败: "+err.Error())
		return
	}

	if record.IsDir {
		response.HandleBadRequest(c, "不能下载目录", nil)
		return
	}

	rootPath, ok := h.rootNames[rootName]
	if !ok {
		response.HandleNotFound(c, "根目录配置不存在: "+rootName)
		return
	}

	fullPath := filepath.Join(rootPath, record.FilePath)
	// 危险类型（HTML/SVG/XML/JS 等可在同源执行脚本）即使请求 inline 也强制按附件下载，
	// 避免 OpenAPI 成为存储型 XSS 的投放渠道。
	if disposition == "inline" && utils.IsUnsafePreviewExtension(filepath.Ext(record.FileName)) {
		disposition = "attachment"
	}
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

	// 用户配置的下载 MIME 优先；未配置则由 c.File 按扩展名自动推断 Content-Type
	if mt := utils.GetDownloadContentType(filepath.Ext(record.FileName)); mt != "" {
		c.Header("Content-Type", mt)
	}

	// 下载记录是下载次数的唯一真源：此处既要累加计数，也要落一条操作记录，
	// 否则计数会超出记录集（删除记录后重算会凭空减少）。两者使用同一「新下载请求」判定。
	counted := services.IsNewDownloadRequest(c.Request)
	if counted {
		h.db.Model(&models.FileRecordPublic{}).
			Where("full_path = ?", record.FullPath).
			UpdateColumn("download_count", gorm.Expr("download_count + 1"))
	}
	rec := &models.OperationRecord{
		Action:       "download",
		FileName:     record.FileName,
		FilePath:     record.FilePath,
		FullPath:     record.FullPath,
		FileSize:     record.FileSize,
		RootName:     record.RootName,
		ClientIP:     c.ClientIP(),
		FileRecordID: record.ID,
		SourceID:     record.ID,
		SourceType:   models.SourceTypePublic,
		UploadTime:   utils.Now(),
		CreatedAt:    utils.Now(),
	}
	if counted {
		rec.DlCounted = 1
	}
	if !services.SubmitDownloadRecord(rec) {
		if err := h.db.Create(rec).Error; err != nil {
			utils.OpenAPIWarn("记录开放接口下载操作失败", utils.Err(err))
		}
	}

	c.File(fullPath)
}

// ==================== FR-3.5: 重复文件查询 API ====================

// ListDuplicates GET /api/open/v1/files/duplicates
func (h *Handler) ListDuplicates(c *gin.Context) {
	minSize, ok := optionalIntQuery(c, "minSize", 0, 0, 0)
	if !ok {
		return
	}
	page, ok := optionalIntQuery(c, "page", 1, 1, 0)
	if !ok {
		return
	}
	pageSize, ok := optionalIntQuery(c, "pageSize", 50, 1, 100)
	if !ok {
		return
	}

	query := index.DuplicateQuery{
		MinSize:  minSize,
		Page:     int(page),
		PageSize: int(pageSize),
	}

	groups, total, err := h.indexSvc.ListDuplicateGroups(query)
	if err != nil {
		response.HandleInternalServerError(c, "查询重复文件失败: "+err.Error())
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"groups": duplicateGroupsJSON(groups),
		"pagination": gin.H{
			"page":      page,
			"page_size": pageSize,
			"total":     total,
		},
	})
}

// duplicateGroupsJSON 把索引层的重复文件分组转换为 OpenAPI 响应结构。
// OpenAPI 响应字段统一使用 snake_case，索引层模型（同时供 Web 端使用）使用 camelCase。
func duplicateGroupsJSON(groups []index.DuplicateGroup) []gin.H {
	out := make([]gin.H, 0, len(groups))
	for _, g := range groups {
		files := make([]gin.H, 0, len(g.Files))
		for _, f := range g.Files {
			files = append(files, gin.H{
				"id":        f.ID,
				"file_name": f.FileName,
				"file_path": f.FilePath,
				"full_path": f.FullPath,
				"root_name": f.RootName,
				"file_size": f.FileSize,
				"mod_time":  f.ModTime,
			})
		}
		out = append(out, gin.H{
			"xxh3_hash":  g.Xxh3Hash,
			"file_count": g.FileCount,
			"total_size": g.TotalSize,
			"files":      files,
		})
	}
	return out
}

// dependencyTreeJSON 把索引层依赖树转换为 OpenAPI 响应结构（snake_case）。
func dependencyTreeJSON(node *index.DependencyTreeNode) gin.H {
	if node == nil {
		return nil
	}
	children := make([]gin.H, 0, len(node.Children))
	for _, child := range node.Children {
		if child != nil {
			children = append(children, dependencyTreeJSON(child))
		}
	}
	downstream := make([]gin.H, 0, len(node.Downstream))
	for _, item := range node.Downstream {
		if item != nil {
			downstream = append(downstream, dependencyTreeJSON(item))
		}
	}
	return gin.H{
		"id":            node.ID,
		"dependency_id": node.DependencyID,
		"file_name":     node.FileName,
		"file_path":     node.FilePath,
		"full_path":     node.FullPath,
		"root_name":     node.RootName,
		"file_size":     node.FileSize,
		"is_dir":        node.IsDir,
		"relation":      node.Relation,
		"download_url":  node.DownloadURL,
		"children":      children,
		"downstream":    downstream,
	}
}

// ==================== 文件详情 API ====================

// GetFileInfo GET /api/open/v1/files/info
// 支持按文件记录 ID 或完整路径（/rootName/path/to/file）查询单个文件/目录的详细信息。
func (h *Handler) GetFileInfo(c *gin.Context) {
	idStr := strings.TrimSpace(c.Query("id"))
	pathStr := strings.TrimSpace(c.Query("path"))
	if idStr == "" && pathStr == "" {
		response.HandleBadRequest(c, "缺少查询条件: id 或 path 至少提供一个", nil)
		return
	}
	if idStr != "" && pathStr != "" {
		response.HandleBadRequest(c, "id 与 path 只能提供一个", nil)
		return
	}

	var record models.FileRecordPublic
	if idStr != "" {
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil || id == 0 {
			response.HandleBadRequest(c, "无效的记录ID", nil)
			return
		}
		if err := h.db.Where("id = ? AND status = ?", uint(id), models.FileStatusActive).
			First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				response.HandleNotFound(c, "文件记录不存在")
				return
			}
			response.HandleInternalServerError(c, "查询文件记录失败: "+err.Error())
			return
		}
	} else {
		cleanPath, err := normalizeOpenAPIPath(pathStr)
		if err != nil {
			response.HandleBadRequest(c, "path 参数非法: "+err.Error(), nil)
			return
		}
		parts := splitPath(cleanPath)
		if len(parts) < 2 {
			response.HandleBadRequest(c, "路径格式错误: 需要 /rootName/path/to/file", nil)
			return
		}
		rootName := parts[0]
		relPath := "/" + strings.Join(parts[1:], "/")
		if err := h.db.Where("root_name = ? AND file_path = ? AND status = ?",
			rootName, relPath, models.FileStatusActive).First(&record).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				response.HandleNotFound(c, "文件记录不存在: "+rootName+relPath)
				return
			}
			response.HandleInternalServerError(c, "查询文件记录失败: "+err.Error())
			return
		}
	}

	// 依赖树并入文件详情：查询失败不阻塞详情返回，仅返回 depends=null。
	var depTree interface{}
	if h.depSvc != nil && !record.IsDir {
		if tree, err := h.depSvc.GetDependencyTree(record.ID); err == nil {
			depTree = dependencyTreeJSON(tree)
		}
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"id":             record.ID,
		"name":           record.FileName,
		"path":           record.FullPath,
		"full_path":      record.FullPath,
		"root":           record.RootName,
		"file_path":      record.FilePath,
		"size":           record.FileSize,
		"is_dir":         record.IsDir,
		"mod_time":       record.ModTime,
		"xxh3_hash":      record.Xxh3Hash,
		"hash_status":    record.HashStatus,
		"notes":          record.Notes,
		"depends":        depTree,
		"download_count": record.DownloadCount,
		"status":         record.Status,
		"created_at":     record.CreatedAt,
		"updated_at":     record.UpdatedAt,
	})
}

// ==================== 文件存在性查询 API ====================

// CheckFilesExistRequest 文件存在性查询请求
type CheckFilesExistRequest struct {
	// Xxh3 待查询的 xxh3 哈希列表（不区分大小写）
	Xxh3 []string `json:"xxh3" binding:"required,min=1,max=100"`
}

// CheckFilesExist POST /api/open/v1/files/exists
// 按一组 xxh3 哈希批量查询文件是否存在，并返回每个哈希对应的路径。
func (h *Handler) CheckFilesExist(c *gin.Context) {
	var req CheckFilesExistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误: 需要提供 xxh3 数组（1-100 个）", nil)
		return
	}

	// 规范化哈希：去空白、转小写、去重，保持输入顺序
	hashes := make([]string, 0, len(req.Xxh3))
	seen := make(map[string]bool, len(req.Xxh3))
	for _, raw := range req.Xxh3 {
		hash := strings.ToLower(strings.TrimSpace(raw))
		if hash == "" {
			response.HandleBadRequest(c, "xxh3 不能包含空值", nil)
			return
		}
		if !validHexHash(hash) {
			response.HandleBadRequest(c, "xxh3 哈希格式非法: "+hash+"（需 16 位十六进制）", nil)
			return
		}
		if seen[hash] {
			continue
		}
		seen[hash] = true
		hashes = append(hashes, hash)
	}

	var records []models.FileRecordPublic
	if err := h.db.Where("status = ? AND is_dir = ? AND xxh3_hash IN ?",
		models.FileStatusActive, false, hashes).Order("id ASC").Find(&records).Error; err != nil {
		response.HandleInternalServerError(c, "查询文件存在性失败: "+err.Error())
		return
	}

	grouped := make(map[string][]models.FileRecordPublic, len(hashes))
	for _, rec := range records {
		key := strings.ToLower(rec.Xxh3Hash)
		grouped[key] = append(grouped[key], rec)
	}

	results := make([]gin.H, 0, len(hashes))
	for _, hash := range hashes {
		matched := grouped[hash]
		paths := make([]string, 0, len(matched))
		files := make([]gin.H, 0, len(matched))
		for _, rec := range matched {
			paths = append(paths, rec.FullPath)
			files = append(files, gin.H{
				"id":        rec.ID,
				"name":      rec.FileName,
				"path":      rec.FullPath,
				"root":      rec.RootName,
				"file_path": rec.FilePath,
				"size":      rec.FileSize,
				"mod_time":  rec.ModTime,
				"xxh3_hash": rec.Xxh3Hash,
			})
		}
		item := gin.H{
			"xxh3":   hash,
			"exists": len(matched) > 0,
			"paths":  paths,
			"files":  files,
		}
		if len(matched) > 0 {
			item["path"] = matched[0].FullPath
			item["id"] = matched[0].ID
			item["size"] = matched[0].FileSize
		}
		results = append(results, item)
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{"results": results})
}

// ==================== 文件重命名/移动 API ====================

// RenameFileRequest 重命名或移动文件请求
type RenameFileRequest struct {
	Path    string `json:"path" binding:"required"`
	NewName string `json:"newName"` // 仅修改文件名（同目录）
	NewPath string `json:"newPath"` // 修改完整路径（可跨目录）
}

// RenameFile POST /api/open/v1/files/rename
// 支持仅修改文件名（newName）或修改完整路径（newPath），二者至少提供一个。
func (h *Handler) RenameFile(c *gin.Context) {
	var req RenameFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误: path 必填", nil)
		return
	}

	oldPath, err := normalizeOpenAPIPath(req.Path)
	if err != nil {
		response.HandleBadRequest(c, "path 参数非法: "+err.Error(), nil)
		return
	}
	if len(splitPath(oldPath)) < 2 {
		response.HandleBadRequest(c, "路径格式错误: 需要 /rootName/path/to/file", nil)
		return
	}
	newName := strings.TrimSpace(req.NewName)
	newPathRaw := strings.TrimSpace(req.NewPath)
	if newName == "" && newPathRaw == "" {
		response.HandleBadRequest(c, "newName 或 newPath 至少需要提供一项", nil)
		return
	}
	if newName != "" && len(newName) > 255 {
		response.HandleBadRequest(c, "newName 长度不能超过 255", nil)
		return
	}
	newPath := ""
	if newPathRaw != "" {
		newPath, err = normalizeOpenAPIPath(newPathRaw)
		if err != nil {
			response.HandleBadRequest(c, "newPath 参数非法: "+err.Error(), nil)
			return
		}
		if len(splitPath(newPath)) < 2 {
			response.HandleBadRequest(c, "newPath 格式错误: 需要 /rootName/path/to/file", nil)
			return
		}
	}
	if h.fileSvc == nil {
		response.HandleInternalServerError(c, "文件服务不可用")
		return
	}

	if newPath != "" && newName != "" && filepath.Base(newPath) != newName {
		response.HandleBadRequest(c, "newName 与 newPath 的文件名不一致", nil)
		return
	}

	if newPath != "" {
		if err := h.fileSvc.MoveFile(oldPath, newPath); err != nil {
			handleFileServiceError(c, err)
			return
		}
		if oldRoot, oldRel := splitRootPath(oldPath); oldRoot != "" {
			if newRoot, newRel := splitRootPath(newPath); newRoot != "" {
				h.syncIndexMove(oldRoot, oldRel, newRoot, newRel)
				newPath = "/" + newRoot + strings.TrimPrefix(newRel, "/")
			}
		}
	} else {
		if err := h.fileSvc.RenameFile(oldPath, newName); err != nil {
			handleFileServiceError(c, err)
			return
		}
		if root, rel := splitRootPath(oldPath); root != "" {
			newRel := "/" + strings.TrimPrefix(filepath.ToSlash(filepath.Join(filepath.Dir(rel), newName)), "/")
			newPath = "/" + root + newRel
			h.syncIndexMove(root, rel, root, newRel)
		}
	}

	rootName, _ := splitRootPath(newPath)
	response.HandleSuccess(c, http.StatusOK, "重命名成功", gin.H{
		"old_path": oldPath,
		"path":     newPath,
		"name":     filepath.Base(newPath),
		"root":     rootName,
	})
}

// syncIndexMove 文件移动/重命名后的索引同步（跨根时软删旧记录并重建新记录）。
func (h *Handler) syncIndexMove(oldRoot, oldRel, newRoot, newRel string) {
	if h.indexSvc == nil {
		return
	}
	oldRel = "/" + strings.TrimPrefix(filepath.ToSlash(oldRel), "/")
	newRel = "/" + strings.TrimPrefix(filepath.ToSlash(newRel), "/")
	if oldRoot == newRoot {
		if err := h.indexSvc.MoveFile(oldRoot, oldRel, newRel); err != nil {
			utils.Warn("开放接口移动文件后同步索引失败",
				utils.String("root", oldRoot),
				utils.String("from", oldRel),
				utils.String("to", newRel),
				utils.Err(err))
		}
		return
	}
	if err := h.indexSvc.RemoveFile(oldRoot, oldRel); err != nil {
		utils.Warn("开放接口跨根移动软删旧索引失败",
			utils.String("root", oldRoot), utils.String("path", oldRel), utils.Err(err))
	}
	if err := h.indexSvc.SyncFile(newRoot, newRel); err != nil {
		utils.Warn("开放接口跨根移动重建索引失败",
			utils.String("root", newRoot), utils.String("path", newRel), utils.Err(err))
	}
}

// ==================== 文件删除 API ====================

// DeleteFileQuery 删除文件查询参数
type DeleteFileQuery struct {
	Path string `form:"path" binding:"required"`
}

// DeleteFile DELETE /api/open/v1/files?path=/rootName/path/to/file
// 删除文件或目录（目录会递归删除），并同步清理索引记录。
func (h *Handler) DeleteFile(c *gin.Context) {
	var req DeleteFileQuery
	if err := c.ShouldBindQuery(&req); err != nil {
		response.HandleBadRequest(c, "缺少文件路径 path", nil)
		return
	}
	cleanPath, err := normalizeOpenAPIPath(req.Path)
	if err != nil {
		response.HandleBadRequest(c, "path 参数非法: "+err.Error(), nil)
		return
	}
	rootName, relPath := splitRootPath(cleanPath)
	if rootName == "" || relPath == "" {
		response.HandleBadRequest(c, "路径格式错误: 需要 /rootName/path/to/file", nil)
		return
	}
	if h.fileSvc == nil {
		response.HandleInternalServerError(c, "文件服务不可用")
		return
	}

	// 删除前判断目标类型与文件大小：类型决定所需 scope，大小供响应与索引清理使用。
	st, ok := h.storageFor(rootName)
	if !ok {
		response.HandleNotFound(c, "根目录不存在: "+rootName)
		return
	}
	info, err := st.Stat(c.Request.Context(), strings.TrimPrefix(relPath, "/"))
	if err != nil {
		if storage.IsNotExist(err) {
			response.HandleNotFound(c, "文件或目录不存在: "+cleanPath)
			return
		}
		response.HandleInternalServerError(c, "检查目标失败: "+err.Error())
		return
	}
	isDir := info.IsDir
	var fileSize int64
	if !isDir {
		fileSize = info.Size
	}

	// 文件与目录删除权限分开：目录递归删除风险更高，必须单独授权。
	requiredScope := models.ScopeFilesDelete
	targetKind := "文件"
	if isDir {
		requiredScope = models.ScopeFilesDeleteDir
		targetKind = "目录"
	}
	if !hasContextScope(c, requiredScope) {
		response.HandleCustomError(c, http.StatusForbidden, response.CodeForbidden,
			fmt.Sprintf("权限不足：删除%s需要 scope: %s", targetKind, requiredScope))
		return
	}

	if err := h.fileSvc.DeleteFile(cleanPath); err != nil {
		handleFileServiceError(c, err)
		return
	}

	if h.indexSvc != nil {
		var records []models.FileRecordPublic
		var queryErr error
		if isDir {
			// 目录删除：同时清理目录自身的索引记录与其所有子项（前缀匹配）
			prefix := strings.TrimPrefix(relPath, "/") + "/"
			queryErr = h.db.Where("root_name = ? AND (file_path = ? OR file_path LIKE ?) AND status = ?",
				rootName, relPath, "/"+escapeLike(prefix)+"%", models.FileStatusActive).Find(&records).Error
		} else {
			queryErr = h.db.Where("root_name = ? AND file_path = ? AND status = ?",
				rootName, relPath, models.FileStatusActive).Find(&records).Error
		}
		if queryErr != nil {
			utils.Warn("开放接口删除后查找索引记录失败",
				utils.String("root", rootName), utils.String("path", relPath), utils.Err(queryErr))
		} else {
			recordIDs := make([]uint, 0, len(records))
			for _, rec := range records {
				recordIDs = append(recordIDs, rec.ID)
				if err := h.indexSvc.DeleteRecord(rec.ID); err != nil {
					utils.Warn("开放接口删除索引记录失败",
						utils.Int("id", int(rec.ID)),
						utils.String("root", rootName),
						utils.String("path", rec.FilePath),
						utils.Err(err))
				}
			}
			// 与 Web 端删除保持一致：级联清理该文件/目录的上传、下载操作记录
			if removed, err := repositories.NewRecordRepository(h.db).
				DeleteByFile(rootName, relPath, isDir, recordIDs); err != nil {
				utils.Warn("开放接口删除后清理操作记录失败",
					utils.String("root", rootName), utils.String("path", relPath), utils.Err(err))
			} else if removed > 0 {
				utils.Info("开放接口删除已清理操作记录",
					utils.Int("count", int(removed)),
					utils.String("root", rootName), utils.String("path", relPath))
			}
		}
	}

	middleware.LogOperation(c, "file.delete", cleanPath, nil)
	response.HandleSuccess(c, http.StatusOK, "删除成功", gin.H{
		"path":      cleanPath,
		"deleted":   true,
		"is_dir":    isDir,
		"file_size": fileSize,
	})
}

// ==================== 文件上传 API ====================

// UploadFile POST /api/open/v1/files/upload
// 以 multipart/form-data 上传单个文件到指定公共目录：
//   - file: 上传的文件（必填）
//   - path: 目标目录（必填），格式 /rootName[/subdir]
//   - overwrite: 目标已存在时是否覆盖，需具备 open_api:files:overwrite 权限
func (h *Handler) UploadFile(c *gin.Context) {
	if !appconfig.GlobalConfig.Upload.Enabled {
		response.HandleForbidden(c, "公共目录上传功能已关闭")
		return
	}

	if maxSize := appconfig.GlobalConfig.Upload.MaxFileSize; maxSize > 0 {
		// 预留 8MiB 用于 multipart 边界与其他表单项，避免超大请求体占满临时目录
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSize+8<<20)
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.HandleErrorCompat(c, http.StatusRequestEntityTooLarge, "上传内容超出大小限制", nil)
			return
		}
		response.HandleBadRequest(c, "缺少上传文件: 请使用 multipart/form-data 的 file 字段", nil)
		return
	}

	targetDir := strings.TrimSpace(c.PostForm("path"))
	if targetDir == "" {
		response.HandleBadRequest(c, "缺少目标目录 path", nil)
		return
	}
	cleanTargetDir, err := normalizeOpenAPIPath(targetDir)
	if err != nil {
		response.HandleBadRequest(c, "path 参数非法: "+err.Error(), nil)
		return
	}
	parts := splitPath(cleanTargetDir)
	if len(parts) == 0 {
		response.HandleBadRequest(c, "path 格式错误: 需要 /rootName[/subdir]", nil)
		return
	}

	rootName := parts[0]
	if _, ok := h.rootNames[rootName]; !ok {
		response.HandleNotFound(c, "根目录不存在: "+rootName)
		return
	}
	if !appconfig.PublicDirUploadAllowed(rootName) {
		response.HandleForbidden(c, "该目录不允许上传")
		return
	}

	filename := sanitizeUploadFileName(fileHeader.Filename)
	if filename == "" {
		response.HandleBadRequest(c, "文件名无效", nil)
		return
	}
	if !pathutils.IsFileAllowed(appconfig.GlobalConfig.Storage.AllowedExtensions, filename) {
		response.HandleBadRequest(c, "不允许的文件类型: "+filepath.Ext(filename), nil)
		return
	}
	if maxSize := appconfig.GlobalConfig.Upload.MaxFileSize; maxSize > 0 && fileHeader.Size > maxSize {
		response.HandleErrorCompat(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("文件大小超出限制（最大 %d 字节）", maxSize), nil)
		return
	}

	relDir := strings.Join(parts[1:], "/")
	relPath := filename
	if relDir != "" {
		relPath = relDir + "/" + filename
	}

	st, ok := h.storageFor(rootName)
	if !ok {
		response.HandleNotFound(c, "根目录存储不可用: "+rootName)
		return
	}

	// 本地后端禁止上传写入符号链接路径（含 Windows 目录联接），避免穿透根目录
	if rootPath := h.rootNames[rootName]; st.Kind() == "local" && rootPath != "" {
		if err := utils.EnsureNoSymlinkPath(rootPath, filepath.Join(rootPath, filepath.FromSlash(relPath))); err != nil {
			response.HandleBadRequest(c, err.Error(), nil)
			return
		}
	}

	ctx := c.Request.Context()
	clientIP := utils.GetClientIP(c)

	existing := false
	if _, err := st.Stat(ctx, relPath); err == nil {
		existing = true
	} else if !storage.IsNotExist(err) {
		response.HandleInternalServerError(c, "检查目标文件失败: "+err.Error())
		return
	}

	overwrite, err := parseBoolForm(c, "overwrite")
	if err != nil {
		response.HandleBadRequest(c, "overwrite 仅支持 true / false", nil)
		return
	}
	if existing {
		if !overwrite {
			response.HandleErrorCompat(c, http.StatusConflict,
				"目标文件已存在；如需覆盖请传 overwrite=true", nil)
			return
		}
		if !hasContextScope(c, models.ScopeFilesOverwrite) {
			response.HandleForbidden(c, "覆盖已有文件需要 open_api:files:overwrite 权限")
			return
		}
	}

	src, err := fileHeader.Open()
	if err != nil {
		response.HandleInternalServerError(c, "读取上传文件失败: "+err.Error())
		return
	}
	defer src.Close()

	if relDir != "" {
		if err := st.MkdirAll(ctx, relDir); err != nil {
			response.HandleInternalServerError(c, "创建目标目录失败: "+err.Error())
			return
		}
	}
	dst, err := st.OpenWriter(ctx, relPath)
	if err != nil {
		response.HandleInternalServerError(c, "打开目标文件失败: "+err.Error())
		return
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = st.Remove(ctx, relPath)
		response.HandleInternalServerError(c, "写入文件失败: "+err.Error())
		return
	}
	if err := dst.Close(); err != nil {
		_ = st.Remove(ctx, relPath)
		response.HandleInternalServerError(c, "保存文件失败: "+err.Error())
		return
	}

	indexPath := "/" + relPath
	fullPath := "/" + rootName + indexPath

	// 上传记录先落库，保证索引同步后能把 file_record_id 回填到同一条记录
	record := &models.OperationRecord{
		Action:      "upload",
		FileName:    filename,
		FileSize:    fileHeader.Size,
		FilePath:    indexPath,
		FullPath:    fullPath,
		RootName:    rootName,
		FileType:    pathutils.GetFileType(filename),
		ClientIP:    clientIP,
		UserID:      h.callerUserUUID(c),
		UploadType:  models.TargetTypeRegular,
		Overwritten: existing,
		UploadTime:  utils.Now(),
		CreatedAt:   utils.Now(),
	}
	if err := h.db.Create(record).Error; err != nil {
		utils.OpenAPIWarn("记录开放接口上传操作失败", utils.Err(err))
	}

	if h.indexSvc != nil {
		if err := h.indexSvc.SyncFile(rootName, indexPath); err != nil {
			utils.Warn("开放接口上传后同步索引失败",
				utils.String("root", rootName),
				utils.String("path", indexPath),
				utils.Err(err))
		} else {
			h.indexSvc.MarkRecentlySynced(rootName, indexPath)
			if clientIP != "" {
				if uerr := h.db.Model(&models.FileRecordPublic{}).
					Where("root_name = ? AND file_path = ?", rootName, indexPath).
					UpdateColumn("uploader_ip", clientIP).Error; uerr != nil {
					utils.Warn("写入上传者IP失败",
						utils.String("root", rootName),
						utils.String("path", indexPath),
						utils.Err(uerr))
				}
			}
			if record.ID > 0 {
				if fid, rerr := h.resolvePublicFileID(rootName, fullPath); rerr == nil && fid > 0 {
					if uerr := h.db.Model(&models.OperationRecord{}).Where("id = ?", record.ID).
						Update("file_record_id", fid).Error; uerr != nil {
						utils.Warn("上传记录关联索引ID失败",
							utils.Int64("record", int64(record.ID)), utils.Err(uerr))
					}
				}
			}
			// 触发哈希计算（后台执行，不阻塞）
			h.indexSvc.TriggerHash(context.Background())
		}
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"id":          record.ID,
		"name":        filename,
		"path":        fullPath,
		"root":        rootName,
		"size":        fileHeader.Size,
		"full_path":   fullPath,
		"overwritten": existing,
	})
}

// ==================== 分片上传 API ====================
// 与 Web 端公共上传共用 UploadSessionService：init 创建会话 → chunk 逐片上传 →
// finalize 合并落盘并同步索引。会话按调用者身份（用户 / 上传 IP）隔离。

// InitChunkUploadRequest 创建分片上传会话请求
type InitChunkUploadRequest struct {
	Filename string `json:"filename" binding:"required,max=255"`
	FileSize int64  `json:"fileSize" binding:"required,min=1"`
	Path     string `json:"path"` // 目标目录，格式 /rootName[/subdir]
}

// InitChunkUpload POST /api/open/v1/files/upload/init
func (h *Handler) InitChunkUpload(c *gin.Context) {
	if !appconfig.GlobalConfig.Upload.Enabled {
		response.HandleForbidden(c, "公共目录上传功能已关闭")
		return
	}

	var req InitChunkUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误: "+err.Error(), nil)
		return
	}

	if strings.TrimSpace(req.Path) == "" {
		response.HandleBadRequest(c, "缺少目标目录 path", nil)
		return
	}
	rootName, dir, ok := h.parseTargetDir(c, req.Path)
	if !ok {
		return
	}

	filename := sanitizeUploadFileName(req.Filename)
	if filename == "" {
		response.HandleBadRequest(c, "文件名无效", nil)
		return
	}
	if !pathutils.IsFileAllowed(appconfig.GlobalConfig.Storage.AllowedExtensions, filename) {
		response.HandleBadRequest(c, "不允许的文件类型: "+filepath.Ext(filename), nil)
		return
	}
	if maxSize := appconfig.GlobalConfig.Upload.MaxFileSize; maxSize > 0 && req.FileSize > maxSize {
		response.HandleErrorCompat(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("文件大小超出限制（最大 %d 字节）", maxSize), nil)
		return
	}

	session, err := h.uploadSvc.CreateSession(&services.CreateSessionReq{
		Filename:   filename,
		FileSize:   req.FileSize,
		Dir:        dir,
		RootName:   rootName,
		TargetType: models.TargetTypeRegular,
		UserID:     h.uploadIdentity(c),
		ClientIP:   utils.GetClientIP(c),
	})
	if err != nil {
		response.HandleBadRequest(c, err.Error(), nil)
		return
	}

	response.HandleSuccess(c, http.StatusCreated, "会话创建成功", gin.H{
		"upload_id":    session.ID,
		"chunk_size":   session.ChunkSize,
		"total_chunks": session.TotalChunks,
		"expired_at":   session.ExpiredAt,
		"root":         rootName,
		"path":         "/" + rootName + dir,
	})
}

// UploadFileChunk POST /api/open/v1/files/upload/chunk
// multipart/form-data: uploadId、chunkIndex、chunk、checksum（可选，MD5/大小校验由服务端处理）
func (h *Handler) UploadFileChunk(c *gin.Context) {
	uploadIDStr := c.PostForm("uploadId")
	uploadID, err := strconv.ParseUint(uploadIDStr, 10, 64)
	if err != nil || uploadID == 0 {
		response.HandleBadRequest(c, "无效的会话ID", nil)
		return
	}
	chunkIndex, err := strconv.Atoi(c.PostForm("chunkIndex"))
	if err != nil || chunkIndex < 0 {
		response.HandleBadRequest(c, "无效的分片索引", nil)
		return
	}
	checksum := strings.TrimSpace(c.PostForm("checksum"))
	if !validChunkChecksum(checksum) {
		response.HandleBadRequest(c, "checksum 必须为 1-16 位十六进制字符串", nil)
		return
	}

	session, ok := h.ownedUploadSession(c, uint(uploadID))
	if !ok {
		return
	}
	if chunkIndex >= session.TotalChunks {
		response.HandleBadRequest(c, fmt.Sprintf("分片索引超出范围: 0-%d", session.TotalChunks-1), nil)
		return
	}

	if h.chunkSize > 0 {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.chunkSize+8<<20)
	}
	chunk, err := c.FormFile("chunk")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.HandleErrorCompat(c, http.StatusRequestEntityTooLarge, "分片大小超出限制", nil)
			return
		}
		response.HandleBadRequest(c, "未找到分片文件: "+err.Error(), nil)
		return
	}

	// 预期分片大小：除最后一片外均为 chunk_size，超出直接拒绝，避免服务端多写后再回滚。
	expectedChunkSize := session.ChunkSize
	if chunkIndex == session.TotalChunks-1 {
		expectedChunkSize = session.FileSize - int64(chunkIndex)*session.ChunkSize
	}
	if chunk.Size > expectedChunkSize {
		response.HandleErrorCompat(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("分片大小 %d 超过限制 %d", chunk.Size, expectedChunkSize), nil)
		return
	}

	src, err := chunk.Open()
	if err != nil {
		response.HandleInternalServerError(c, "打开分片文件失败: "+err.Error())
		return
	}
	defer src.Close()

	if err := h.uploadSvc.UploadChunk(&services.UploadChunkReq{
		UploadID:   uint(uploadID),
		ChunkIndex: chunkIndex,
		ChunkData:  src,
		ChunkSize:  chunk.Size,
		Checksum:   checksum,
	}); err != nil {
		handleUploadChunkError(c, err)
		return
	}

	response.HandleSuccess(c, http.StatusOK, "分片上传成功", gin.H{"chunk_index": chunkIndex})
}

// GetChunkUploadStatus GET /api/open/v1/files/upload/:id
func (h *Handler) GetChunkUploadStatus(c *gin.Context) {
	uploadID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || uploadID == 0 {
		response.HandleBadRequest(c, "无效的会话ID", nil)
		return
	}
	if _, ok := h.ownedUploadSession(c, uint(uploadID)); !ok {
		return
	}

	status, err := h.uploadSvc.GetStatus(uint(uploadID))
	if err != nil {
		response.HandleNotFound(c, "上传会话不存在")
		return
	}

	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"upload_id":        status.ID,
		"file_name":        status.FileName,
		"file_size":        status.FileSize,
		"chunk_size":       status.ChunkSize,
		"total_chunks":     status.TotalChunks,
		"uploaded_indexes": status.UploadedIndexes,
		"status":           status.Status,
		"expired":          status.Expired,
		"expired_at":       status.ExpiredAt,
	})
}

// FinalizeChunkUploadRequest 完成分片上传请求
type FinalizeChunkUploadRequest struct {
	UploadID uint64 `json:"uploadId" binding:"required"`
	// Overwrite 目标文件已存在时是否覆盖，需具备 open_api:files:overwrite 权限
	Overwrite bool `json:"overwrite"`
}

// FinalizeChunkUpload POST /api/open/v1/files/upload/finalize
func (h *Handler) FinalizeChunkUpload(c *gin.Context) {
	var req FinalizeChunkUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "请求数据格式错误: uploadId 必填且为整数", nil)
		return
	}

	session, ok := h.ownedUploadSession(c, uint(req.UploadID))
	if !ok {
		return
	}

	// 仅在目标确实已存在时才需要覆盖权限：先按调用方声明与实际 scope 计算允许覆盖标志，
	// 目标冲突时再根据是否声明覆盖给出 403/409，避免目标不存在时也要求覆盖权限。
	canOverwrite := hasContextScope(c, models.ScopeFilesOverwrite)
	result, err := h.uploadSvc.Finalize(uint(req.UploadID), req.Overwrite && canOverwrite)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrFileExists):
			if req.Overwrite && !canOverwrite {
				response.HandleForbidden(c, "覆盖已有文件需要 open_api:files:overwrite 权限")
			} else {
				response.HandleErrorCompat(c, http.StatusConflict, "目标文件已存在；如需覆盖请传 overwrite=true", nil)
			}
		case strings.Contains(err.Error(), "不存在"):
			response.HandleNotFound(c, err.Error())
		case strings.Contains(err.Error(), "分片未全部上传"), strings.Contains(err.Error(), "分片索引不完整"),
			strings.Contains(err.Error(), "会话已过期"):
			response.HandleErrorCompat(c, http.StatusConflict, err.Error(), nil)
		default:
			response.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		}
		return
	}

	fullPath := "/" + session.TargetRoot + "/" + strings.TrimPrefix(result.TargetPath, "/")
	fileID, _ := h.resolvePublicFileID(session.TargetRoot, fullPath)

	response.HandleSuccess(c, http.StatusOK, "文件上传完成", gin.H{
		"id":          fileID,
		"name":        result.FileName,
		"path":        fullPath,
		"full_path":   fullPath,
		"root":        session.TargetRoot,
		"size":        session.FileSize,
		"overwritten": result.Overwritten,
	})
}

// parseTargetDir 解析 /rootName[/subdir] 目标目录，失败时写入错误响应并返回 false。
func (h *Handler) parseTargetDir(c *gin.Context, targetDir string) (string, string, bool) {
	cleanPath, err := normalizeOpenAPIPath(targetDir)
	if err != nil {
		response.HandleBadRequest(c, "path 参数非法: "+err.Error(), nil)
		return "", "", false
	}
	parts := splitPath(cleanPath)
	if len(parts) == 0 {
		response.HandleBadRequest(c, "path 格式错误: 需要 /rootName[/subdir]", nil)
		return "", "", false
	}
	rootName := parts[0]
	if _, ok := h.rootNames[rootName]; !ok {
		response.HandleNotFound(c, "根目录不存在: "+rootName)
		return "", "", false
	}
	if !appconfig.PublicDirUploadAllowed(rootName) {
		response.HandleForbidden(c, "该目录不允许上传")
		return "", "", false
	}
	dir := ""
	if len(parts) > 1 {
		dir = "/" + strings.Join(parts[1:], "/")
	}
	return rootName, dir, true
}

// ownedUploadSession 加载上传会话并校验归属；失败时写入错误响应并返回 false。
func (h *Handler) ownedUploadSession(c *gin.Context, uploadID uint) (*models.UploadSession, bool) {
	var session models.UploadSession
	if err := h.db.Where("id = ?", uploadID).First(&session).Error; err != nil {
		response.HandleNotFound(c, "上传会话不存在")
		return nil, false
	}
	identity := h.uploadIdentity(c)
	if identity == "" || session.UserID != identity {
		response.HandleForbidden(c, "无权操作该上传会话")
		return nil, false
	}
	return &session, true
}

// uploadIdentity 返回分片上传会话的归属标识：优先用户 UUID，其次用户 ID，最后回退客户端 IP。
func (h *Handler) uploadIdentity(c *gin.Context) string {
	if uuid := h.callerUserUUID(c); uuid != "" {
		return uuid
	}
	if v, ok := c.Get(userIDContextKey); ok {
		if id, ok := v.(uint); ok && id > 0 {
			return strconv.FormatUint(uint64(id), 10)
		}
	}
	return utils.GetClientIP(c)
}

// resolvePublicFileID 按完整路径反查公共文件索引记录 ID（仅 active）。
func (h *Handler) resolvePublicFileID(rootName, fullPath string) (uint, error) {
	var ids []uint
	err := h.db.Model(&models.FileRecordPublic{}).
		Where("root_name = ? AND status = ?", rootName, models.FileStatusActive).
		Where("full_path = ? OR full_path = ?", fullPath, strings.TrimPrefix(fullPath, "/")).
		Order("id ASC").Limit(1).Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	return ids[0], nil
}

// storageFor 获取指定公开根目录对应的存储后端；未注册远程后端时回退本地磁盘。
func (h *Handler) storageFor(rootName string) (storage.Storage, bool) {
	if h.storages != nil {
		if st, ok := h.storages[rootName]; ok && st != nil {
			return st, true
		}
	}
	rootPath, ok := h.rootNames[rootName]
	if !ok {
		return nil, false
	}
	return storage.NewLocalStorage(rootPath), true
}

// sanitizeUploadFileName 提取并清洗上传文件名，拒绝路径片段与控制字符。
func sanitizeUploadFileName(name string) string {
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	return name
}

// callerUserUUID 返回调用者用户 UUID：Web 会话直接取 JWT claims，API Key 按所属用户回查。
func (h *Handler) callerUserUUID(c *gin.Context) string {
	if v, ok := c.Get(string(constants.ContextKeyUserUUID)); ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	if v, ok := c.Get(userIDContextKey); ok {
		if id, ok := v.(uint); ok && id > 0 {
			var user models.User
			if err := h.db.Select("uuid").Where("id = ?", id).First(&user).Error; err == nil {
				return user.UUID
			}
		}
	}
	return ""
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

// splitRootPath 将完整路径拆为根目录名与带前导 / 的相对路径。
func splitRootPath(p string) (rootName, relPath string) {
	parts := splitPath(filepath.ToSlash(p))
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], "/" + strings.Join(parts[1:], "/")
}
