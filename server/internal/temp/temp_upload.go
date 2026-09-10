package temp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zeebo/xxh3"
	"gorm.io/gorm"

	configPkg "fuzhan/internal/appconfig"
	"fuzhan/internal/index"
	"fuzhan/internal/middleware"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
)

// Storage 临时上传的落盘路径策略：
// 目标为 `{path}/{Dir}/{码哈希}`，上传中临时文件为同目录 `. {码哈希}.uploading`。
// 与公开/私有一致，分片流程只关心这两个路径落在哪，业务差异在策略内收敛。
type Storage struct {
	basePath string
}

// NewStorage 创建临时上传的落盘路径策略。
func NewStorage(basePath string) *Storage {
	return &Storage{basePath: basePath}
}

// Paths 实现临时上传的路径构建与越界校验。
func (s *Storage) Paths(session *models.UploadSession) (targetPath string, uploadingPath string, err error) {
	basePath := s.basePath
	if basePath == "" {
		err = fmt.Errorf("临时存储未配置")
		return
	}
	if session.Code == "" {
		err = fmt.Errorf("缺少访问码")
		return
	}

	cleanDir := session.TargetPath
	if cleanDir == "" || cleanDir == "/" {
		cleanDir = ""
	} else {
		cleanDir = strings.TrimSpace(cleanDir)
		cleanDir = strings.Trim(cleanDir, `/\`)
		cleanDir = filepath.Clean(cleanDir)
		if cleanDir == "." || filepath.IsAbs(cleanDir) || cleanDir == ".." ||
			strings.HasPrefix(cleanDir, ".."+string(filepath.Separator)) {
			err = fmt.Errorf("无效的目标路径")
			return
		}
	}

	dir := filepath.Join(basePath, cleanDir)
	if !isPathWithinRoot(basePath, dir) {
		err = fmt.Errorf("路径越界")
		return
	}

	hash := xxh3.Hash([]byte(session.Code + "fuzhan-secret"))
	safeFilename := fmt.Sprintf("%016x", hash)
	targetPath = filepath.Join(dir, safeFilename)
	uploadingPath = filepath.Join(dir, "."+safeFilename+".uploading")
	return
}

// Finalizer 临时上传的收尾策略：
// 目标文件按码哈希命名、唯一且不可被覆盖；落盘后写 temp_files 与 file_records_temp
// （码 32hex 作为分享/下载凭证，与存储布局解耦），清理会话与分片并触发哈希。
type Finalizer struct {
	db                *gorm.DB
	indexSvc          *index.Service
	sessionRepo       *repositories.SessionRepository
	chunkRepo         *repositories.ChunkRepository
	basePath          string
	defaultExpireDays int
}

// NewFinalizer 创建临时上传的收尾策略。
func NewFinalizer(db *gorm.DB, indexSvc *index.Service, basePath string, defaultExpireDays int) *Finalizer {
	return &Finalizer{
		db:                db,
		indexSvc:          indexSvc,
		sessionRepo:       repositories.NewSessionRepository(db),
		chunkRepo:         repositories.NewChunkRepository(db),
		basePath:          basePath,
		defaultExpireDays: defaultExpireDays,
	}
}

// AllowsOverwrite 临时上传的文件名由访问码哈希派生、全场唯一，无需覆盖。
func (*Finalizer) AllowsOverwrite(_ *models.UploadSession, _ bool) bool {
	return false
}

// Complete 临时上传的完成收尾。
func (f *Finalizer) Complete(session *models.UploadSession, targetPath string) (*services.FinalizeResult, error) {
	now := utils.Now()

	// 计算临时文件过期时间（会话过期只约束上传过程，产物有效期用配置的默认天数）
	expireDays := f.defaultExpireDays
	if expireDays <= 0 {
		expireDays = 7
	}
	expiredAt := now.Add(time.Duration(expireDays) * 24 * time.Hour)

	cleanDir := ""
	if dp := strings.Trim(strings.TrimSuffix(session.TargetPath, "/"), `/\`); dp != "" {
		cleanDir = dp
	}

	tempFile := &models.TempFile{
		Code:             session.Code,
		Filename:         session.FileName,
		FileSize:         session.FileSize,
		FilePath:         targetPath,
		ClientIP:         session.ClientIP,
		Dir:              cleanDir,
		DeleteOnDownload: session.DeleteOnDownload,
		ExpiredAt:        expiredAt,
	}
	if err := f.db.Create(tempFile).Error; err != nil {
		os.Remove(targetPath)
		return nil, fmt.Errorf("保存临时文件记录失败: %w", err)
	}

	// 同步到临时文件索引表（与简单上传一致，码作为 file_path）
	tempRecord := models.FileRecordTemp{
		FileRecordBase: models.FileRecordBase{
			FileName:     session.FileName,
			FilePath:     session.Code,
			RootName:     "temp",
			FullPath:     "/temp/" + session.Code,
			FileSize:     session.FileSize,
			IsDir:        false,
			ModTime:      now,
			Status:       models.FileStatusActive,
			OwnerID:      session.ClientIP,
			LastSyncedAt: now,
		},
	}
	if err := f.db.Create(&tempRecord).Error; err != nil {
		utils.Warn("同步临时文件索引失败", utils.String("code", session.Code), utils.Err(err))
	}

	_ = f.sessionRepo.UpdateStatus(session.ID, models.UploadStatusCompleted)
	_ = f.chunkRepo.DeleteBySessionID(session.ID)
	_ = f.sessionRepo.Delete(session.ID)

	if f.indexSvc != nil {
		// 触发哈希计算（后台执行，不阻塞）
		f.indexSvc.TriggerHash(context.Background())
	}

	return &services.FinalizeResult{
		TargetPath: session.Code,
		FileName:   session.FileName,
		ShareCode:  session.Code,
	}, nil
}

// generateTempCode 生成128bit访问码（32位十六进制，防在线枚举）
func generateTempCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}

// verifyTempSession 解析会话ID并校验归属：仅限临时类型且绑定创建者 IP，
// 防止凭自增ID越权访问他人上传会话。
func (h *Handler) verifyTempSession(c *gin.Context, uploadIDStr string) (*models.UploadSession, error) {
	if uploadIDStr == "" {
		return nil, fmt.Errorf("缺少上传ID")
	}
	id, err := strconv.ParseUint(uploadIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("无效的会话ID")
	}
	session, err := repositories.NewSessionRepository(h.db).GetByID(uint(id))
	if err != nil {
		return nil, fmt.Errorf("会话不存在")
	}
	if session.TargetType != models.TargetTypeTemp {
		return nil, fmt.Errorf("会话不存在")
	}
	if !h.checkSessionIP(c, session.ClientIP) {
		return nil, os.ErrPermission
	}
	return session, nil
}

// handleTempSessionErr 统一映射会话校验错误：越权返回 403，其余返回 404。
func handleTempSessionErr(c *gin.Context, err error) {
	if errors.Is(err, os.ErrPermission) {
		utils.HandleForbidden(c, "无权访问该上传会话")
		return
	}
	utils.HandleNotFound(c, err.Error())
}

// CreateTempSessionHandler 处理创建临时文件上传会话
func (h *Handler) CreateTempSessionHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	var req struct {
		Filename         string `json:"filename" binding:"required"`
		FileSize         int64  `json:"fileSize" binding:"required"`
		Dir              string `json:"dir"`
		DeleteOnDownload bool   `json:"deleteOnDownload"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.HandleBadRequest(c, "参数错误", err)
		return
	}

	ip := h.getClientIP(c)

	// 检查磁盘空间
	if err := services.CheckDiskSpace(h.config.Path, req.FileSize); err != nil {
		utils.HandleBadRequest(c, err.Error(), nil)
		return
	}

	// 检查配额
	used, _ := h.tempService.GetQuotaUsage(ip)
	quotaPerIP, _ := configPkg.ParseQuotaString(h.config.Quota.PerIP)
	if quotaPerIP > 0 {
		if used+req.FileSize > quotaPerIP {
			utils.HandleBadRequest(c, fmt.Sprintf("超出IP配额限制（已用：%d 字节，配额：%d 字节）",
				used, quotaPerIP), nil)
			return
		}
	}

	// 验证 Dir 路径安全性
	if req.Dir != "" {
		cleanDir := filepath.Clean(req.Dir)
		target := filepath.Join(h.config.Path, cleanDir)
		if !isPathWithinRoot(h.config.Path, target) {
			utils.HandleBadRequest(c, "不允许的路径", nil)
			return
		}
	}

	// 生成访问码（128bit 熵，作为分享/下载凭证与最终文件名来源）
	code, err := generateTempCode()
	if err != nil {
		utils.Error("生成访问码失败", utils.Err(err))
		utils.HandleInternalServerError(c, "生成访问码失败")
		return
	}

	cs, err := h.uploadSvc.CreateSession(&services.CreateSessionReq{
		Filename:         req.Filename,
		FileSize:         req.FileSize,
		Dir:              req.Dir,
		RootName:         "temp",
		TargetType:       models.TargetTypeTemp,
		ClientIP:         ip,
		Code:             code,
		DeleteOnDownload: req.DeleteOnDownload,
	})
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	utils.HandleSuccess(c, http.StatusCreated, "", gin.H{
		"uploadId":    cs.ID,
		"code":        code,
		"totalChunks": cs.TotalChunks,
		"chunkSize":   cs.ChunkSize,
		"expiredAt":   cs.ExpiredAt,
	})
}

// GetTempSessionHandler 处理获取临时文件上传会话状态
func (h *Handler) GetTempSessionHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	session, err := h.verifyTempSession(c, c.Param("uploadId"))
	if err != nil {
		handleTempSessionErr(c, err)
		return
	}

	status, err := h.uploadSvc.GetStatus(session.ID)
	if err != nil {
		utils.HandleNotFound(c, err.Error())
		return
	}

	uploadedSize := int64(len(status.UploadedIndexes)) * status.ChunkSize
	if uploadedSize > status.FileSize {
		uploadedSize = status.FileSize
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"session": gin.H{
			"uploadId":        status.ID,
			"code":            session.Code,
			"filename":        status.FileName,
			"fileSize":        status.FileSize,
			"totalChunks":     status.TotalChunks,
			"chunkSize":       status.ChunkSize,
			"uploadedSize":    uploadedSize,
			"uploadedIndexes": status.UploadedIndexes,
			"expiredAt":       status.ExpiredAt,
		},
	})
}

