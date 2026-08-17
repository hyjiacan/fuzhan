package index

import (
    "fmt"
    "testing"
    "time"

    "github.com/glebarez/sqlite"
    "gorm.io/gorm"
    "fuzhan/internal/models"
)

// setupTestDB 创建内存 SQLite 数据库用于测试
func setupTestDB(t *testing.T) *gorm.DB {
    db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    if err != nil {
        t.Fatalf("打开内存数据库失败: %v", err)
    }
    if err := db.AutoMigrate(&models.FileRecordPublic{}, &models.FileRecordTemp{}, &models.FileRecordPrivate{}, &models.FileDependency{}); err != nil {
        t.Fatalf("自动迁移失败: %v", err)
    }
    return db
}

// createTestRecord 创建测试文件记录
func createTestRecord(db *gorm.DB, name, path, root, hash string, size int64) uint {
    rec := models.FileRecordPublic{
        FileRecordBase: models.FileRecordBase{
            FileName: name,
            FilePath: path,
            RootName: root,
            FileSize: size,
            IsDir:    false,
            Xxh3Hash: hash,
            Status:   models.FileStatusActive,
            ModTime:  time.Now(),
        },
    }
    db.Create(&rec)
    return rec.ID
}

// ========== 测试: ListDuplicateGroups ==========

func TestListDuplicateGroups_NoDuplicates(t *testing.T) {
    db := setupTestDB(t)
    svc := NewService(db, nil, "", "")

    // 创建两个不同哈希的文件
    createTestRecord(db, "f1.txt", "/f1.txt", "docs", "hash1", 100)
    createTestRecord(db, "f2.txt", "/f2.txt", "docs", "hash2", 200)

    groups, total, err := svc.ListDuplicateGroups(DuplicateQuery{})
    if err != nil {
        t.Fatalf("查询重复分组失败: %v", err)
    }
    if total != 0 {
        t.Errorf("期望无重复分组，实际有 %d 组", total)
    }
    if len(groups) != 0 {
        t.Errorf("期望空分组列表，实际有 %d 组", len(groups))
    }
}

func TestListDuplicateGroups_WithDuplicates(t *testing.T) {
    db := setupTestDB(t)
    svc := NewService(db, nil, "", "")

    // 创建三个相同哈希的文件
    createTestRecord(db, "original.txt", "/original.txt", "docs", "samehash", 100)
    createTestRecord(db, "copy1.txt", "/subdir/copy1.txt", "docs", "samehash", 100)
    createTestRecord(db, "copy2.txt", "/subdir/copy2.txt", "docs", "samehash", 100)

    groups, total, err := svc.ListDuplicateGroups(DuplicateQuery{})
    if err != nil {
        t.Fatalf("查询重复分组失败: %v", err)
    }
    if total != 1 {
        t.Errorf("期望 1 个重复分组，实际有 %d", total)
    }
    if len(groups) != 1 {
        t.Fatalf("期望 1 组数据，实际有 %d", len(groups))
    }
    if groups[0].FileCount != 3 {
        t.Errorf("期望分组有 3 个文件，实际有 %d", groups[0].FileCount)
    }
    if len(groups[0].Files) != 3 {
        t.Errorf("期望文件详情有 3 项，实际有 %d", len(groups[0].Files))
    }
}

