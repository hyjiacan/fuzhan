package downloadanalytics

import (
	"sync"
	"time"
)

// maxCacheAge 缓存兜底有效期。水位机制保证「自上次计算结果以来无新增公开下载事件」时结果被无限期复用，
// 不再按固定 TTL 无条件重算；但 TopFiles 的备注/上传时间等增强字段依赖 file_records_public，可能独立变化，
// 需要兜底刷新以收敛陈旧性（同时也兜底软删除、字段更新等未进入水位追踪的变化）。
const maxCacheAge = 60 * time.Second

type cacheEntry struct {
	at   time.Time
	data interface{}
	wm   int64 // 结果计算时已消费到的 operation_records 公开下载事件水位（该水位之后若出现新事件则缓存失效）
}

// watermarkCache 按 key 的内存缓存：get 时结合兜底有效期与调用方水位判定新鲜度，命中即复用，
// 未命中则重算。条目数少，不做容量淘汰。
type watermarkCache struct {
	mu sync.Mutex
	m  map[string]cacheEntry
}

func newWatermarkCache() *watermarkCache {
	return &watermarkCache{m: make(map[string]cacheEntry)}
}

func (c *watermarkCache) get(key string) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return cacheEntry{}, false
	}
	if time.Since(e.at) > maxCacheAge {
		delete(c.m, key)
		return cacheEntry{}, false
	}
	return e, true
}

func (c *watermarkCache) set(key string, e cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = e
}
