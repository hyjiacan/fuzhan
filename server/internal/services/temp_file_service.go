package services

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"github.com/zeebo/xxh3"
	"gorm.io/gorm"
)

// TempFileResult 临时文件上传结果
type TempFileResult struct {
	Code        string    `json:"code"`
	Filename    string    `json:"filename"`
	FileSize    int64     `json:"fileSize"`
	ExpiredAt   time.Time `json:"expiredAt"`
	DownloadURL string    `json:"downloadUrl"`
	FilePath    string    `json:"-"` // 实际文件路径，内部使用
}

// TempFileListResult 临时文件列表结果
type TempFileListResult struct {
	Files   []models.TempFileResponse `json:"files"`
	Subdirs []string                  `json:"subdirs"`
	Quota   models.TempQuotaInfo      `json:"quota"`
}

// TempFileService 临时文件服务
type TempFileService struct {
	db     *gorm.DB
	config TempServiceConfig
}

// TempServiceConfig 临时文件服务配置
type TempServiceConfig struct {
	Path              string
	Enabled           bool
	DefaultExpireDays int
	DeleteOnDownload  bool
	QuotaPerIP        int64
}

// NewTempFileService 创建临时文件服务实例
func NewTempFileService(db *gorm.DB, path string, defaultExpireDays int, deleteOnDownload bool, quotaPerIP int64) *TempFileService {
	return &TempFileService{
		db: db,
		config: TempServiceConfig{
			Path:              path,
			Enabled:           true,
			DefaultExpireDays: defaultExpireDays,
			DeleteOnDownload:  deleteOnDownload,
			QuotaPerIP:        quotaPerIP,
		},
	}
}

// NewTempFileServiceWithConfig 使用完整配置创建临时文件服务
func NewTempFileServiceWithConfig(db *gorm.DB, config TempServiceConfig) *TempFileService {
	return &TempFileService{
		db:     db,
		config: config,
	}
}

// generateCode 生成8位访问码
func (s *TempFileService) generateCode() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(bytes)), nil
}

// generateFilePath 生成安全的文件存储路径（使用 xxh3 哈希）
func (s *TempFileService) generateFilePath(code string) string {
	hash := xxh3.Hash([]byte(code + "fuzhan-secret"))
	safeFilename := fmt.Sprintf("%016x", hash)
	return filepath.Join(s.config.Path, safeFilename)
}

// GetQuotaUsage 获取指定 IP 的配额使用量
func (s *TempFileService) GetQuotaUsage(ip string) (int64, error) {
	var total int64
	err := s.db.Model(&models.TempFile{}).
		Where("client_ip = ? AND expired_at > ?", ip, time.Now()).
		Select("COALESCE(SUM(file_size), 0)").
		Scan(&total).Error
	return total, err
}