func TestListDuplicateGroups_ExcludesDeletedAndDirs(t *testing.T) {
    db := setupTestDB(t)
    svc := NewService(db, nil, "", "")

    createTestRecord(db, "active.txt", "/active.txt", "docs", "samehash", 100)
    createTestRecord(db, "copy.txt", "/copy.txt", "docs", "samehash", 100)

    // 已删除的文件不应计入重复
    delRec := models.FileRecordPublic{
        FileRecordBase: models.FileRecordBase{
            FileName: "deleted.txt", FilePath: "/deleted.txt", RootName: "docs",
            FileSize: 100, Xxh3Hash: "samehash", Status: models.FileStatusDeleted, ModTime: time.Now(),
        },
    }
    db.Create(&delRec)

    // 目录不应计入重复
    dirRec := models.FileRecordPublic{
        FileRecordBase: models.FileRecordBase{
            FileName: "dir", FilePath: "/dir", RootName: "docs",
            FileSize: 0, Xxh3Hash: "samehash", IsDir: true, Status: models.FileStatusActive, ModTime: time.Now(),
        },
    }
    db.Create(&dirRec)

    groups, total, err := svc.ListDuplicateGroups(DuplicateQuery{})
    if err != nil {
        t.Fatalf("查询失败: %v", err)
    }
    if total != 1 {
        t.Errorf("期望 1 个重复分组（排除删除和目录后），实际 %d", total)
    }
    if len(groups) == 1 && len(groups[0].Files) != 2 {
        t.Errorf("期望每组 2 个文件，实际 %d", len(groups[0].Files))
    }
}

func TestListDuplicateGroups_ExcludesEmptyHash(t *testing.T) {
    db := setupTestDB(t)
    svc := NewService(db, nil, "", "")

    // 空哈希的文件不应计入重复分组
    createTestRecord(db, "a.txt", "/a.txt", "docs", "", 100)
    createTestRecord(db, "b.txt", "/b.txt", "docs", "", 200)

    groups, total, err := svc.ListDuplicateGroups(DuplicateQuery{})
    if err != nil {
        t.Fatalf("查询失败: %v", err)
    }
    if total != 0 {
        t.Errorf("空哈希文件不应计入重复，期望 0，实际 %d", total)
    }
    if len(groups) != 0 {
        t.Errorf("期望空分组列表，实际 %d", len(groups))
    }
}

func TestListDuplicateGroups_MinSize(t *testing.T) {
    db := setupTestDB(t)
    svc := NewService(db, nil, "", "")

    createTestRecord(db, "small1.txt", "/small1.txt", "docs", "hash_small", 50)
    createTestRecord(db, "small2.txt", "/small2.txt", "docs", "hash_small", 50)
    createTestRecord(db, "large1.txt", "/large1.txt", "docs", "hash_large", 1024)
    createTestRecord(db, "large2.txt", "/large2.txt", "docs", "hash_large", 1024)

    // 过滤掉小文件
    groups, total, err := svc.ListDuplicateGroups(DuplicateQuery{MinSize: 100})
    if err != nil {
        t.Fatalf("查询失败: %v", err)
    }
    // total 返回的是系统中所有重复分组的总数（不含 MinSize 过滤）
    if total < 2 {
        t.Errorf("系统中应有 2 个重复分组，实际 %d", total)
    }
    if len(groups) != 1 {
        t.Fatalf("MinSize 过滤后期望 1 个分组，实际 %d", len(groups))
    }
    if groups[0].Xxh3Hash != "hash_large" {
        t.Errorf("期望保留大文件分组，实际得到 %s", groups[0].Xxh3Hash)
    }
}

func TestListDuplicateGroups_Pagination(t *testing.T) {
    db := setupTestDB(t)
    svc := NewService(db, nil, "", "")

    // 创建多组重复（每组的文件数不同）
    createTestRecord(db, "a1.txt", "/a1.txt", "docs", "hash_a", 100)
    createTestRecord(db, "a2.txt", "/a2.txt", "docs", "hash_a", 100)
    createTestRecord(db, "b1.txt", "/b1.txt", "docs", "hash_b", 200)
    createTestRecord(db, "b2.txt", "/b2.txt", "docs", "hash_b", 200)
    createTestRecord(db, "c1.txt", "/c1.txt", "docs", "hash_c", 300)
    createTestRecord(db, "c2.txt", "/c2.txt", "docs", "hash_c", 300)

    // 分页查第一页，每页2条
    groups, total, err := svc.ListDuplicateGroups(DuplicateQuery{Page: 1, PageSize: 2})
    if err != nil {
        t.Fatalf("查询失败: %v", err)
    }
    if total != 3 {
        t.Errorf("期望共 3 个分组，实际 %d", total)
    }
    if len(groups) != 2 {
        t.Errorf("期望第一页 2 个分组，实际 %d", len(groups))
    }
}

