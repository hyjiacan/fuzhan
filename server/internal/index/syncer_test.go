package index

import (
    "os"
    "path/filepath"
    "sync"
    "testing"

    "fuzhan/internal/models"
)

// memoNotifier 记录检索索引变更通知的 mock 实现
type memoNotifier struct {
    mu     sync.Mutex
    adds   map[int64]string // fileID -> fileName
    dels   []int64
}

func newMemoNotifier() *memoNotifier {
    return &memoNotifier{adds: make(map[int64]string)}
}

func (m *memoNotifier) IndexFile(fileID int64, fileName string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.adds[fileID] = fileName
    return nil
}

func (m *memoNotifier) Delete(fileID int64) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.dels = append(m.dels, fileID)
    return nil
}

func (m *memoNotifier) snapshot() (map[int64]string, []int64) {
    m.mu.Lock()
    defer m.mu.Unlock()
    adds := make(map[int64]string, len(m.adds))
    for k, v := range m.adds {
        adds[k] = v
    }
    dels := append([]int64(nil), m.dels...)
    return adds, dels
}

// newSyncerEnv 构造 syncer + 临时公开根目录 + mock notifier
func newSyncerEnv(t *testing.T) (*Syncer, string, *memoNotifier) {
    t.Helper()
    db := setupTestDB(t)
    root := t.TempDir()
    svc := NewService(db, map[string]string{"docs": root}, "", "")
    n := newMemoNotifier()
    svc.SetFileIndexNotifier(n)
    // 读取内部 syncer（测试同包内可直接访问）
    return svc.syncer, root, n
}

func writeFile(t *testing.T, path, content string) {
    t.Helper()
    if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(path, []byte(content), 0644); err != nil {
        t.Fatal(err)
    }
}

// 新增文件应通知检索索引 IndexFile
func TestSyncerNotifyOnSyncFile(t *testing.T) {
    syncer, root, n := newSyncerEnv(t)

    writeFile(t, filepath.Join(root, "年度报告.pdf"), "data")
    if err := syncer.SyncFile("docs", "/年度报告.pdf"); err != nil {
        t.Fatalf("SyncFile 失败: %v", err)
    }

    adds, _ := n.snapshot()
    if len(adds) != 1 {
        t.Fatalf("期望 1 条新增，实际 %v", adds)
    }
    for _, name := range adds {
        if name != "年度报告.pdf" {
            t.Fatalf("期望文件名 年度报告.pdf，实际 %s", name)
        }
    }
}

// 删除文件应通知检索索引 Delete 对应 fileID
func TestSyncerNotifyOnRemoveFile(t *testing.T) {
    syncer, root, n := newSyncerEnv(t)

    writeFile(t, filepath.Join(root, "a.txt"), "data")
    if err := syncer.SyncFile("docs", "/a.txt"); err != nil {
        t.Fatalf("SyncFile 失败: %v", err)
    }
    adds, _ := n.snapshot()
    var fileID int64
    for id := range adds {
        fileID = id
    }

    if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
        t.Fatal(err)
    }
    if err := syncer.RemoveFile("docs", "/a.txt"); err != nil {
        t.Fatalf("RemoveFile 失败: %v", err)
    }

    _, dels := n.snapshot()
    if len(dels) != 1 || dels[0] != fileID {
        t.Fatalf("期望删除 fileID=%d，实际 %v", fileID, dels)
    }
}

// 重命名文件应重新编制检索索引（按最新文件名）
func TestSyncerNotifyOnMoveFile(t *testing.T) {
    syncer, root, n := newSyncerEnv(t)

    writeFile(t, filepath.Join(root, "old_name.txt"), "data")
    if err := syncer.SyncFile("docs", "/old_name.txt"); err != nil {
        t.Fatalf("SyncFile 失败: %v", err)
    }
    adds, _ := n.snapshot()
    var fileID int64
    for id := range adds {
        fileID = id
    }

    // 重命名磁盘文件后同步移动
    if err := os.Rename(filepath.Join(root, "old_name.txt"), filepath.Join(root, "new_name.txt")); err != nil {
        t.Fatal(err)
    }
    if err := syncer.MoveFile("docs", "/old_name.txt", "/new_name.txt"); err != nil {
        t.Fatalf("MoveFile 失败: %v", err)
    }

    adds, _ = n.snapshot()
    if adds[fileID] != "new_name.txt" {
        t.Fatalf("移动后应按最新文件名重入索引，期望 new_name.txt，实际 %s", adds[fileID])
    }
}

// 未注入 notifier 时不产生副作用
func TestSyncerWithoutNotifier(t *testing.T) {
    db := setupTestDB(t)
    root := t.TempDir()
    syncer := NewSyncer(db, map[string]string{"docs": root})

    writeFile(t, filepath.Join(root, "f.txt"), "data")
    if err := syncer.SyncFile("docs", "/f.txt"); err != nil {
        t.Fatalf("SyncFile 失败: %v", err)
    }

    // 写入应成功（不因 nil notifier 报错）
    var count int64
    if err := db.Model(&models.FileRecordPublic{}).Count(&count).Error; err != nil {
        t.Fatal(err)
    }
    if count != 1 {
        t.Fatalf("期望 1 条索引记录，实际 %d", count)
    }
}