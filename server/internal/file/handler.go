package file

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/constants"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// FileHandlers 文件相关处理器
type FileHandlers struct {
	BaseHandler *BaseHandler
	FileService *services.FileService
	DB          *gorm.DB
}

// NewFileHandlers 创建文件处理器实例
func NewFileHandlers(baseHandler *BaseHandler, fileService *services.FileService, db *gorm.DB) *FileHandlers {
	return &FileHandlers{
		BaseHandler: baseHandler,
		FileService: fileService,
		DB:          db,
	}
}

// syncIndexMove 移动/重命名成功后同步索引表。同根目录内通过 MoveFile 复用原
// 记录 ID（保证操作记录的 file_record_id 身份关联不因移动失效）；跨根目录则
// 旧位置软删 + 新位置重新建索引。
func (fh *FileHandlers) syncIndexMove(oldRoot, oldRel, newRoot, newRel string) {
	if fh.BaseHandler == nil || fh.BaseHandler.IndexSvc == nil {
		return
	}
	isvc := fh.BaseHandler.IndexSvc
	if oldRoot == newRoot {
		if err := isvc.MoveFile(oldRoot, oldRel, newRel); err != nil {
			utils.Warn("移动文件后同步索引失败",
				utils.String("from", oldRoot+"/"+oldRel),
				utils.String("to", newRoot+"/"+newRel), utils.Err(err))
		}
		return
	}
	if err := isvc.RemoveFile(oldRoot, oldRel); err != nil {
		utils.Warn("跨根移动软删旧索引失败", utils.String("path", oldRoot+"/"+oldRel), utils.Err(err))
	}
	if err := isvc.SyncFile(newRoot, newRel); err != nil {
		utils.Warn("跨根移动重建索引失败", utils.String("path", newRoot+"/"+newRel), utils.Err(err))
	}
}

// FileRecordInfo DB 查询返回的文件信息结构
// Path 字段是带前导 / 的完整路径 (rootName + 相对路径)，与 FullPath 一致
// 保留此字段名以兼容旧版前端，但实际内容是完整路径
type FileRecordInfo struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Path          string `json:"path"`
	FullPath      string `json:"fullPath"`
	ModifiedTime  string `json:"modifiedTime"`
	Size          int64  `json:"size"`
	Notes         string `json:"notes"`
	Xxh3Hash      string `json:"xxh3Hash,omitempty"`
	HashStatus    string `json:"hashStatus,omitempty"`
	RecordID      uint   `json:"recordId,omitempty"`
	IsDir         bool   `json:"isDir"`
	RootName      string `json:"rootName"`
	DownloadCount int64  `json:"downloadCount"`
	CanManage     bool   `json:"canManage"` // 当前请求者能否管理（管理员，或上传者IP一致）
}