func TestListDuplicateGroups_MultipleHashes(t *testing.T) {
    db := setupTestDB(t)
    svc := NewService(db, nil, "", "")

    // 两个不同的哈希值，每组 5 个文件
    for i := 0; i < 5; i++ {
        createTestRecord(db, "x"+fmt.Sprintf("%d",i)+".txt", "/x"+fmt.Sprintf("%d",i)+".txt", "docs", "hash_x", 100)
        createTestRecord(db, "y"+fmt.Sprintf("%d",i)+".txt", "/y"+fmt.Sprintf("%d",i)+".txt", "docs", "hash_y", 200)
    }

    groups, total, err := svc.ListDuplicateGroups(DuplicateQuery{})
    if err != nil {
        t.Fatalf("查询失败: %v", err)
    }
    if total != 2 {
        t.Errorf("期望 2 个分组，实际 %d", total)
    }
    if len(groups) != 2 {
        t.Fatalf("期望 2 组，实际 %d", len(groups))
    }
    for _, g := range groups {
        if g.FileCount != 5 {
            t.Errorf("每组应有 5 个文件，哈希 %s 有 %d 个", g.Xxh3Hash, g.FileCount)
        }
    }
}

// ========== 测试: UpdateNotes ==========

func TestUpdateNotes_Success(t *testing.T) {
    db := setupTestDB(t)
    svc := NewService(db, nil, "", "")

    id := createTestRecord(db, "test.txt", "/test.txt", "docs", "hash", 100)

    notes := "这是一个测试文件备注，使用 **Markdown** 格式。"
    if err := svc.UpdateNotes(id, notes); err != nil {
        t.Fatalf("更新备注失败: %v", err)
    }

    var updated models.FileRecordPublic
    db.First(&updated, id)
    if updated.Notes != notes {
        t.Errorf("备注内容不匹配，期望 %q，实际 %q", notes, updated.Notes)
    }
}

func TestUpdateNotes_EmptyNotes(t *testing.T) {
    db := setupTestDB(t)
    svc := NewService(db, nil, "", "")

    id := createTestRecord(db, "test.txt", "/test.txt", "docs", "hash", 100)

    // 先设置备注
    svc.UpdateNotes(id, "some notes")
    // 再清空备注
    if err := svc.UpdateNotes(id, ""); err != nil {
        t.Fatalf("清空备注失败: %v", err)
    }

    var updated models.FileRecordPublic
    db.First(&updated, id)
    if updated.Notes != "" {
        t.Errorf("备注应为空，实际为 %q", updated.Notes)
    }
}

func TestUpdateNotes_NotFound(t *testing.T) {
    db := setupTestDB(t)
    svc := NewService(db, nil, "", "")

    err := svc.UpdateNotes(99999, "notes")
    if err == nil {
        t.Error("期望返回错误（记录不存在），实际为 nil")
    }
}

func TestUpdateNotes_MultipleRecords(t *testing.T) {
    db := setupTestDB(t)
    svc := NewService(db, nil, "", "")

    id1 := createTestRecord(db, "a.txt", "/a.txt", "docs", "hash_a", 100)
    id2 := createTestRecord(db, "b.txt", "/b.txt", "docs", "hash_b", 200)

    svc.UpdateNotes(id1, "notes for a")
    svc.UpdateNotes(id2, "notes for b")

    var r1, r2 models.FileRecordPublic
    db.First(&r1, id1)
    db.First(&r2, id2)
    if r1.Notes != "notes for a" || r2.Notes != "notes for b" {
        t.Error("多条记录的备注独立更新失败")
    }
}

// ========== 测试: DependencyService ==========

