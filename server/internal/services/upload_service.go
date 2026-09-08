package services

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"
	"fuzhan/pkg/pathutils"
)

// UploadService 上传服务
type UploadService struct {
	fileService *FileService
}

// NewUploadService 创建上传服务实例
func NewUploadService(fileService *FileService) *UploadService {
	return &UploadService{
		fileService: fileService,
	}
}

// UploadFile 上传文件
func (us *UploadService) UploadFile(w http.ResponseWriter, r *http.Request) {
	utils.PrintRequestInfo(r)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		utils.Warn("解析表单错误", utils.Err(err))
		utils.EncodeResponse(w, nil, "解析表单错误", http.StatusBadRequest)
		return
	}
	uploadDir := r.FormValue("dir")
	uploadDir, _ = pathutils.URLDecode(uploadDir)
	if len(appconfig.GlobalConfig.Storage.Public.RootDirs) == 0 {
		utils.Warn("未配置根目录")
		utils.EncodeResponse(w, nil, "未配置根目录", http.StatusBadRequest)
		return
	}
	parts := strings.SplitN(uploadDir, "/", 2)
	var rootName, subPath string
	if len(parts) == 1 {
		rootName = parts[0]
		subPath = ""
	} else {
		rootName = parts[0]
		subPath = parts[1]
	}
	if _, exists := appconfig.RootNames[rootName]; !exists {
		utils.Warn("指定的根目录名不存在", utils.String("root_name", rootName))
		utils.EncodeResponse(w, nil, "指定的目录不存在", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		utils.Warn("上传请求中未包含文件")
		utils.EncodeResponse(w, nil, "上传请求中未包含文件", http.StatusBadRequest)
		return
	}
	defer file.Close()
	if header.Filename == "" {
		utils.Warn("未选择要上传的文件")
		utils.EncodeResponse(w, nil, "未选择要上传的文件", http.StatusBadRequest)
		return
	}
	if !pathutils.IsFileAllowed(appconfig.GlobalConfig.Storage.AllowedExtensions, header.Filename) {
		utils.Warn("不支持的文件类型", utils.String("filename", header.Filename))
		utils.EncodeResponse(w, nil, "不支持的文件类型", http.StatusBadRequest)
		return
	}

	// 检查文件大小是否超过最大限制
	if appconfig.GlobalConfig.Upload.MaxFileSize > 0 && header.Size > appconfig.GlobalConfig.Upload.MaxFileSize {
		utils.Warn("文件大小超过限制",
			utils.Int64("size", header.Size),
			utils.Int64("max_size", appconfig.GlobalConfig.Upload.MaxFileSize))
		utils.EncodeResponse(w, nil, fmt.Sprintf("文件大小超过限制: %.2f MB (最大允许: %.2f MB)",
			float64(header.Size)/(1024*1024), float64(appconfig.GlobalConfig.Upload.MaxFileSize)/(1024*1024)), http.StatusBadRequest)
		return
	}
	// 使用 PathValidator 验证路径
	validator := utils.NewPathValidatorWithRoots(rootName, appconfig.RootNames)
	if validator == nil {
		utils.EncodeResponse(w, nil, "无效的根目录", http.StatusBadRequest)
		return
	}
	saveFilename := validator.Join(subPath, header.Filename)
	absSaveFilename, _ := filepath.Abs(saveFilename)

	if err := validator.Validate(saveFilename); err != nil {
		utils.EncodeResponse(w, nil, "路径越权访问", http.StatusForbidden)
		return
	}

	// 检查配额

	// 检查 absSaveFilename 文件所在的目录是否存在，如果不存在则创建
	if !utils.CreateDirectory(filepath.Dir(absSaveFilename), w) {
		return
	}

	if _, err := os.Stat(absSaveFilename); !os.IsNotExist(err) {
		utils.Warn("文件已经存在")
		utils.EncodeResponse(w, nil, "文件已经存在", http.StatusBadRequest)
		return
	}
	outFile, err := os.Create(absSaveFilename)
	if err != nil {
		utils.Error("创建文件失败", utils.Err(err))
		utils.EncodeResponse(w, nil, "创建文件失败", http.StatusInternalServerError)
		return
	}
	defer outFile.Close()
	if _, err := io.Copy(outFile, file); err != nil {
		utils.Error("保存上传文件时出错", utils.Err(err))
		utils.EncodeResponse(w, nil, "保存文件失败", http.StatusInternalServerError)
		return
	}
	utils.Info("文件上传成功",
		utils.String("filename", header.Filename),
		utils.String("path", saveFilename))

	// 记录上传信息
	ipAddress := utils.GetRealIP(r)
	us.fileService.AddUploadRecord(ipAddress, header.Filename, saveFilename)

	utils.EncodeResponse(w, map[string]string{
		"fileName": header.Filename,
	}, "", http.StatusOK)
}
