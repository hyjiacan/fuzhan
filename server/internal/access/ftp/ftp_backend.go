package ftp

// FtpBackend FTP 虚拟文件系统所需的后端能力，由服务层实现并注入。
// 独立于 FTP 传输层，使权限/索引/配额逻辑复用 Web 上传体系，
// 避免 FTP 成为绕过权限模型的旁路。
type FtpBackend interface {
	// IsAdmin 判断用户是否为管理员（匿名/未知返回 false）。
	IsAdmin(userUUID string) bool

	// PublicUploaderIP 返回公开文件索引记录中的上传者 IP（无记录返回空串）。
	PublicUploaderIP(rootName, relPath string) string

	// UploadEnabled 公共目录上传总开关（关闭时拒绝所有公共目录写操作）。
	UploadEnabled() bool

	// CheckDiskSpace 检查路径所在分区剩余空间是否足够。
	CheckDiskSpace(path string, required int64) error

	// CheckPrivateQuota 检查用户私有存储配额（配额 <=0 视为不限制）。
	CheckPrivateQuota(userUUID string, addSize int64) error

	// SyncPublicFile 将公开文件同步进索引：写 file_records_public、
	// 标记最近已同步（防止 watcher 重复处理）、更新上传者 IP、触发哈希。
	SyncPublicFile(rootName, relPath, uploaderIP string)

	// RemovePublicFile 软删除公开文件索引。
	RemovePublicFile(rootName, relPath string)

	// MovePublicFile 同步公开文件重命名/移动后的索引路径。
	MovePublicFile(rootName, oldRelPath, newRelPath string)

	// AddPrivateFile 写入私有文件记录（含分享码）并触发哈希。
	AddPrivateFile(userUUID, relPath, fileName string, size int64) error

	// SoftDeletePrivateFile 软删除私有文件记录。
	SoftDeletePrivateFile(userUUID, relPath string) error

	// RenamePrivateFile 同步私有文件重命名/移动后的记录路径。
	RenamePrivateFile(userUUID, oldRelPath, newRelPath string) error
}