// ListDirectories 列出目录内容（基于数据库查询）
func (fh *FileHandlers) ListDirectories(c *gin.Context) {
	var req models.ListDirectoriesRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.HandleBadRequest(c, "请求参数格式错误", err.Error())
		return
	}

	path := strings.ReplaceAll(req.Path, "\\", "/")
	path = strings.Trim(path, "/")

	// 根目录：返回所有激活的根目录列表
	if path == "" {
		rootNames := fh.getRootNameList()
		var result []FileRecordInfo
		for _, rn := range rootNames {
			result = append(result, FileRecordInfo{
				Name:     rn,
				Type:     "directory",
				Path:     rn,
				FullPath: rn,
				RootName: rn,
				IsDir:    true,
			})
		}
		utils.HandleSuccess(c, http.StatusOK, "", result)
		return
	}

	// 解析路径：第一部分为 root name
	parts := strings.SplitN(path, "/", 2)
	rootName := parts[0]
	if _, exists := appconfig.RootNames[rootName]; !exists {
		utils.HandleBadRequest(c, "指定的目录不存在: "+rootName, nil)
		return
	}

	if len(parts) == 1 {
		// 列出根目录下的直接子项
		files, err := fh.listDirectChildren(rootName, "", utils.GetClientIP(c), fh.requesterIsAdmin(c))
		if err != nil {
			utils.HandleError(c, http.StatusInternalServerError, 500, "数据库查询失败", err.Error())
			return
		}
		utils.HandleSuccess(c, http.StatusOK, "", files)
		return
	}

	// 列出子目录下的直接子项
	prefix := parts[1]
	files, err := fh.listDirectChildren(rootName, prefix, utils.GetClientIP(c), fh.requesterIsAdmin(c))
	if err != nil {
		utils.HandleError(c, http.StatusInternalServerError, 500, "数据库查询失败", err.Error())
		return
	}
	if len(files) == 0 {
		// 目录为空：先检查文件系统确认目录真实存在（空目录可能没有索引记录）
		if fh.directoryExistsOnDisk(rootName, prefix) {
			utils.HandleSuccess(c, http.StatusOK, "", files)
			return
		}
		// 文件系统不存在时，回退检查索引记录（可能是尚未扫描到的目录）
		var count int64
		fh.DB.Model(&models.FileRecordPublic{}).
			Where("root_name = ? AND file_path = ? AND status = ?",
				rootName, prefix, models.FileStatusActive).
			Count(&count)
		if count == 0 {
			utils.HandleBadRequest(c, "指定的目录不存在: "+rootName+"/"+prefix, nil)
			return
		}
		// 有索引记录但无子项，返回空列表
	}
	utils.HandleSuccess(c, http.StatusOK, "", files)
}

// directoryExistsOnDisk 检查目录在文件系统上是否存在（带路径越界防护）
func (fh *FileHandlers) directoryExistsOnDisk(rootName, prefix string) bool {
	rootPath, ok := appconfig.RootNames[rootName]
	if !ok {
		return false
	}
	cleanPrefix := strings.Trim(prefix, "/")
	dirPath := rootPath
	if cleanPrefix != "" {
		dirPath = filepath.Join(rootPath, filepath.FromSlash(cleanPrefix))
	}
	absDir, err := filepath.Abs(dirPath)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return false
	}
	if absDir != absRoot && !strings.HasPrefix(absDir, absRoot+string(filepath.Separator)) {
		return false
	}
	info, err := os.Stat(absDir)
	return err == nil && info.IsDir()
}

// getRootNameList 获取配置的根目录名称列表
func (fh *FileHandlers) getRootNameList() []string {
	var names []string
	for name := range appconfig.RootNames {
		names = append(names, name)
	}
	return names
}

// listDirectChildren 列出指定目录下的直接子项（文件和子目录）
// prefix 为空时列出根级直接子项；isAdmin 为 true 时所有条目均可管理
func (fh *FileHandlers) listDirectChildren(rootName, prefix, requesterIP string, isAdmin bool) ([]FileRecordInfo, error) {
	query := fh.DB.Model(&models.FileRecordPublic{}).
		Where("root_name = ? AND status = ?", rootName, models.FileStatusActive)

	if prefix == "" {
		// 根级：文件路径不包含额外 "/"（只有前导 /）
		// 示例：/file.txt 匹配，/dir/file.txt 不匹配
		query = query.Where("file_path NOT LIKE ?", "/%/%")
	} else {
		// 子目录：路径以 /prefix/ 开头，但不包含更深层级
		// 示例：prefix=subdir，/subdir/file.txt 匹配，/subdir/nested/file.txt 不匹配
		cleanPrefix := strings.TrimRight(prefix, "/")
		query = query.Where(
			"file_path LIKE ? AND file_path NOT LIKE ?",
			"/"+cleanPrefix+"/%", "/"+cleanPrefix+"/%/%",
		)
	}

	var records []models.FileRecordPublic
	if err := query.Order("is_dir DESC, file_name ASC").Find(&records).Error; err != nil {
		return nil, err
	}

	// 可管理：管理员，或上传者IP与当前请求者IP一致
	result := make([]FileRecordInfo, 0, len(records))
	for _, r := range records {
		fileType := "file"
		if r.IsDir {
			fileType = "directory"
		}
		modifiedTime := ""
		if !r.ModTime.IsZero() {
			modifiedTime = r.ModTime.Format("2006-01-02T15:04:05")
		}
		canManage := isAdmin
		result = append(result, FileRecordInfo{
			Name:          r.FileName,
			Type:          fileType,
			Path:          r.FullPath,
			FullPath:      r.FullPath,
			ModifiedTime:  modifiedTime,
			Size:          r.FileSize,
			Notes:         r.Notes,
			Xxh3Hash:      r.Xxh3Hash,
			HashStatus:    r.HashStatus,
			RecordID:      r.ID,
			IsDir:         r.IsDir,
			RootName:      r.RootName,
			DownloadCount: r.DownloadCount,
			CanManage:     canManage,
		})
	}
	return result, nil
}

