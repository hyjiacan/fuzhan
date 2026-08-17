package migration

import (
    "context"
    "sync"
    "testing"
    "time"
)

// TestLockService_AcquireLock tests acquiring a lock
func TestLockService_AcquireLock(t *testing.T) {
    lockService := InitLockService(5 * time.Minute)
    defer lockService.ForceRelease()

    // First acquisition should succeed
    if !lockService.AcquireLock("migration-1") {
        t.Error("首次获取锁应成功")
    }

    // After acquisition, lock should be held
    if !lockService.IsLocked() {
        t.Error("获取锁后 IsLocked 应返回 true")
    }

    // Second acquisition with different ID should fail (lock already held)
    if lockService.AcquireLock("migration-2") {
        t.Error("锁被占用时不应能再次获取锁")
    }

    // Release first lock
    lockService.ReleaseLock("migration-1")

    // After release, lock should not be held
    if lockService.IsLocked() {
        t.Error("释放锁后 IsLocked 应返回 false")
    }

    // Now second lock should succeed
    if !lockService.AcquireLock("migration-2") {
        t.Error("释放后应能获取锁")
    }

    lockService.ForceRelease()
}

// TestLockService_ReleaseLock tests releasing a lock
func TestLockService_ReleaseLock(t *testing.T) {
    lockService := InitLockService(5 * time.Minute)
    defer lockService.ForceRelease()

    lockService.AcquireLock("migration-1")

    // Release with wrong ID should fail
    if lockService.ReleaseLock("migration-2") {
        t.Error("使用错误 ID 释放锁应失败")
    }

    // Release with correct ID should succeed
    if !lockService.ReleaseLock("migration-1") {
        t.Error("使用正确 ID 释放锁应成功")
    }

    if lockService.IsLocked() {
        t.Error("锁应已释放")
    }
}

// TestLockService_ForceRelease tests force release
func TestLockService_ForceRelease(t *testing.T) {
    lockService := InitLockService(5 * time.Minute)

    lockService.AcquireLock("migration-1")

    // Force release should work regardless of ID
    lockService.ForceRelease()

    if lockService.IsLocked() {
        t.Error("锁应已释放")
    }
}

// TestLockService_IsLockedByMe tests checking lock ownership
func TestLockService_IsLockedByMe(t *testing.T) {
    lockService := InitLockService(5 * time.Minute)
    defer lockService.ForceRelease()

    lockService.AcquireLock("migration-1")

    if !lockService.IsLockedByMe("migration-1") {
        t.Error("应报告锁由 migration-1 持有")
    }

    if lockService.IsLockedByMe("migration-2") {
        t.Error("不应报告锁由 migration-2 持有")
    }
}

// TestLockService_GetLockHolder tests getting lock holder
func TestLockService_GetLockHolder(t *testing.T) {
    lockService := InitLockService(5 * time.Minute)
    defer lockService.ForceRelease()

    if lockService.GetLockHolder() != "" {
        t.Error("初始状态锁持有者应为空")
    }

    lockService.AcquireLock("migration-1")

    if lockService.GetLockHolder() != "migration-1" {
        t.Errorf("锁持有者应为 migration-1，实际为 %s", lockService.GetLockHolder())
    }
}

// TestLockService_GetLockAge tests getting lock age
func TestLockService_GetLockAge(t *testing.T) {
    lockService := InitLockService(5 * time.Minute)
    defer lockService.ForceRelease()

    if lockService.GetLockAge() != 0 {
        t.Error("未锁定时锁年龄应为 0")
    }

    lockService.AcquireLock("migration-1")

    // Wait a bit
    time.Sleep(10 * time.Millisecond)

    age := lockService.GetLockAge()
    if age < 10*time.Millisecond {
        t.Errorf("锁年龄应 >= 10ms，实际为 %v", age)
    }
}

// TestLockService_WaitForLock tests waiting for lock release
func TestLockService_WaitForLock(t *testing.T) {
    lockService := InitLockService(5 * time.Minute)
    defer lockService.ForceRelease()

    // Acquire lock
    lockService.AcquireLock("migration-1")

    // Start waiting in goroutine
    var wg sync.WaitGroup
    wg.Add(1)

    var waitErr error
    go func() {
        defer wg.Done()
        ctx := context.Background()
        waitErr = lockService.WaitForLock(ctx, 500*time.Millisecond)
    }()

    // Release after a short delay
    time.Sleep(50 * time.Millisecond)
    lockService.ReleaseLock("migration-1")

    wg.Wait()

    if waitErr != nil {
        t.Errorf("等待不应返回错误: %v", waitErr)
    }
}

// TestLockService_WaitForLock_Timeout tests wait timeout
func TestLockService_WaitForLock_Timeout(t *testing.T) {
    lockService := InitLockService(5 * time.Minute)
    defer lockService.ForceRelease()

    lockService.AcquireLock("migration-1")

    ctx := context.Background()
    err := lockService.WaitForLock(ctx, 50*time.Millisecond)

    if err == nil {
        t.Error("预期超时错误")
    }
}

// TestLockService_GlobalSingleton tests global lock service singleton
func TestLockService_GlobalSingleton(t *testing.T) {
    // Get two lock services
    lock1 := InitLockService(5 * time.Minute)
    lock2 := InitLockService(10 * time.Minute)

    // They should be the same instance
    if lock1 != lock2 {
        t.Error("InitLockService 应返回单例")
    }

    lock1.ForceRelease()
}

// TestLockService_Concurrency tests concurrent lock operations
func TestLockService_Concurrency(t *testing.T) {
    lockService := InitLockService(5 * time.Minute)
    defer lockService.ForceRelease()

    var wg sync.WaitGroup
    successCount := 0
    var mu sync.Mutex

    // Try to acquire lock from multiple goroutines
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()

            // Try multiple times
            for j := 0; j < 10; j++ {
                if lockService.AcquireLock("migration-1") {
                    mu.Lock()
                    successCount++
                    mu.Unlock()

                    time.Sleep(1 * time.Millisecond)
                    lockService.ReleaseLock("migration-1")
                }
                time.Sleep(1 * time.Millisecond)
            }
        }(i)
    }

    wg.Wait()

    // Only one should succeed at a time
    if successCount == 0 {
        t.Error("至少应有一次成功获取锁")
    }
}