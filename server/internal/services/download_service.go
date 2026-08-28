package services

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/pkg/pathutils"
	"fuzhan/internal/utils"
)

// recordDownload 安全记录下载操作（带超时保护）
func recordDownload(repo RecordRepository, record *models.OperationRecord) {
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()

		done := make(chan struct{})
		go func() {
			if err := repo.Create(record); err != nil {
				utils.Error("记录下载操作失败", utils.String("file", record.FileName), utils.Err(err))
			}
			close(done)
		}()

		select {
		case <-done:
		case <-timer.C:
		}
	}()
}

// RecordRepository 接口用于记录下载
type RecordRepository interface {
	Create(record *models.OperationRecord) error
}

// isHashPath 判断路径是否为单段 xxh3 64位哈希（16位十六进制字符串）
func isHashPath(path string) bool {
	// 路径不能为空，不能包含目录分隔符
	if path == "" || strings.Contains(path, "/") {
		return false
	}
	// xxh3 64位哈希为 16 位十六进制字符串
	if len(path) != 16 {
		return false
	}
	for _, c := range path {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// DownloadService 下载服务
type DownloadService struct {
	recordRepo  RecordRepository
	searchService *SearchService
}

// NewDownloadServiceWithRepo 创建带记录功能的下载服务
func NewDownloadServiceWithRepo(repo RecordRepository, searchService *SearchService) *DownloadService {
	return &DownloadService{
		recordRepo:    repo,
		searchService: searchService,
	}
}

// DownloadByHash 根据 xxh3 哈希下载文件
func (ds *DownloadService) DownloadByHash(w http.ResponseWriter, r *http.Request) {
	utils.PrintRequestInfo(r)

	// 获取 hash 从路径: /api/v1/download/<hash> 或 /download/<hash>
	hash := strings.TrimPrefix(r.URL.Path, "/api/v1/download/")
	hash = strings.TrimPrefix(hash, "/download/")
	hash = strings.TrimSpace(hash)

	if hash == "" {
		utils.Warn("下载请求缺少哈希值")
		utils.EncodeResponse(w, nil, "哈希值不能为空", http.StatusBadRequest)
		return
	}

	// 通过哈希查找文件路径
	fullPath, rootName, err := ds.searchService.FindFilePathByHash(hash)
	if err != nil {
		utils.Warn("未找到对应文件", utils.String("hash", hash), utils.Err(err))
		utils.EncodeResponse(w, nil, "未找到对应文件", http.StatusNotFound)
		return
	}

	// 获取根目录路径
	rootPath, exists := appconfig.RootNames[rootName]
	if !exists {
		utils.Warn("根目录不存在", utils.String("rootName", rootName))
		utils.EncodeResponse(w, nil, "根目录不存在", http.StatusBadRequest)
		return
	}

	// 获取绝对路径
	absRootPath, _ := filepath.Abs(rootPath)
	absTargetPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absTargetPath, absRootPath) {
		utils.Warn("检测到路径越权尝试", utils.String("path", absTargetPath))
		utils.EncodeResponse(w, nil, "路径越权", http.StatusBadRequest)
		return
	}

	targetPath := absTargetPath

	info, err := os.Stat(targetPath)
	if err != nil || info.IsDir() {
		utils.Warn("指定文件不存在", utils.String("path", targetPath))
		utils.EncodeResponse(w, nil, "指定的文件不存在", http.StatusBadRequest)
		return
	}

	// 记录下载操作（非阻塞，仅记录 GET 请求）
	if ds.recordRepo != nil && r.Method == http.MethodGet {
		recordDownload(ds.recordRepo, &models.OperationRecord{
			Action:    "download-by-hash",
			FileName:  filepath.Base(targetPath),
			FilePath:  filepath.Dir(fullPath),
			FullPath:  "/" + rootName + "/" + fullPath,
			FileSize:  info.Size(),
			RootName:  rootName,
			ClientIP:  utils.GetRealIP(r),
			CreatedAt: time.Now(),
		})
	}

	// 判定是否为预览请求
	isPreview := r.URL.Query().Get("preview") == "true"

	// 公共文件下载次数累加（真实下载时，不含预览）
	if !isPreview && r.Method == http.MethodGet && ds.searchService != nil {
		if ierr := ds.searchService.IncrementPublicDownloadCount(fullPath); ierr != nil {
			utils.Warn("公共文件下载次数更新失败", utils.String("path", fullPath), utils.Err(ierr))
		}
	}

	if !isPreview {
		// 仅在下载请求时设置 Content-Disposition（RFC 5987 文件名编码）
		safeFilename := url.QueryEscape(filepath.Base(targetPath))
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, filepath.Base(targetPath), safeFilename))
		// 下载文件的缓存策略：可缓存 30 天
		w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
	} else {
		// 预览请求的缓存策略：可缓存 1 小时
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	http.ServeFile(w, r, targetPath)
}