func TestDependencyService_CreateAndList(t *testing.T) {
    db := setupTestDB(t)
    id1 := createTestRecord(db, "source.txt", "/source.txt", "docs", "h1", 100)
    id2 := createTestRecord(db, "target.txt", "/target.txt", "docs", "h2", 200)

    svc := NewDependencyService(db)

    if err := svc.CreateDependency(id1, id2, "requires", "source.txt 需要 target.txt"); err != nil {
        t.Fatalf("创建依赖关系失败: %v", err)
    }

    // 查询上游依赖
    upstream, err := svc.ListUpstreamDependencies(id1)
    if err != nil {
        t.Fatalf("查询上游依赖失败: %v", err)
    }
    if len(upstream) != 1 {
        t.Fatalf("期望 1 个上游依赖，实际有 %d", len(upstream))
    }
    if upstream[0].TargetFile == nil || upstream[0].TargetFile.FileName != "target.txt" {
        t.Error("上游依赖的目标文件不正确")
    }

    // 查询下游依赖
    downstream, err := svc.ListDownstreamDependencies(id2)
    if err != nil {
        t.Fatalf("查询下游依赖失败: %v", err)
    }
    if len(downstream) != 1 {
        t.Fatalf("期望 1 个下游依赖，实际有 %d", len(downstream))
    }
    if downstream[0].SourceFile == nil || downstream[0].SourceFile.FileName != "source.txt" {
        t.Error("下游依赖的源文件不正确")
    }

    // 查询所有依赖
    allUp, allDown, err := svc.ListDependenciesByRecord(id1)
    if err != nil {
        t.Fatalf("查询所有依赖失败: %v", err)
    }
    if len(allUp) != 1 || len(allDown) != 0 {
        t.Errorf("上游期望 1，下游期望 0，实际上游 %d 下游 %d", len(allUp), len(allDown))
    }
}

func TestDependencyService_CircularDependency(t *testing.T) {
    db := setupTestDB(t)
    id1 := createTestRecord(db, "a.txt", "/a.txt", "docs", "h1", 100)
    id2 := createTestRecord(db, "b.txt", "/b.txt", "docs", "h2", 100)

    svc := NewDependencyService(db)

    // a -> b
    if err := svc.CreateDependency(id1, id2, "requires", ""); err != nil {
        t.Fatalf("创建 a->b 失败: %v", err)
    }

    // b -> a (应被拒绝)
    err := svc.CreateDependency(id2, id1, "requires", "")
    if err == nil {
        t.Fatal("期望循环依赖检测错误，实际为 nil")
    }
}

func TestDependencyService_SelfDependency(t *testing.T) {
    db := setupTestDB(t)
    id := createTestRecord(db, "a.txt", "/a.txt", "docs", "h1", 100)

    svc := NewDependencyService(db)
    err := svc.CreateDependency(id, id, "requires", "")
    if err == nil {
        t.Fatal("期望自身依赖检测错误，实际为 nil")
    }
}

func TestDependencyService_DeleteDependency(t *testing.T) {
    db := setupTestDB(t)
    id1 := createTestRecord(db, "a.txt", "/a.txt", "docs", "h1", 100)
    id2 := createTestRecord(db, "b.txt", "/b.txt", "docs", "h2", 100)

    svc := NewDependencyService(db)
    svc.CreateDependency(id1, id2, "requires", "")

    if err := svc.DeleteDependency(1); err != nil {
        t.Fatalf("删除依赖失败: %v", err)
    }

    upstream, _ := svc.ListUpstreamDependencies(id1)
    if len(upstream) != 0 {
        t.Error("删除后上游依赖应为空")
    }
}

