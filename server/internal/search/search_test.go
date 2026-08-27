package search

import (
    "strings"
    "testing"
)

func newTestIndex(t *testing.T) *SearchIndex {
    t.Helper()
    idx, err := OpenIndex(t.TempDir())
    if err != nil {
        t.Fatalf("OpenIndex 失败: %v", err)
    }
    return idx
}

func TestIndexFileAndClose(t *testing.T) {
    idx := newTestIndex(t)
    if err := idx.IndexFile(1, "README.md"); err != nil {
        t.Fatalf("IndexFile 失败: %v", err)
    }
    if err := idx.IndexFile(2, "年度财务报表.pdf"); err != nil {
        t.Fatalf("IndexFile 失败: %v", err)
    }
    if err := idx.CloseWriter(); err != nil {
        t.Fatalf("CloseWriter 失败: %v", err)
    }
    // 关闭后写入应报错（writer 置空）
    if err := idx.IndexFile(3, "x.txt"); err == nil {
        t.Fatal("关闭后写入应报错")
    }
}

func TestIndexFileUpdateOverwrite(t *testing.T) {
    idx := newTestIndex(t)
    defer idx.CloseWriter()

    // 同一 fileID 两次写入，应覆盖旧文件名
    if err := idx.IndexFile(7, "旧名称.txt"); err != nil {
        t.Fatal(err)
    }
    if err := idx.IndexFile(7, "新名称.txt"); err != nil {
        t.Fatal(err)
    }

    hits, err := idx.AutoComplete("旧名称", 5)
    if err != nil {
        t.Fatal(err)
    }
    if len(hits) != 0 {
        t.Fatalf("旧文件名应被覆盖，实际返回 %v", hits)
    }
}

func TestAutoComplete(t *testing.T) {
    idx := newTestIndex(t)
    defer idx.CloseWriter()

    files := map[int64]string{
        1: "README.md",
        2: "年度财务报表2024.pdf",
        3: "华为Mate60Pro评测.pdf",
    }
    for id, name := range files {
        if err := idx.IndexFile(id, name); err != nil {
            t.Fatal(err)
        }
    }

    cases := []struct {
        prefix string
        want   []string
    }{
        // 英文：大小写不敏感前缀
        {"read", []string{"README.md"}},
        {"README", []string{"README.md"}},
        // 英文/数字混排片段
        {"Mate", []string{"华为Mate60Pro评测.pdf"}},
        // 中文：按语义词前缀
        {"年度", []string{"年度财务报表2024.pdf"}},
        {"财务", []string{"年度财务报表2024.pdf"}},
        {"不存在的前缀", nil},
    }

    for _, c := range cases {
        hits, err := idx.AutoComplete(c.prefix, 10)
        if err != nil {
            t.Fatalf("AutoComplete(%q) 失败: %v", c.prefix, err)
        }
        if c.want == nil {
            if len(hits) != 0 {
                t.Fatalf("AutoComplete(%q) 期望无结果，实际 %v", c.prefix, hits)
            }
            continue
        }
        if len(hits) != len(c.want) || hits[0] != c.want[0] {
            t.Fatalf("AutoComplete(%q) 期望 %v，实际 %v", c.prefix, c.want, hits)
        }
    }
}

func TestSpellCheck(t *testing.T) {
    idx := newTestIndex(t)
    defer idx.CloseWriter()

    if err := idx.IndexFile(1, "README.md"); err != nil {
        t.Fatal(err)
    }
    if err := idx.IndexFile(2, "README_EN.md"); err != nil {
        t.Fatal(err)
    }
    if err := idx.IndexFile(3, "ISO9001质量体系文档.docx"); err != nil {
        t.Fatal(err)
    }

    // REEDME 拼错 -> 应建议 README.md / README_EN.md
    hits, err := idx.SpellCheck("REEDME", 5)
    if err != nil {
        t.Fatalf("SpellCheck 失败: %v", err)
    }
    if len(hits) == 0 {
        t.Fatal("REEDME 应得到纠错建议")
    }
    joined := strings.Join(hits, "|")
    if !strings.Contains(joined, "README.md") {
        t.Fatalf("纠错建议应包含 README.md，实际 %v", hits)
    }

    // ISO9000 近似 -> 应建议 ISO9001
    hits, err = idx.SpellCheck("ISO9000", 5)
    if err != nil {
        t.Fatal(err)
    }
    if len(hits) == 0 {
        t.Fatal("ISO9000 应得到纠错建议")
    }
    if !strings.Contains(strings.Join(hits, "|"), "ISO9001") {
        t.Fatalf("纠错建议应包含 ISO9001，实际 %v", hits)
    }
}

// TestIndexBatchAndAutoComplete 覆盖全量构建路径（IndexBatch + 真实风格文件名）
func TestIndexBatchAndAutoComplete(t *testing.T) {
    idx := newTestIndex(t)
    defer idx.CloseWriter()

    entries := []IndexEntry{
        {1, "Firefox Setup 108.0.2.msi"},
        {2, "2026年度全国保密教育线上培训证书.png"},
        {3, "MobaXterm_Installer_v26.4.zip"},
        {4, "Cherry-Studio-1.9.12-x64-setup.exe"},
    }
    if err := idx.IndexBatch(entries); err != nil {
        t.Fatalf("IndexBatch 失败: %v", err)
    }

    cases := []struct {
        prefix string
        want   string
    }{
        {"fire", "Firefox Setup 108.0.2.msi"},
        {"moba", "MobaXterm_Installer_v26.4.zip"},
        {"cherry", "Cherry-Studio-1.9.12-x64-setup.exe"},
        {"年度", "2026年度全国保密教育线上培训证书.png"},
    }
    for _, c := range cases {
        hits, err := idx.AutoComplete(c.prefix, 10)
        if err != nil {
            t.Fatalf("AutoComplete(%q) 失败: %v", c.prefix, err)
        }
        if len(hits) == 0 {
            t.Fatalf("AutoComplete(%q) 期望命中 %s，实际空", c.prefix, c.want)
            continue
        }
        if hits[0] != c.want {
            t.Fatalf("AutoComplete(%q) 期望 %s，实际 %v", c.prefix, c.want, hits)
        }
    }
}