// UploadTempChunkHandler 处理上传临时文件分片
func (h *Handler) UploadTempChunkHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	uploadID := c.PostForm("uploadId")
	chunkIndexStr := c.PostForm("chunkIndex")
	checksum := c.PostForm("checksum")

	if uploadID == "" || chunkIndexStr == "" {
		utils.HandleBadRequest(c, "缺少必要参数", nil)
		return
	}

	session, err := h.verifyTempSession(c, uploadID)
	if err != nil {
		handleTempSessionErr(c, err)
		return
	}

	chunkIndex, err := strconv.Atoi(chunkIndexStr)
	if err != nil {
		utils.HandleBadRequest(c, "无效的分片索引", nil)
		return
	}

	// 幂等：分片已上传时直接返回成功，避免重试触发唯一索引冲突
	indexes, _ := repositories.NewChunkRepository(h.db).GetUploadedIndexes(session.ID)
	for _, i := range indexes {
		if i == chunkIndex {
			utils.HandleSuccess(c, http.StatusOK, "", gin.H{"chunkIndex": chunkIndex, "checksum": checksum})
			return
		}
	}

	file, err := c.FormFile("chunk")
	if err != nil {
		utils.HandleBadRequest(c, "获取分片文件失败", nil)
		return
	}
	src, err := file.Open()
	if err != nil {
		utils.HandleBadRequest(c, "打开分片文件失败", nil)
		return
	}
	defer src.Close()

	if err := h.uploadSvc.UploadChunk(&services.UploadChunkReq{
		UploadID:   session.ID,
		ChunkIndex: chunkIndex,
		ChunkData:  src,
		ChunkSize:  file.Size,
		Checksum:   checksum,
	}); err != nil {
		utils.HandleErrorCompat(c, http.StatusBadRequest, err.Error(), nil)
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{"chunkIndex": chunkIndex, "checksum": checksum})
}

