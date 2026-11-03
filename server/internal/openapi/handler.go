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
	"fuzhan/internal/models"
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
	// chunkSize 分片大小（字节），由配置 upload.chunk_size 决定
	chunkSize int64
}

// NewHandler 创建 Open API 处理器
func NewHandler(db *gorm.DB, indexSvc *index.Service, depSvc *index.DependencyService,
	rootNames map[string]string, storages map[string]storage.Storage,
	uploadSvc *services.UploadSessionService, chunkSize int64) *Handler {
	return &Handler{
		db:        db,
		indexSvc:  indexSvc,
		depSvc:    depSvc,
		rootNames: rootNames,
		storages:  storages,
		uploadSvc: uploadSvc,
		chunkSize: chunkSize,
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
	if disposition != "attachment" && disposition != "inline" {
		response.HandleBadRequest(c, "disposition 仅支持 attachment 或 inline", nil)
		return
	}
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

	var record models.FileRecordPublic
	if idStr != "" {
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil || id == 0 {
			response.HandleBadRequest(c, "无效的记录ID", nil)
			return
		}
		if err := h.db.Where("id = ? AND status = ?", uint(id), models.FileStatusActive).
			First(&record).Error; err != nil {
			response.HandleNotFound(c, "文件记录不存在")
			return
		}
	} else {
		parts := splitPath(filepath.ToSlash(pathStr))
		if len(parts) < 2 {
			response.HandleBadRequest(c, "路径格式错误: 需要 /rootName/path/to/file", nil)
			return
		}
		rootName := parts[0]
		relPath := filepath.ToSlash("/" + strings.Join(parts[1:], "/"))
		if err := h.db.Where("root_name = ? AND file_path = ? AND status = ?",
			rootName, relPath, models.FileStatusActive).First(&record).Error; err != nil {
			response.HandleNotFound(c, "文件记录不存在: "+rootName+relPath)
			return
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
		"download_count": record.DownloadCount,
		"status":         record.Status,
		"created_at":     record.CreatedAt,
		"updated_at":     record.UpdatedAt,
	})
}

// ==================== 文件上传 API ====================

// UploadFile POST /api/open/v1/files/upload
// 以 multipart/form-data 上传单个文件到指定公共目录：
//   - file: 上传的文件（必填）
//   - path: 目标目录，格式 /rootName[/subdir]，缺省为 /
//   - overwrite: 目标已存在时是否覆盖，仅管理员可覆盖
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
		targetDir = "/"
	}
	parts := splitPath(filepath.ToSlash(targetDir))
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
	for _, p := range parts[1:] {
		if p == "." || p == ".." {
			response.HandleBadRequest(c, "path 不能包含 . 或 ..", nil)
			return
		}
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

	overwrite := false
	if v := strings.TrimSpace(c.PostForm("overwrite")); v != "" {
		overwrite, _ = strconv.ParseBool(v)
	}
	if existing {
		indexPath := "/" + relPath
		var existingRec models.FileRecordPublic
		recErr := h.db.Where("root_name = ? AND file_path = ? AND status = ?",
			rootName, indexPath, models.FileStatusActive).First(&existingRec).Error
		if !overwrite || recErr != nil || clientIP == "" || existingRec.UploaderIP != clientIP {
			response.HandleErrorCompat(c, http.StatusConflict,
				"目标文件已存在；仅上传者同一 IP 可覆盖，且需传 overwrite=true", nil)
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

	targetDir := strings.TrimSpace(req.Path)
	if targetDir == "" {
		targetDir = "/"
	}
	rootName, dir, ok := h.parseTargetDir(c, targetDir)
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
		"uploadId":    session.ID,
		"chunkSize":   session.ChunkSize,
		"totalChunks": session.TotalChunks,
		"expiredAt":   session.ExpiredAt,
		"root":        rootName,
		"path":        "/" + rootName + dir,
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

	if _, ok := h.ownedUploadSession(c, uint(uploadID)); !ok {
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
		Checksum:   c.PostForm("checksum"),
	}); err != nil {
		response.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	response.HandleSuccess(c, http.StatusOK, "分片上传成功", gin.H{"chunkIndex": chunkIndex})
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
		"uploadId":        status.ID,
		"fileName":        status.FileName,
		"fileSize":        status.FileSize,
		"chunkSize":       status.ChunkSize,
		"totalChunks":     status.TotalChunks,
		"uploadedIndexes": status.UploadedIndexes,
		"status":          status.Status,
		"expired":         status.Expired,
		"expiredAt":       status.ExpiredAt,
	})
}

// FinalizeChunkUploadRequest 完成分片上传请求
type FinalizeChunkUploadRequest struct {
	UploadID uint64 `json:"uploadId" binding:"required"`
}

// FinalizeChunkUpload POST /api/open/v1/files/upload/finalize
func (h *Handler) FinalizeChunkUpload(c *gin.Context) {
	var req FinalizeChunkUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleBadRequest(c, "无效的会话ID: "+err.Error(), nil)
		return
	}

	session, ok := h.ownedUploadSession(c, uint(req.UploadID))
	if !ok {
		return
	}

	// 公开目录覆盖沿用 Web 上传语义：不引入管理员会话，仅当上传者 IP 与现有记录一致时放行
	result, err := h.uploadSvc.Finalize(uint(req.UploadID), false)
	if err != nil {
		if errors.Is(err, services.ErrFileExists) {
			response.HandleErrorCompat(c, http.StatusConflict, err.Error(), nil)
		} else {
			response.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		}
		return
	}

	fullPath := "/" + session.TargetRoot + "/" + strings.TrimPrefix(result.TargetPath, "/")
	fileID, _ := h.resolvePublicFileID(session.TargetRoot, fullPath)

	response.HandleSuccess(c, http.StatusOK, "文件上传完成", gin.H{
		"id":        fileID,
		"name":      result.FileName,
		"path":      fullPath,
		"full_path": fullPath,
		"root":      session.TargetRoot,
		"size":      session.FileSize,
	})
}

// parseTargetDir 解析 /rootName[/subdir] 目标目录，失败时写入错误响应并返回 false。
func (h *Handler) parseTargetDir(c *gin.Context, targetDir string) (string, string, bool) {
	parts := splitPath(filepath.ToSlash(targetDir))
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
	for _, p := range parts[1:] {
		if p == "." || p == ".." {
			response.HandleBadRequest(c, "path 不能包含 . 或 ..", nil)
			return "", "", false
		}
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
