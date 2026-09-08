// Package online 提供客户端 IP 在线状态跟踪。
//
// 这里的"在线"与登录状态无关，仅依据最近是否有请求判定：
// 只要某个 IP 在 IdleTimeout 内发起过任一元请求（无论是否登录），
// 即认为该 IP 当前在线。管理端通过 Snapshot 查看在线 IP 及其在线时长。
package online

import (
	"sort"
	"sync"
	"time"
)

// IdleTimeout 判断一个 IP 是否"在线"的空闲超时。
// 距上次请求超过该时长未再活动，即视为离线。
const IdleTimeout = 3 * time.Minute

// PruneAfter 内部清理阈值：距上次请求超过该时长即从记录中移除，
// 防止长期不活动的 IP 无限占用内存。
const PruneAfter = 24 * time.Hour

// Entry 单个客户端 IP 的在线记录。
type Entry struct {
	IP           string
	FirstSeen    time.Time
	LastSeen     time.Time
	RequestCount int64
	UserAgent    string
}

// Snapshot 对外返回的在线 IP 条目。
type Snapshot struct {
	IP            string `json:"ip"`
	FirstSeen     string `json:"firstSeen"`
	LastSeen      string `json:"lastSeen"`
	OnlineSeconds int64  `json:"onlineSeconds"`
	RequestCount  int64  `json:"requestCount"`
	UserAgent     string `json:"userAgent"`
}

// Tracker 记录各客户端 IP 的访问情况。
type Tracker struct {
	mu      sync.RWMutex
	entries map[string]*Entry
}

// NewTracker 创建在线跟踪器。
func NewTracker() *Tracker {
	return &Tracker{entries: make(map[string]*Entry)}
}

// Record 记录一次来自该 IP 的请求。now 由调用方传入以保证一致。
func (t *Tracker) Record(ip, userAgent string, now time.Time) {
	if ip == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[ip]
	if !ok {
		e = &Entry{IP: ip, FirstSeen: now}
		t.entries[ip] = e
	}
	e.LastSeen = now
	e.RequestCount++
	if userAgent != "" {
		e.UserAgent = userAgent
	}
}

// Snapshot 返回当前在线（距上次请求未超过 IdleTimeout）的 IP 列表，
// 顺带清理久未活动的条目。按最近活动时间倒序排列。
func (t *Tracker) Snapshot(now time.Time) []Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]Snapshot, 0, len(t.entries))
	for ip, e := range t.entries {
		idle := now.Sub(e.LastSeen)
		switch {
		case idle <= IdleTimeout:
			out = append(out, Snapshot{
				IP:            ip,
				FirstSeen:     e.FirstSeen.Format(time.RFC3339),
				LastSeen:      e.LastSeen.Format(time.RFC3339),
				OnlineSeconds: int64(now.Sub(e.FirstSeen).Seconds()),
				RequestCount:  e.RequestCount,
				UserAgent:     e.UserAgent,
			})
		case idle > PruneAfter:
			delete(t.entries, ip)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen > out[j].LastSeen
	})
	return out
}

// Default 全局在线跟踪器，由监控中间件记录、管理端查询。
var Default = NewTracker()
