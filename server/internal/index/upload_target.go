package index

import "sync"

// BeginUploadTarget 登记"正在上传的目标路径"，返回幂等的注销函数。
// watcher 在登记有效期内忽略该路径的 Remove 事件：覆盖上传用 os.Rename 原子替换
// 目标文件时，Windows 会先上报目标路径的 Remove 再上报 Rename；若按 Remove 软删索引，
// 上传收尾刚写入的 active 记录会被抹掉，文件仍在磁盘上却从列表消失，再次上传也会
// 因查不到 active 归属记录被判为目标已存在。
func (s *Service) BeginUploadTarget(rootName, relPath string) func() {
	key := syncedKey(rootName, relPath)

	s.uploadTargetsMu.Lock()
	s.uploadTargets[key]++
	s.uploadTargetsMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			s.uploadTargetsMu.Lock()
			if n := s.uploadTargets[key]; n <= 1 {
				delete(s.uploadTargets, key)
			} else {
				s.uploadTargets[key] = n - 1
			}
			s.uploadTargetsMu.Unlock()
		})
	}
}

// IsUploadTarget 判断路径是否处于上传中。
func (s *Service) IsUploadTarget(rootName, relPath string) bool {
	key := syncedKey(rootName, relPath)
	s.uploadTargetsMu.Lock()
	defer s.uploadTargetsMu.Unlock()
	return s.uploadTargets[key] > 0
}