// ResumeTempSessionHandler 处理恢复临时文件上传会话
func (h *Handler) ResumeTempSessionHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	session, err := h.verifyTempSession(c, c.Param("uploadId"))
	if err != nil {
		handleTempSessionErr(c, err)
		return
	}

	status, err := h.uploadSvc.Resume(session.ID)
	if err != nil {
		utils.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	uploadedSize := int64(len(status.UploadedIndexes)) * status.ChunkSize
	if uploadedSize > status.FileSize {
		uploadedSize = status.FileSize
	}

	utils.HandleSuccess(c, http.StatusOK, "会话已恢复", gin.H{
		"uploadId":        status.ID,
		"uploadedIndexes": status.UploadedIndexes,
		"uploadedSize":    uploadedSize,
		"totalChunks":     status.TotalChunks,
		"chunkSize":       status.ChunkSize,
		"expiredAt":       status.ExpiredAt,
	})
}

// CancelTempSessionHandler 处理取消临时文件上传会话
func (h *Handler) CancelTempSessionHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	session, err := h.verifyTempSession(c, c.Param("uploadId"))
	if err != nil {
		handleTempSessionErr(c, err)
		return
	}

	if err := h.uploadSvc.Cancel(session.ID); err != nil {
		utils.HandleNotFound(c, err.Error())
		return
	}

	middleware.LogOperation(c, "temp.upload.session.cancel", c.Param("uploadId"), nil)
	utils.HandleSuccess(c, http.StatusOK, "会话已取消", nil)
}