func TestDependencyService_DuplicateDependency(t *testing.T) {
    db := setupTestDB(t)
    id1 := createTestRecord(db, "a.txt", "/a.txt", "docs", "h1", 100)
    id2 := createTestRecord(db, "b.txt", "/b.txt", "docs", "h2", 100)

    svc := NewDependencyService(db)

    svc.CreateDependency(id1, id2, "requires", "")
    err := svc.CreateDependency(id1, id2, "requires", "")
    if err == nil {
        t.Fatal("期望重复依赖检测错误，实际为 nil")
    }
}

func TestDependencyService_InvalidRelation(t *testing.T) {
    db := setupTestDB(t)
    id1 := createTestRecord(db, "a.txt", "/a.txt", "docs", "h1", 100)
    id2 := createTestRecord(db, "b.txt", "/b.txt", "docs", "h2", 100)

    svc := NewDependencyService(db)
    err := svc.CreateDependency(id1, id2, "invalid_type", "")
    if err == nil {
        t.Fatal("期望无效关系类型错误，实际为 nil")
    }
}

func TestDependencyService_RecordNotFound(t *testing.T) {
    db := setupTestDB(t)
    id1 := createTestRecord(db, "a.txt", "/a.txt", "docs", "h1", 100)

    svc := NewDependencyService(db)
    err := svc.CreateDependency(id1, 99999, "requires", "")
    if err == nil {
        t.Fatal("期望文件记录不存在错误，实际为 nil")
    }
}

func TestDependencyService_ListDependenciesPagination(t *testing.T) {
    db := setupTestDB(t)
    ids := make([]uint, 3)
    for i := 0; i < 3; i++ {
        ids[i] = createTestRecord(db, "f"+fmt.Sprintf("%d",i)+".txt", "/f"+fmt.Sprintf("%d",i)+".txt", "docs", "h"+fmt.Sprintf("%d",i), 100)
    }

    svc := NewDependencyService(db)
    svc.CreateDependency(ids[0], ids[1], "requires", "")
    svc.CreateDependency(ids[0], ids[2], "related", "")

    deps, total, err := svc.ListDependencies(DependencyListQuery{Page: 1, PageSize: 10})
    if err != nil {
        t.Fatalf("分页查询失败: %v", err)
    }
    if total != 2 {
        t.Errorf("期望 2 条记录，实际 %d", total)
    }
    if len(deps) != 2 {
        t.Errorf("期望返回 2 条记录，实际 %d", len(deps))
    }
}

func TestDependencyService_DeleteNotFound(t *testing.T) {
    db := setupTestDB(t)
    svc := NewDependencyService(db)

    err := svc.DeleteDependency(99999)
    if err == nil {
        t.Fatal("期望依赖不存在错误，实际为 nil")
    }
}

func TestDependencyService_ChainDepthLimit(t *testing.T) {
    db := setupTestDB(t)
    ids := make([]uint, 12)
    for i := 0; i < 12; i++ {
        ids[i] = createTestRecord(db, "f"+fmt.Sprintf("%d",i)+".txt", "/f"+fmt.Sprintf("%d",i)+".txt", "docs", "h"+fmt.Sprintf("%d",i), 100)
    }

    svc := NewDependencyService(db)

    // 创建链: f0 -> f1 -> f2 -> ... -> f10
    for i := 0; i < 11; i++ {
        if err := svc.CreateDependency(ids[i], ids[i+1], "requires", ""); err != nil {
            t.Fatalf("第 %d 个依赖创建失败: %v", i, err)
        }
    }

    // f11 -> f0 尝试创建循环依赖，链太深应该被拒绝
    err := svc.CreateDependency(ids[11], ids[0], "requires", "")
    if err == nil {
        t.Error("期望依赖链过深/循环依赖错误，实际为 nil")
    }
}

// ========== 测试: GetDependencyTree ==========

