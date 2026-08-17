package migration

import (
    "context"
    "sync"
    "time"
)

// LockService 迁移锁服务
type LockService struct {
    mu           sync.Mutex
    locked       bool
    lockedAt     time.Time
    lockedBy     string // 迁移ID
    ttl          time.Duration
    releaseTimer *time.Timer
    ctx          context.Context
    cancel       context.CancelFunc
}

// GlobalMigrationLock 全局迁移锁
var GlobalMigrationLock *LockService

// InitLockService 初始化全局锁服务
func InitLockService(ttl time.Duration) *LockService {
    if GlobalMigrationLock != nil {
        return GlobalMigrationLock
    }
    GlobalMigrationLock = &LockService{
        ttl: ttl,
        ctx: context.Background(),
    }
    return GlobalMigrationLock
}

// AcquireLock 获取迁移锁
// 返回 true 表示获取成功，false 表示锁已被占用
func (l *LockService) AcquireLock(migrationID string) bool {
    l.mu.Lock()
    defer l.mu.Unlock()

    if l.locked {
        return false
    }

    l.locked = true
    l.lockedAt = time.Now()
    l.lockedBy = migrationID

    // 设置锁超时
    if l.ttl > 0 {
        l.ctx, l.cancel = context.WithCancel(context.Background())
        l.releaseTimer = time.AfterFunc(l.ttl, func() {
            l.ForceRelease()
        })
    }

    return true
}

// ReleaseLock 释放迁移锁
// 只有锁的持有者才能释放
func (l *LockService) ReleaseLock(migrationID string) bool {
    l.mu.Lock()
    defer l.mu.Unlock()

    if !l.locked || l.lockedBy != migrationID {
        return false
    }

    l.forceReleaseLocked()
    return true
}

// ForceRelease 强制释放锁
func (l *LockService) ForceRelease() {
    l.mu.Lock()
    defer l.mu.Unlock()
    l.forceReleaseLocked()
}

// forceReleaseLocked 内部释放锁方法 (需要持有锁)
func (l *LockService) forceReleaseLocked() {
    l.locked = false
    l.lockedBy = ""
    l.lockedAt = time.Time{}

    if l.releaseTimer != nil {
        l.releaseTimer.Stop()
        l.releaseTimer = nil
    }
    if l.cancel != nil {
        l.cancel()
        l.cancel = nil
    }
}

// IsLocked 检查锁状态
func (l *LockService) IsLocked() bool {
    l.mu.Lock()
    defer l.mu.Unlock()
    return l.locked
}

// IsLockedByMe 检查是否是当前迁移持有锁
func (l *LockService) IsLockedByMe(migrationID string) bool {
    l.mu.Lock()
    defer l.mu.Unlock()
    return l.locked && l.lockedBy == migrationID
}

// GetLockHolder 获取锁持有者
func (l *LockService) GetLockHolder() string {
    l.mu.Lock()
    defer l.mu.Unlock()
    return l.lockedBy
}

// GetLockAge 获取锁持有时间
func (l *LockService) GetLockAge() time.Duration {
    l.mu.Lock()
    defer l.mu.Unlock()
    if !l.locked {
        return 0
    }
    return time.Since(l.lockedAt)
}

// WaitForLock 等待锁释放
// ctx 取消时返回
func (l *LockService) WaitForLock(ctx context.Context, timeout time.Duration) error {
    deadline, ok := ctx.Deadline()
    if ok {
        timeout = min(timeout, time.Until(deadline))
    }

    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    timeoutTimer := time.NewTimer(timeout)
    defer timeoutTimer.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-timeoutTimer.C:
            return context.DeadlineExceeded
        case <-ticker.C:
            l.mu.Lock()
            if !l.locked {
                l.mu.Unlock()
                return nil
            }
            l.mu.Unlock()
        }
    }
}

// min 返回较小值
func min(a, b time.Duration) time.Duration {
    if a < b {
        return a
    }
    return b
}