// DownloadFile 下载文件
func (ds *DownloadService) DownloadFile(w http.ResponseWriter, r *http.Request) {
	ds.downloadFile(w, r, true)
}

// AdminDownloadFile 管理页面下载文件（不记录历史）
func (ds *DownloadService) AdminDownloadFile(w http.ResponseWriter, r *http.Request) {
	ds.downloadFile(w, r, false)
}

func (ds *DownloadService) downloadFile(w http.ResponseWriter, r *http.Request, record bool) {
	utils.PrintRequestInfo(r)

	// 路径格式: /api/v1/download/根目录名/子路径
	// 管理页面路径格式: /api/v1/admin/download/根目录名/子路径
	// 别名路径格式: /download/根目录名/子路径
	filename := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/download/")
	filename = strings.TrimPrefix(filename, "/api/v1/download/")
	filename = strings.TrimPrefix(filename, "/download/")
	filename, _ = pathutils.URLDecode(filename)

	// 检测是否为按哈希下载: 路径为单段且是 16 位十六进制字符串（xxh3 64位）
	if isHashPath(filename) {
		ds.DownloadByHash(w, r)
		return
	}

	parts := strings.SplitN(filename, "/", 2)
	if len(parts) < 2 {
		utils.Warn("指定路径格式不正确，应为 '根目录名/子路径'", utils.String("path", filename))
		utils.EncodeResponse(w, nil, "路径格式错误", http.StatusBadRequest)
		return
	}
	rootName := parts[0]
	subPath := parts[1]

	// 获取根目录路径
	rootPath, exists := appconfig.RootNames[rootName]
	if !exists {
		utils.Warn("指定的根目录名不存在", utils.String("root_name", rootName))
		utils.EncodeResponse(w, nil, "指定的目录不存在", http.StatusBadRequest)
		return
	}

	// 先验证路径安全性，再拼接完整路径
	// 清理子路径中的潜在路径遍历字符
	cleanSubPath := filepath.Clean(subPath)
	if cleanSubPath == ".." || strings.HasPrefix(cleanSubPath, ".."+string(filepath.Separator)) {
		utils.Warn("检测到路径越权尝试", utils.String("path", subPath))
		utils.EncodeResponse(w, nil, "路径越权", http.StatusBadRequest)
		return
	}

	// 获取绝对路径并验证
	absRootPath, _ := filepath.Abs(rootPath)
	absTargetPath, _ := filepath.Abs(filepath.Join(rootPath, cleanSubPath))
	if !strings.HasPrefix(absTargetPath, absRootPath) {
		utils.Warn("检测到路径越权尝试", utils.String("path", absTargetPath))
		utils.EncodeResponse(w, nil, "路径越权", http.StatusBadRequest)
		return
	}

	// 路径验证通过后再拼接
	targetPath := absTargetPath

	info, err := os.Stat(targetPath)
	if err != nil || info.IsDir() {
		utils.Warn("指定路径不存在", utils.String("path", targetPath))
		utils.EncodeResponse(w, nil, "指定的文件不存在", http.StatusBadRequest)
		return
	}

	// 记录下载操作（非阻塞，仅记录 GET 请求）
	if record && ds.recordRepo != nil && r.Method == http.MethodGet {
		recordDownload(ds.recordRepo, &models.OperationRecord{
			Action:    "download",
			FileName:  filepath.Base(targetPath),
			FilePath:  subPath,
			FullPath:  "/" + rootName + "/" + strings.TrimPrefix(subPath, "/"),
			FileSize:  info.Size(),
			RootName:  rootName,
			ClientIP:  utils.GetRealIP(r),
			CreatedAt: time.Now(),
		})
	}

	// 公共文件下载次数累加（真实下载时，不含预览）
	if record && r.Method == http.MethodGet && r.URL.Query().Get("preview") != "true" && ds.searchService != nil {
		if ierr := ds.searchService.IncrementPublicDownloadCount("/" + rootName + "/" + strings.TrimPrefix(subPath, "/")); ierr != nil {
			utils.Warn("公共文件下载次数更新失败", utils.String("path", subPath), utils.Err(ierr))
		}
	}

	// 判定是否为预览请求
	isPreview := r.URL.Query().Get("preview") == "true"
	if !isPreview {
		// 仅在下载请求时设置 Content-Disposition（RFC 5987 文件名编码）
		safeFilename := url.QueryEscape(filepath.Base(targetPath))
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, filepath.Base(targetPath), safeFilename))
		// 下载文件的缓存策略：可缓存 30 天
		w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
	} else {
		// 预览请求的缓存策略：可缓存 1 小时
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	http.ServeFile(w, r, targetPath)
}

// Close 关闭下载服务
func (ds *DownloadService) Close() error {
	return nil
}

// Validate 验证下载服务配置
func (ds *DownloadService) Validate() error {
	return nil
}