func TestGetDependencyTree_SimpleTree(t *testing.T) {
    db := setupTestDB(t)
    idA := createTestRecord(db, "A.txt", "A.txt", "docs", "hA", 100)
    idB := createTestRecord(db, "B.txt", "B.txt", "docs", "hB", 200)
    idC := createTestRecord(db, "C.txt", "C.txt", "docs", "hC", 300)

    svc := NewDependencyService(db)
    svc.CreateDependency(idA, idB, "requires", "")
    svc.CreateDependency(idA, idC, "requires", "")

    tree, err := svc.GetDependencyTree(idA)
    if err != nil {
        t.Fatalf("查询依赖树失败: %v", err)
    }

    if tree.ID != idA {
        t.Errorf("根节点 ID 期望 %d, 实际 %d", idA, tree.ID)
    }
    if tree.FileName != "A.txt" {
        t.Errorf("根节点文件名期望 A.txt, 实际 %s", tree.FileName)
    }
    if tree.DownloadURL != "/api/v1/download/docs/A.txt" {
        t.Errorf("根节点下载 URL 不正确: %s", tree.DownloadURL)
    }

    // 上游应有 2 个子节点
    if len(tree.Children) != 2 {
        t.Fatalf("上游依赖期望 2 个, 实际 %d", len(tree.Children))
    }
    // 下游应为空（没有文件依赖 A）
    if len(tree.Downstream) != 0 {
        t.Errorf("下游依赖期望 0 个, 实际 %d", len(tree.Downstream))
    }

    // 验证下游视角（B 的上游是空，下游是 A）
    treeB, _ := svc.GetDependencyTree(idB)
    if len(treeB.Downstream) != 1 {
        t.Errorf("B 的下游期望 1 个 (A), 实际 %d", len(treeB.Downstream))
    }
    if treeB.Downstream[0].FileName != "A.txt" {
        t.Errorf("B 的下游文件名期望 A.txt, 实际 %s", treeB.Downstream[0].FileName)
    }
    if treeB.Downstream[0].DownloadURL != "/api/v1/download/docs/A.txt" {
        t.Errorf("下游节点下载 URL 不正确: %s", treeB.Downstream[0].DownloadURL)
    }
}

func TestGetDependencyTree_MultiLevelUpstream(t *testing.T) {
    db := setupTestDB(t)
    idA := createTestRecord(db, "A.txt", "A.txt", "docs", "hA", 100)
    idB := createTestRecord(db, "B.txt", "B.txt", "docs", "hB", 200)
    idC := createTestRecord(db, "C.txt", "C.txt", "docs", "hC", 300)

    svc := NewDependencyService(db)
    // A -> B -> C
    svc.CreateDependency(idA, idB, "requires", "")
    svc.CreateDependency(idB, idC, "requires", "")

    tree, err := svc.GetDependencyTree(idA)
    if err != nil {
        t.Fatalf("查询依赖树失败: %v", err)
    }

    // A 的上游是 B, B 的上游是 C
    if len(tree.Children) != 1 {
        t.Fatalf("A 的 children 期望 1, 实际 %d", len(tree.Children))
    }
    if tree.Children[0].ID != idB {
        t.Errorf("A 的 child 期望 B, 实际 ID %d", tree.Children[0].ID)
    }
    if len(tree.Children[0].Children) != 1 {
        t.Fatalf("B 的 children 期望 1 (C), 实际 %d", len(tree.Children[0].Children))
    }
    if tree.Children[0].Children[0].ID != idC {
        t.Errorf("B 的 child 期望 C, 实际 ID %d", tree.Children[0].Children[0].ID)
    }
}