// Upload 上传临时文件
func (s *TempFileService) Upload(src io.Reader, filename string, ip string, dir string, fileSize int64, deleteOnDownload bool) (*TempFileResult, error) {
	// 检查配额
	quotaPerIP := s.config.QuotaPerIP
	if quotaPerIP > 0 {
		used, err := s.GetQuotaUsage(ip)
		if err != nil {
			return nil, fmt.Errorf("获取配额信息失败: %w", err)
		}
		if used+fileSize > quotaPerIP {
			return nil, fmt.Errorf("超出IP配额限制（已用：%d，配额：%d）", used, quotaPerIP)
		}
	}

	// 生成访问码
	code, err := s.generateCode()
	if err != nil {
		return nil, fmt.Errorf("生成分享码失败: %w", err)
	}

	// 确保目录存在
	if err := os.MkdirAll(s.config.Path, 0755); err != nil {
		return nil, fmt.Errorf("创建存储目录失败: %w", err)
	}

	// 保存文件
	filePath := s.generateFilePath(code)
	out, err := os.Create(filePath)
	if err != nil {
		return nil, fmt.Errorf("保存文件失败: %w", err)
	}
	defer out.Close()

	written, err := io.Copy(out, src)
	if err != nil {
		os.Remove(filePath)
		return nil, fmt.Errorf("保存文件失败: %w", err)
	}

	// 计算过期时间
	expireDays := s.config.DefaultExpireDays
	if expireDays <= 0 {
		expireDays = 7
	}
	expiredAt := time.Now().Add(time.Duration(expireDays) * 24 * time.Hour)

	// 校验目录参数
	cleanDir := ""
	if dir != "" {
		cleanDir = filepath.Clean(dir)
		if strings.HasPrefix(cleanDir, "..") || strings.HasPrefix(cleanDir, "/") || strings.HasPrefix(cleanDir, "\\") {
			os.Remove(filePath)
			return nil, fmt.Errorf("不允许的路径")
		}
	}

	// 保存记录
	tempFile := &models.TempFile{
		Code:             code,
		Filename:         filename,
		FileSize:         written,
		FilePath:         filePath,
		ClientIP:         ip,
		Dir:              cleanDir,
		DeleteOnDownload: deleteOnDownload,
		ExpiredAt:        expiredAt,
	}

	if err := s.db.Create(tempFile).Error; err != nil {
		os.Remove(filePath)
		return nil, fmt.Errorf("保存记录失败: %w", err)
	}

	// 同步到临时文件索引表
	now := time.Now()
	tempRecord := models.FileRecordTemp{
		FileRecordBase: models.FileRecordBase{
			FileName:     filename,
			FilePath:     code,
			RootName:     "temp",
			FullPath:     "/temp/" + code,
			FileSize:     written,
			IsDir:        false,
			ModTime:      now,
			Status:       models.FileStatusActive,
			OwnerID:      ip,
			LastSyncedAt: now,
		},
	}
	if err := s.db.Create(&tempRecord).Error; err != nil {
		utils.Warn("同步临时文件索引失败", utils.String("code", code), utils.Err(err))
	}

	return &TempFileResult{
		Code:        code,
		Filename:    filename,
		FileSize:    written,
		ExpiredAt:   expiredAt,
		DownloadURL: fmt.Sprintf("/api/v1/temp/%s/download", code),
		FilePath:    filePath,
	}, nil
}

// List 查询临时文件列表
func (s *TempFileService) List(ip string, downloadCode string, dirPath string) (*TempFileListResult, error) {
	var files []models.TempFile
	var subdirs []string

	if downloadCode != "" {
		err := s.db.Where("code = ? AND expired_at > ?", downloadCode, time.Now()).
			Order("created_at DESC").
			Find(&files).Error
		if err != nil {
			return nil, fmt.Errorf("查询文件列表失败: %w", err)
		}
	} else {
		query := s.db.Where("client_ip = ? AND expired_at > ?", ip, time.Now())
		if dirPath == "" || dirPath == "/" {
			query = query.Where("(dir = ? OR dir IS NULL)", "")
		} else {
			query = query.Where("dir = ?", dirPath)
		}
		if err := query.Order("created_at DESC").Find(&files).Error; err != nil {
			return nil, fmt.Errorf("查询文件列表失败: %w", err)
		}

		subdirs = s.listSubdirs(ip, dirPath)
	}

	// 计算配额
	var used int64
	if downloadCode == "" {
		used, _ = s.GetQuotaUsage(ip)
	}

	responses := make([]models.TempFileResponse, len(files))
	for i, f := range files {
		responses[i] = *f.ToResponse()
	}

	return &TempFileListResult{
		Files:   responses,
		Subdirs: subdirs,
		Quota: models.TempQuotaInfo{
			Used:  used,
			Limit: s.config.QuotaPerIP,
		},
	}, nil
}

// GetByCode 按访问码查询临时文件
func (s *TempFileService) GetByCode(code string) (*models.TempFile, error) {
	var tempFile models.TempFile
	err := s.db.Where("code = ?", code).First(&tempFile).Error
	if err != nil {
		return nil, err
	}
	return &tempFile, nil
}