// FinalizeTempUploadHandler 处理完成临时文件上传
func (h *Handler) FinalizeTempUploadHandler(c *gin.Context) {
	if !h.config.Enabled {
		utils.HandleBadRequest(c, "临时文件功能已禁用", nil)
		return
	}

	uploadID := c.PostForm("uploadId")
	if uploadID == "" {
		// 前端 UploadManager 发送 JSON body，尝试解析
		var body struct {
			UploadID string `json:"uploadId"`
		}
		if err := c.ShouldBindJSON(&body); err == nil && body.UploadID != "" {
			uploadID = body.UploadID
		}
	}

	session, err := h.verifyTempSession(c, uploadID)
	if err != nil {
		handleTempSessionErr(c, err)
		return
	}

	result, err := h.uploadSvc.Finalize(session.ID, false)
	if err != nil {
		if errors.Is(err, services.ErrFileExists) {
			utils.HandleErrorCompat(c, http.StatusConflict, "目标文件已存在", nil)
			return
		}
		utils.HandleErrorCompat(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	expireDays := h.config.DefaultExpireDays
	if expireDays <= 0 {
		expireDays = 7
	}
	expiredAt := utils.Now().Add(time.Duration(expireDays) * 24 * time.Hour)

	utils.HandleSuccess(c, http.StatusCreated, "", gin.H{
		"code":        result.ShareCode,
		"filename":    result.FileName,
		"fileSize":    session.FileSize,
		"expiredAt":   expiredAt,
		"downloadUrl": fmt.Sprintf("/api/v1/temp/%s/download", result.ShareCode),
	})
}

// CleanupTempSessions 清理过期的临时上传会话（统一会话表，供定时任务调用）
func (h *Handler) CleanupTempSessions() int {
	if !h.config.Enabled {
		return 0
	}

	n, err := h.uploadSvc.CleanupExpired()
	if err != nil {
		utils.Error("清理过期临时上传会话失败", utils.Err(err))
		return 0
	}
	if n > 0 {
		utils.Info("已清理过期临时上传会话", utils.Int("count", n))
	}
	return n
}
