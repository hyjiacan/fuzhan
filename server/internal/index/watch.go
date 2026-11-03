package index

import (
	"path/filepath"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"
)

// syncedKey 统一"最近同步/上传中"记录的键：去掉前导 /、统一为 / 分隔。
// 上传流程传入的索引路径带前导 /，watcher 解析出的是相对路径，统一后才能命中同一个键。
func syncedKey(rootName, relPath string) string {
	return rootName + ":" + strings.TrimPrefix(filepath.ToSlash(relPath), "/")
}

// MarkRecentlySynced 标记文件为最近已同步，防止 watcher 重复处理
// 上传完成后调用此方法，watcher 在处理 Create/Write 事件时会跳过此文件
func (s *Service) MarkRecentlySynced(rootName, relPath string) {
	key := syncedKey(rootName, relPath)
	s.recentlyMu.Lock()
	s.recentlySynced[key] = utils.Now()
	s.recentlyMu.Unlock()
}

// IsRecentlySynced 检查文件是否最近已同步（30 秒内）
func (s *Service) IsRecentlySynced(rootName, relPath string) bool {
	key := syncedKey(rootName, relPath)
	s.recentlyMu.Lock()
	t, ok := s.recentlySynced[key]
	s.recentlyMu.Unlock()
	if !ok {
		return false
	}
	return time.Since(t) < 30*time.Second
}

// cleanupRecentlySynced 定期清理过期的最近同步记录（每分钟执行一次）
func (s *Service) cleanupRecentlySynced() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.recentlyMu.Lock()
			now := utils.Now()
			for key, t := range s.recentlySynced {
				if now.Sub(t) > 30*time.Second {
					delete(s.recentlySynced, key)
				}
			}
			s.recentlyMu.Unlock()
		case <-s.recentlyStopCh:
			return
		}
	}
}

// StartWatcher 启动文件监听器（实时增量索引）
func (s *Service) StartWatcher() {
	s.watcher = NewWatcher(s.syncer, s.rootNames)
	// 注入最近同步检查回调，防止 watcher 重复处理上传流程已同步的文件
	s.watcher.skipRecentlySynced = s.IsRecentlySynced
	// 注入上传中目标检查回调，防止覆盖上传的瞬时 Remove 事件误删索引
	s.watcher.isUploadTarget = s.IsUploadTarget
	if err := s.watcher.Start(); err != nil {
		utils.Warn("文件监听器启动失败", utils.Err(err))
		s.watcher = nil
	}
}

// StopWatcher 停止文件监听器
func (s *Service) StopWatcher() {
	if s.watcher != nil {
		s.watcher.Stop()
		s.watcher = nil
	}
}

// RestartWatcher 重启文件监听器（用于配置热生效）
func (s *Service) RestartWatcher() {
	utils.Info("配置变更，重启文件监听器")
	s.StopWatcher()

	// 更新 rootNames（可能因配置变更）
	s.rootNames = appconfig.RootNames
	s.syncer.rootNames = appconfig.RootNames

	s.StartWatcher()
}
