package search

import (
	"strings"
	"testing"
	"time"
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

// TestTermCacheInvalidation 验证词表缓存：未写入时复用缓存，写入后重建反映新词表。
// 测试将节流窗口置 0，使写入后下一次读立即重建。
func TestTermCacheInvalidation(t *testing.T) {
	idx := newTestIndex(t)
	defer idx.CloseWriter()
	idx.suggestDebounceNs = 0 // 禁用节流，验证写入后下次读立即重建

	if terms, _ := idx.enumSuggestTermsUncached(); len(terms) != 0 {
		t.Fatalf("空索引词表应为空，实际 %v", terms)
	}
	_ = idx.IndexFile(1, "README.md")

	// 首次读取触发缓存加载
	first, err := idx.enumSuggestTerms()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 {
		t.Fatal("写入后词表不应为空")
	}
	if !idx.termCacheLoaded || idx.lastWriteAt != 0 {
		t.Fatalf("首次读取后应缓存已加载且无挂起写入：loaded=%v lastWriteAt=%d", idx.termCacheLoaded, idx.lastWriteAt)
	}

	// 未写入时再读，应命中缓存（不触发枚举重建）
	second, err := idx.enumSuggestTerms()
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != len(first) {
		t.Fatalf("未写入时缓存应对等，首次 %d 二次 %d", len(first), len(second))
	}

	// 写入后标记挂起重建，下一次读重建并反映新词表
	_ = idx.IndexFile(2, "年度财务报表.pdf")
	if idx.lastWriteAt == 0 {
		t.Fatal("写入后 lastWriteAt 应非零（标记挂起重建）")
	}
	third, err := idx.enumSuggestTerms()
	if err != nil {
		t.Fatal(err)
	}
	if idx.lastWriteAt != 0 {
		t.Fatal("重建后 lastWriteAt 应清零")
	}
	if len(third) <= len(first) {
		t.Fatalf("重建后词表应包含新增 term，首次 %d 重建后 %d", len(first), len(third))
	}
}

// TestTermCacheDebounce 验证节流：节点窗口内的读复用旧缓存不重建，窗口过后再读才重建。
func TestTermCacheDebounce(t *testing.T) {
	idx := newTestIndex(t)
	defer idx.CloseWriter()
	idx.suggestDebounceNs = int64(30 * time.Millisecond)

	_ = idx.IndexFile(1, "README.md")
	first, err := idx.enumSuggestTerms() // 首次重建
	if err != nil {
		t.Fatal(err)
	}

	// 写入另一文件，紧接读应落在节流窗口内 → 复用旧缓存（不含新词）
	_ = idx.IndexFile(2, "年度财务报表.pdf")
	stale, err := idx.enumSuggestTerms()
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != len(first) {
		t.Fatalf("节流窗口内的读应复用旧缓存：首 %d 窗口内 %d", len(first), len(stale))
	}

	// 等待窗口过后再读 → 重建并包含新词
	time.Sleep(50 * time.Millisecond)
	fresh, err := idx.enumSuggestTerms()
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh) <= len(first) {
		t.Fatalf("窗口过后应重建包含新词：首 %d 之后 %d", len(first), len(fresh))
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

// TestSuggestKeywords 覆盖关键词补全：返回关键词（term）而非完整文件名
func TestSuggestKeywords(t *testing.T) {
	idx := newTestIndex(t)
	defer idx.CloseWriter()
	files := map[int64]string{
		1: "README.md",
		2: "年度财务报表2024.pdf",
		3: "华为Mate60Pro评测.pdf",
		4: "readme-zh_CN.md",
		5: "ISO9001质量体系文档.docx",
	}
	for id, name := range files {
		if err := idx.IndexFile(id, name); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		prefix string
		want   string
	}{
		{"read", "readme"},    // 英文整词（README.md 的中锋词）
		{"Mate", "mate60pro"}, // 数字混排整词
		{"年度", "年度"},          // 中文语义词
		{"财务", "财务报表"},        // 前缀命中完整语义词
		{"ISO9001", "iso9001"},
		{"不存在的词", ""}, // 无命中应返回空
	}
	for _, c := range cases {
		got, err := idx.SuggestKeywords(c.prefix, 10)
		if err != nil {
			t.Fatalf("SuggestKeywords(%q) 失败: %v", c.prefix, err)
		}
		if c.want == "" {
			if len(got) != 0 {
				t.Fatalf("SuggestKeywords(%q) 期望无结果，实际 %v", c.prefix, got)
			}
			continue
		}
		if len(got) == 0 {
			t.Fatalf("SuggestKeywords(%q) 期望含 %s，实际空", c.prefix, c.want)
			continue
		}
		if got[0] != c.want {
			t.Fatalf("SuggestKeywords(%q) 期望 %s，实际 %v", c.prefix, c.want, got)
		}
	}
}

// TestSuggestCorrectKeywords 覆盖关键词纠错：返回编辑距离内的关键词候选
func TestSuggestCorrectKeywords(t *testing.T) {
	idx := newTestIndex(t)
	defer idx.CloseWriter()
	if err := idx.IndexFile(1, "README.md"); err != nil {
		t.Fatal(err)
	}
	if err := idx.IndexFile(2, "ISO9001质量体系文档.docx"); err != nil {
		t.Fatal(err)
	}

	// REEDME -> readme
	hits, err := idx.SuggestCorrectKeywords("REEDME", 5)
	if err != nil {
		t.Fatalf("SuggestCorrectKeywords 失败: %v", err)
	}
	if len(hits) == 0 || hits[0] != "readme" {
		t.Fatalf("REEDME 纠错应首选关键词 readme，实际 %v", hits)
	}

	// ISO9000 -> iso9001
	hits, err = idx.SuggestCorrectKeywords("ISO9000", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0] != "iso9001" {
		t.Fatalf("ISO9000 纠错应首选 iso9001，实际 %v", hits)
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
