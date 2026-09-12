package webdav

import (
	"errors"
	"os"
)

// stubBackend 是 Backend 的测试替身：可控制上传开关/校验结果，并记录调用。
type stubBackend struct {
	uploadEnabled bool
	disallowExt   string       // 非空时，该扩展名的文件会被拒绝
	quotaBlock    bool         // true 时配额校验返回错误
	diskBlock     bool         // true 时磁盘空间返回错误
	syncRoot      string       // 最近一次 SyncPublicFile 的 rootName
	syncRel       string       // 最近一次 SyncPublicFile 的 relPath
	syncIP        string       // 最近一次 SyncPublicFile 的上传者 IP
	privateAdded  []privateAdd // AddPrivateFile 调用记录
}

type privateAdd struct {
	userUUID string
	relPath  string
	fileName string
	size     int64
}

func (s *stubBackend) UploadEnabled() bool { return s.uploadEnabled }

func (s *stubBackend) IsAdmin(string) bool { return false }

func (s *stubBackend) CheckDiskSpace(_ string, _ int64) error {
	if s.diskBlock {
		return os.ErrPermission
	}
	return nil
}

func (s *stubBackend) CheckPrivateQuota(_ string, _ int64) error {
	if s.quotaBlock {
		return os.ErrPermission
	}
	return nil
}

func (s *stubBackend) ValidateExtension(filename string) error {
	if s.disallowExt != "" && filename != "" {
		if len(filename) >= len(s.disallowExt) && filename[len(filename)-len(s.disallowExt):] == s.disallowExt {
			return errors.New("disallowed extension")
		}
	}
	return nil
}

func (s *stubBackend) SyncPublicFile(rootName, relPath, uploaderIP string) {
	s.syncRoot = rootName
	s.syncRel = relPath
	s.syncIP = uploaderIP
}

func (s *stubBackend) RemovePublicFile(_, _ string) {}

func (s *stubBackend) MovePublicFile(_, _, _ string) {}

func (s *stubBackend) AddPrivateFile(userUUID, relPath, fileName string, size int64) error {
	if s.quotaBlock {
		return os.ErrPermission
	}
	s.privateAdded = append(s.privateAdded, privateAdd{userUUID, relPath, fileName, size})
	return nil
}

func (s *stubBackend) SoftDeletePrivateFile(_, _ string) error { return nil }

func (s *stubBackend) RenamePrivateFile(_, _, _ string) error { return nil }