// GetByCodeWithIP 按访问码查询，同时验证 IP
func (s *TempFileService) GetByCodeWithIP(code string, ip string) (*models.TempFile, error) {
	var tempFile models.TempFile
	err := s.db.Where("code = ? AND client_ip = ?", code, ip).First(&tempFile).Error
	if err != nil {
		return nil, err
	}
	return &tempFile, nil
}

// DownloadFile 获取下载文件路径（不处理流式传输，由 handler 负责）
func (s *TempFileService) DownloadFile(code string) (*models.TempFile, error) {
	tempFile, err := s.GetByCode(code)
	if err != nil {
		return nil, err
	}

	if time.Now().After(tempFile.ExpiredAt) {
		return nil, fmt.Errorf("文件已过期")
	}

	if _, err := os.Stat(tempFile.FilePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("文件不存在")
	}

	return tempFile, nil
}

// MarkDownloaded 标记文件已下载
func (s *TempFileService) MarkDownloaded(tempFile *models.TempFile, deleteOnDownload bool) {
	// 优先使用文件级别的 deleteOnDownload 设置
	autoDelete := tempFile.DeleteOnDownload || deleteOnDownload
	if autoDelete {
		s.db.Model(tempFile).Updates(map[string]interface{}{
			"downloaded":     true,
			"download_count": tempFile.DownloadCount + 1,
		})
		os.Remove(tempFile.FilePath)
	} else {
		s.db.Model(tempFile).Update("download_count", tempFile.DownloadCount+1)
	}
}

// Delete 删除临时文件（含 IP 校验）
func (s *TempFileService) Delete(code string, ip string) error {
	tempFile, err := s.GetByCodeWithIP(code, ip)
	if err != nil {
		return fmt.Errorf("文件不存在或无权限删除")
	}

	if tempFile.FilePath != "" {
		os.Remove(tempFile.FilePath)
	}
	s.db.Delete(tempFile)

	return nil
}

// CleanupExpired 清理过期文件
func (s *TempFileService) CleanupExpired() (int, error) {
	var files []models.TempFile
	if err := s.db.Where("expired_at < ? AND downloaded = ?", time.Now(), false).Find(&files).Error; err != nil {
		return 0, fmt.Errorf("查询过期文件失败: %w", err)
	}

	for _, f := range files {
		if f.FilePath != "" {
			if err := os.Remove(f.FilePath); err != nil {
				utils.Warn("删除过期文件失败", utils.String("path", f.FilePath), utils.Err(err))
			}
		}
		if err := s.db.Delete(&f).Error; err != nil {
			utils.Error("删除过期记录失败", utils.Err(err))
		}
	}

	return len(files), nil
}

// listSubdirs 获取指定路径下的子目录列表
func (s *TempFileService) listSubdirs(clientIP, parentDir string) []string {
	var allDirs []string
	if parentDir == "" || parentDir == "/" {
		s.db.Model(&models.TempFile{}).
			Where("client_ip = ? AND expired_at > ? AND dir != '' AND dir IS NOT NULL", clientIP, time.Now()).
			Pluck("DISTINCT dir", &allDirs)
	} else {
		escapedDir := strings.ReplaceAll(strings.ReplaceAll(parentDir, "%", "\\%"), "_", "\\_")
		s.db.Model(&models.TempFile{}).
			Where("client_ip = ? AND expired_at > ? AND dir LIKE ? ESCAPE '\\'", clientIP, time.Now(), escapedDir+"/%").
			Pluck("DISTINCT dir", &allDirs)
	}

	prefix := ""
	if parentDir != "" && parentDir != "/" {
		prefix = parentDir + "/"
	}

	seen := make(map[string]bool)
	var subdirs []string
	for _, d := range allDirs {
		if !strings.HasPrefix(d, prefix) {
			continue
		}
		rest := strings.TrimPrefix(d, prefix)
		if idx := strings.Index(rest, "/"); idx > 0 {
			rest = rest[:idx]
		}
		if rest != "" && !seen[rest] {
			seen[rest] = true
			subdirs = append(subdirs, rest)
		}
	}

	sort.Strings(subdirs)
	return subdirs
}