func TestGetDependencyTree_CycleDetection(t *testing.T) {
    db := setupTestDB(t)
    idA := createTestRecord(db, "A.txt", "A.txt", "docs", "hA", 100)
    idB := createTestRecord(db, "B.txt", "B.txt", "docs", "hB", 200)

    svc := NewDependencyService(db)
    svc.CreateDependency(idA, idB, "requires", "")

    // 通过手动插入制造循环: B -> A
    db.Create(&models.FileDependency{
        FileRecordID: idB,
        DependsOnID:  idA,
        Relation:     "requires",
    })

    // 查询 A 的树, B -> A 在递归时会检测到循环
    tree, err := svc.GetDependencyTree(idA)
    if err != nil {
        t.Fatalf("有循环依赖时应返回部分树（子节点为空），但返回错误: %v", err)
    }
    // A 的上游是 B (通过 A->B), B 的上游是 A (B->A), 但 A 已在路径中
    if len(tree.Children) != 1 {
        t.Fatalf("A 的 upstream 期望 1 个 (B), 实际 %d", len(tree.Children))
    }
    // B 的 upstream (A) 的 children 因循环检测应为空
    bNode := tree.Children[0]
    if bNode.ID != idB {
        t.Errorf("A 的 child 期望 B, 实际 %d", bNode.ID)
    }
    if len(bNode.Children) != 1 {
        t.Fatalf("B 的 children 期望 1 (A), 实际 %d", len(bNode.Children))
    }
    // A（在 B 下的）的 children 应为空（循环被检测）
    aNodeUnderB := bNode.Children[0]
    if aNodeUnderB.ID != idA {
        t.Errorf("B 的 child 期望 A, 实际 %d", aNodeUnderB.ID)
    }
    if len(aNodeUnderB.Children) != 0 {
        t.Errorf("循环检测后 A 的 children 应为空, 实际 %d", len(aNodeUnderB.Children))
    }
    // 下游: A 的下游应有 B(B -> A)
    if len(tree.Downstream) != 1 {
        t.Errorf("A 的 downstream 期望 1 个(B), 实际 %d", len(tree.Downstream))
    }
}

func TestGetDependencyTree_NoDependencies(t *testing.T) {
    db := setupTestDB(t)
    idA := createTestRecord(db, "A.txt", "A.txt", "docs", "hA", 100)

    svc := NewDependencyService(db)
    tree, err := svc.GetDependencyTree(idA)
    if err != nil {
        t.Fatalf("无依赖时应成功返回: %v", err)
    }
    if len(tree.Children) != 0 {
        t.Errorf("无上游依赖时 children 应为空, 实际 %d", len(tree.Children))
    }
    if len(tree.Downstream) != 0 {
        t.Errorf("无下游依赖时 downstream 应为空, 实际 %d", len(tree.Downstream))
    }
}

func TestGetDependencyTree_FileNotFound(t *testing.T) {
    db := setupTestDB(t)
    svc := NewDependencyService(db)

    _, err := svc.GetDependencyTree(99999)
    if err == nil {
        t.Fatal("文件不存在时应返回错误")
    }
}

func TestGetDependencyTree_DepthLimit(t *testing.T) {
    db := setupTestDB(t)
    ids := make([]uint, 12)
    for i := 0; i < 12; i++ {
        ids[i] = createTestRecord(db, "f"+fmt.Sprintf("%d",i)+".txt", "f"+fmt.Sprintf("%d",i)+".txt", "docs", "h"+fmt.Sprintf("%d",i), 100)
    }

    svc := NewDependencyService(db)

    // 创建链: f0 -> f1 -> f2 -> ... -> f11 (11层)
    for i := 0; i < 11; i++ {
        svc.CreateDependency(ids[i], ids[i+1], "requires", "")
    }

    // 查询 f0 的树, 深度 11 层
    tree, err := svc.GetDependencyTree(ids[0])
    if err != nil {
        t.Fatalf("深度链查询失败: %v", err)
    }

    // 递归检查层数, 最多 10 层
    maxDepth := 0
    var walk func(n *DependencyTreeNode, depth int)
    walk = func(n *DependencyTreeNode, depth int) {
        if depth > maxDepth {
            maxDepth = depth
        }
        for _, child := range n.Children {
            walk(child, depth+1)
        }
    }
    walk(tree, 0)

    // 11 层的链, 根 + 10 层递归 = 最多看到 10 层
    if maxDepth > 10 {
        t.Errorf("深度不应超过 10 层, 实际 %d", maxDepth)
    }
}

