package services

import (
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strconv"
	"strings"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
)

// UploadStorage 分片上传的落盘路径策略。
// 无论何种存储类型，分片上传流程都一致：分片写入 uploading 临时文件，
// Finalize 时转为最终的 target 正式文件。不同存储的唯一差异是这两个路径
// 落在哪——公开：共享根目录下的逻辑路径+真实文件名；私有：用户私有根目录下
// 的逻辑路径+真实文件名。分享码属于业务凭证，不参与路径构造。
type UploadStorage interface {
	// Paths 返回最终目标文件路径与上传中临时文件路径（含路径越界校验，不创建目录）。
	// 会话属于远端存储后端时，返回的 targetPath/uploadingPath 为逻辑路径（虚拟前缀），
	// 供 Storage 实例操作；本地后端返回物理绝对路径。
	Paths(session *models.UploadSession) (targetPath string, uploadingPath string, err error)

	// ChunkObjectPaths 返回远端存储后端的"每片独立临时对象"路径。
	// 仅当会话的所属根目录使用远端后端时由流程调用；本地后端返回 ok=false，
	// 流程保持"单 uploading 文件随机写 + 最终重命名"的既有实现。
	// 返回 (分片目录逻辑路径, 指定分片索引的对象逻辑路径, ok)。
	ChunkObjectPaths(session *models.UploadSession, chunkIndex int) (dirPath string, objectPath string, ok bool)
}

// IsRemoteSession 判断会话所属根目录是否使用远端存储后端。
// 该判定以全局存储配置为准；私有/临时等本地子系统不参与远端分片降级。
func IsRemoteSession(session *models.UploadSession) bool {
	return appconfig.StorageBackendIsRemote() && session.TargetType == models.TargetTypeRegular
}

// chunkObjectSuffix 远端分片对象名需含"上传进行中"标记，避免与正式文件同目录混淆。
const uploadsChunkDirName = ".chunks"

// EnsureUploadDir 确保上传目录存在（uploading 临时文件所在的父目录）。
// MkdirAll(filepath.Dir(uploadingPath)) 对任意存储的上传落点都成立，
// 因此作为流程内的通用辅助函数，而非存储策略的一部分。
func EnsureUploadDir(uploadingPath string) error {
	if err := os.MkdirAll(filepath.Dir(uploadingPath), 0755); err != nil {
		return fmt.Errorf("创建上传目录失败: %w", err)
	}
	return nil
}

// PublicStorage 公开上传的落盘路径策略。
// 目标文件落在共享根目录下的逻辑目录+真实文件名；上传中临时文件为同目录下的
// `.{basename}.uploading`，二者路径语义一致、与分享无关。
type PublicStorage struct{}

// Paths 实现公开上传的路径构建与校验（源自原 UploadSessionService.resolvePaths）。
// 本地后端返回物理绝对路径（targetPath/uploadingPath 用 os 文件语义）；
// 远端后端返回逻辑相对路径（不含根前缀，以 / 分隔，供 Storage 实例内部拼接虚拟前缀）。
func (PublicStorage) Paths(session *models.UploadSession) (targetPath string, uploadingPath string, err error) {
	remote := IsRemoteSession(session)
	joiner := filepath.Join
	if remote {
		joiner = pathpkg.Join
	}

	rootPath := appconfig.RootNames[session.TargetRoot]
	if rootPath == "" {
		// 带上实际的根目录名与当前已配置的根目录清单，便于直接定位是配置问题还是请求参数问题
		err = fmt.Errorf("无效的目标根目录: %q（已配置的根目录: %v）",
			session.TargetRoot, appconfig.RootNameKeys())
		return
	}

	cleanDir := session.TargetPath
	if cleanDir == "" || cleanDir == "/" {
		cleanDir = ""
	} else {
		cleanDir = filepath.Clean(cleanDir)
	}
	if filepath.IsAbs(cleanDir) || cleanDir == ".." || (len(cleanDir) > 256 && cleanDir != "") {
		err = fmt.Errorf("无效的目标路径: %q", session.TargetPath)
		return
	}

	cleanDir = strings.TrimPrefix(cleanDir, "/")
	if remote {
		// 远端：Storage 实例已以根前缀 rootPath 为 prefix，此处仅返回不含根前缀的
		// 相对逻辑路径（/ 分隔）。目标为 dir/filename，uploading 为同目录 .basename.uploading。
		cleanFilename := filepath.Base(session.FileName)
		if cleanFilename == "" || cleanFilename == ".." || strings.ContainsAny(cleanFilename, "/\\") {
			err = fmt.Errorf("无效的文件名: %q", session.FileName)
			return
		}
		targetPath = joinRemotePath(cleanDir, cleanFilename)
		uploadingPath = pathpkg.Join(pathpkg.Dir(targetPath), "."+pathpkg.Base(targetPath)+".uploading")
		return
	}

	targetDir := joiner(rootPath, cleanDir)
	absTargetDir, absErr := filepath.Abs(targetDir)
	if absErr != nil {
		err = fmt.Errorf("目标路径解析失败: %q: %w", targetDir, absErr)
		return
	}
	absRootPath, absErr := filepath.Abs(rootPath)
	if absErr != nil {
		err = fmt.Errorf("根路径解析失败: %q: %w", rootPath, absErr)
		return
	}
	if absTargetDir != absRootPath && !strings.HasPrefix(absTargetDir, absRootPath+string(os.PathSeparator)) {
		err = fmt.Errorf("路径越界: 目标 %q 不在根目录 %q 内", absTargetDir, absRootPath)
		return
	}

	cleanFilename := filepath.Clean(session.FileName)
	if strings.Contains(cleanFilename, "..") || strings.Contains(cleanFilename, "/") || strings.Contains(cleanFilename, "\\") {
		err = fmt.Errorf("无效的文件名: %q", session.FileName)
		return
	}

	targetPath = joiner(targetDir, cleanFilename)
	uploadingPath = joiner(filepath.Dir(targetPath), "."+filepath.Base(targetPath)+".uploading")

	absTarget, _ := filepath.Abs(targetPath)
	if !strings.HasPrefix(absTarget, absRootPath+string(os.PathSeparator)) {
		err = fmt.Errorf("路径越界: 目标文件 %q 不在根目录 %q 内", absTarget, absRootPath)
		return
	}

	return
}

// joinRemotePath 拼接远端相对逻辑路径（dir 可能为空；文件名不允许为目录）。
func joinRemotePath(dir, file string) string {
	if dir == "" {
		return file
	}
	return pathpkg.Join(dir, file)
}

// ChunkObjectPaths 实现远端公开存储的"每片独立临时对象"路径。
// 所有分片存放在逻辑根目录下隐藏目录 .chunks/{ChunkDir}/ 中，与正式文件隔离；
// 每个分片为独立的临时对象 {index}.part，乱序上传互不影响（对象存储无文件句柄）。
// 仅公开（Regular）会话可为远端，因此直接在 PublicStorage 上实现。
// 返回不含根前缀的相对逻辑路径（Storage 实例已以根前缀为 prefix）。
func (PublicStorage) ChunkObjectPaths(session *models.UploadSession, chunkIndex int) (dirPath string, objectPath string, ok bool) {
	if session.ChunkDir == "" {
		return "", "", false
	}
	dirPath = pathpkg.Join(uploadsChunkDirName, session.ChunkDir)
	objectPath = pathpkg.Join(dirPath, strconv.FormatInt(int64(chunkIndex), 10)+".part")
	return dirPath, objectPath, true
}