// requesterIsAdmin 判断请求者是否为管理员
func (fh *FileHandlers) requesterIsAdmin(c *gin.Context) bool {
	role, _ := c.Get(string(constants.ContextKeyRole))
	return role == "admin"
}

// canManagePublic 判断请求者是否有权管理某公开文件（管理员，或上传者IP一致）
// fullPath 为带 rootName 的完整路径，如 "documents/report.pdf"
func (fh *FileHandlers) canManagePublic(c *gin.Context, fullPath string) bool {
	if fh.requesterIsAdmin(c) {
		return true
	}
	ip := utils.GetClientIP(c)
	if ip == "" {
		return false
	}
	rootName, relPath := splitPath(fullPath)
	if rootName == "" {
		return false
	}
	var rec models.FileRecordPublic
	if err := fh.DB.Where("root_name = ? AND file_path = ? AND status = ?",
		rootName, relPath, models.FileStatusActive).First(&rec).Error; err != nil {
		return false
	}
	return rec.UploaderIP != "" && rec.UploaderIP == ip
}

// RenamePublicFileHandler 重命名公开文件（管理员，或上传者IP一致）
// 兼容请求体 { oldPath, newName } 与 { path, newName }
func (fh *FileHandlers) RenamePublicFileHandler(c *gin.Context) {
	var req struct {
		OldPath string `json:"oldPath"`
		Path    string `json:"path"`
		NewName string `json:"newName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleBadRequest(c, "请求数据格式错误", err.Error())
		return
	}
	oldPath := req.OldPath
	if oldPath == "" {
		oldPath = req.Path
	}
	if oldPath == "" || req.NewName == "" {
		utils.HandleBadRequest(c, "路径和新文件名不能为空", nil)
		return
	}

	if !fh.canManagePublic(c, oldPath) {
		utils.HandleErrorCompat(c, http.StatusForbidden, "无权操作（仅文件上传者或管理员可管理）", nil)
		return
	}

	if err := fh.FileService.RenameFile(oldPath, req.NewName); err != nil {
		middleware.LogOperation(c, "file.rename", oldPath+" -> "+req.NewName, err)
		utils.HandleBadRequest(c, err.Error(), nil)
		return
	}
	middleware.LogOperation(c, "file.rename", oldPath+" -> "+req.NewName, nil)
	if rootName, rel := splitPath(oldPath); rootName != "" {
		fh.syncIndexMove(rootName, rel, rootName, path.Join(path.Dir(rel), req.NewName))
	}
	utils.HandleSuccess(c, http.StatusOK, "重命名成功", nil)
}

// MoveFileHandler 移动文件处理器（管理员，或上传者IP一致）
func (fh *FileHandlers) MoveFileHandler(c *gin.Context) {
	var req models.MoveFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleBadRequest(c, "请求参数格式错误", err.Error())
		return
	}

	if !fh.canManagePublic(c, req.OldPath) {
		utils.HandleErrorCompat(c, http.StatusForbidden, "无权操作（仅文件上传者或管理员可管理）", nil)
		return
	}

	// 执行移动
	if err := fh.FileService.MoveFile(req.OldPath, req.NewPath); err != nil {
		middleware.LogOperation(c, "file.move", req.OldPath+" -> "+req.NewPath, err)
		utils.HandleBadRequest(c, err.Error(), nil)
		return
	}

	middleware.LogOperation(c, "file.move", req.OldPath+" -> "+req.NewPath, nil)
	if oldRoot, oldRel := splitPath(req.OldPath); oldRoot != "" {
		if newRoot, newRel := splitPath(req.NewPath); newRoot != "" {
			fh.syncIndexMove(oldRoot, oldRel, newRoot, newRel)
		}
	}
	utils.HandleSuccess(c, http.StatusOK, "移动成功", nil)
}

// DeleteFileHandler 删除文件处理器（管理员，或上传者IP一致）
func (fh *FileHandlers) DeleteFileHandler(c *gin.Context) {
	var req models.DeleteFileRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		utils.HandleBadRequest(c, "请求参数格式错误", err.Error())
		return
	}

	if !fh.canManagePublic(c, req.Path) {
		utils.HandleErrorCompat(c, http.StatusForbidden, "无权操作（仅文件上传者或管理员可管理）", nil)
		return
	}

	// 在删除前判断是否为目录（删除后 os.Stat 会失败）
	isDir := false
	if rootName, relPath := splitPath(req.Path); rootName != "" {
		if rootPath, exists := appconfig.RootNames[rootName]; exists {
			realPath := filepath.Join(rootPath, relPath)
			if info, err := os.Stat(realPath); err == nil {
				isDir = info.IsDir()
			}
		}
	}

	// 执行删除
	if err := fh.FileService.DeleteFile(req.Path); err != nil {
		middleware.LogOperation(c, "file.delete", req.Path, err)
		utils.HandleBadRequest(c, err.Error(), nil)
		return
	}

	// 删除成功后硬删除索引记录
	if rootName, relPath := splitPath(req.Path); rootName != "" {
		var records []models.FileRecordPublic
		var queryErr error
		if isDir {
			// 目录删除：清理该目录下所有索引记录（前缀匹配）
			prefix := strings.TrimPrefix(relPath, "/") + "/"
			queryErr = fh.DB.Where("root_name = ? AND file_path LIKE ? AND status = ?",
				rootName, "/"+prefix+"%", models.FileStatusActive).Find(&records).Error
		} else {
			// 文件删除：精确匹配
			queryErr = fh.DB.Where("root_name = ? AND file_path = ? AND status = ?",
				rootName, relPath, models.FileStatusActive).Find(&records).Error
		}
		if queryErr != nil {
			utils.Warn("查找索引记录失败",
				utils.String("root", rootName),
				utils.String("path", relPath),
				utils.Err(queryErr))
		} else {
			// 收集被删除的索引记录 ID，用于级联清理操作记录（覆盖移动/重命名后的旧路径快照）
			recordIDs := make([]uint, 0, len(records))
			for _, rec := range records {
				recordIDs = append(recordIDs, rec.ID)
				if err := fh.BaseHandler.IndexSvc.DeleteRecord(rec.ID); err != nil {
					utils.Warn("硬删除索引记录失败",
						utils.Int("id", int(rec.ID)),
						utils.String("root", rootName),
						utils.String("path", relPath),
						utils.Err(err))
				}
			}
			// 级联删除对应的上传/下载操作记录（保留查询优化为当前路径匹配）
			if fh.BaseHandler.RecordRepo != nil {
				if cnt, err := fh.BaseHandler.RecordRepo.DeleteByFile(rootName, relPath, isDir, recordIDs); err != nil {
					utils.Warn("删除文件对应操作记录失败",
						utils.String("root", rootName),
						utils.String("path", relPath),
						utils.Err(err))
				} else if cnt > 0 {
					utils.Info("已清理文件删除后的操作记录",
						utils.String("root", rootName),
						utils.String("path", relPath),
						utils.Int64("count", cnt))
				}
			}
		}
	}

	middleware.LogOperation(c, "file.delete", req.Path, nil)
	utils.HandleSuccess(c, http.StatusOK, "删除成功", nil)
}

// splitPath 将完整路径拆分为 rootName 和相对路径
// 输入: "/rootName/subdir/file.txt" → 返回: "rootName", "/subdir/file.txt"
func splitPath(fullPath string) (rootName, relPath string) {
	p := strings.Trim(fullPath, "/")
	parts := strings.SplitN(p, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	rootName = parts[0]
	if len(parts) > 1 {
		relPath = "/" + parts[1]
	}
	return
}
