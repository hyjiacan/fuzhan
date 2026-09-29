package search

import (
	"reflect"
	"sort"
	"testing"

	"github.com/blugelabs/bluge/analysis"
)

// analyzeTerms 用给定分析器切分文本，返回 token 词形集合（保序，便于断言）。
func analyzeTerms(a *analysis.Analyzer, text string) []string {
	stream := a.Analyze([]byte(text))
	out := make([]string, 0, len(stream))
	for _, tok := range stream {
		out = append(out, string(tok.Term))
	}
	return out
}

func hasTerm(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestAnalyzerFieldSeparation 验证双字段分析器职责隔离：
// 完整词分析器只输出完整词、不含任何 N-gram 片段；片段分析器在完整词基础上补充 2~4 片段。
func TestAnalyzerFieldSeparation(t *testing.T) {
	idx := newTestIndex(t)
	defer idx.CloseWriter()

	full := newFullWordAnalyzer(idx.seg)
	ngram := newNgramAnalyzer(idx.seg)

	const name = "final textbook book.pdf"

	fullTerms := analyzeTerms(full, name)
	// 完整词字段：中文语义单元 / 英文整词必须保留
	for _, w := range []string{"final", "textbook", "book", "pdf"} {
		if !hasTerm(fullTerms, w) {
			t.Fatalf("完整词字段应含完整词 %q，实际 %v", w, fullTerms)
		}
	}
	// 完整词字段：不得出现任何 N-gram 片段
	for _, frag := range []string{"fina", "inal", "extb", "boo", "ook", "pd"} {
		if hasTerm(fullTerms, frag) {
			t.Fatalf("完整词字段不应含 N-gram 片段 %q，实际 %v", frag, fullTerms)
		}
	}

	ngramTerms := analyzeTerms(ngram, name)
	// 片段字段：完整词仍在
	for _, w := range []string{"final", "textbook", "book", "pdf"} {
		if !hasTerm(ngramTerms, w) {
			t.Fatalf("片段字段应含完整词 %q，实际 %v", w, ngramTerms)
		}
	}
	// 片段字段：补充了 2~4 长度片段
	for _, frag := range []string{"fina", "inal", "extb", "boo", "ook"} {
		if !hasTerm(ngramTerms, frag) {
			t.Fatalf("片段字段应含片段 %q，实际 %v", frag, ngramTerms)
		}
	}
}

// TestSuggestTermTableIsPureFullWords 验证词表枚举只来自完整词字段：
// 合法完整词（含长度 4 且恰为更长词子串的 book）不再被误删，N-gram 碎片（如 final 的 fina）不再出现。
func TestSuggestTermTableIsPureFullWords(t *testing.T) {
	idx := newTestIndex(t)
	defer idx.CloseWriter()
	idx.suggestDebounceNs = 0 // 禁用节流，写入后下次读立即重建

	for id, name := range map[int64]string{
		1: "final report.pdf",
		2: "textbook cover.pdf",
		3: "book list.pdf",
	} {
		if err := idx.IndexFile(id, name); err != nil {
			t.Fatalf("IndexFile 失败: %v", err)
		}
	}

	terms, err := idx.enumSuggestTerms()
	if err != nil {
		t.Fatalf("enumSuggestTerms 失败: %v", err)
	}
	set := make(map[string]struct{}, len(terms))
	for _, x := range terms {
		set[x.term] = struct{}{}
	}

	// 合法完整词必须全部保留（book 历史上会被 textbook 的子串判据误删）
	for _, w := range []string{"final", "textbook", "book"} {
		if _, ok := set[w]; !ok {
			t.Fatalf("词表应含完整词 %q，实际 %v", w, keysOf(terms))
		}
	}
	// N-gram 碎片不得进入词表
	for _, frag := range []string{"fina", "inal", "extb", "boo", "ook"} {
		if _, ok := set[frag]; ok {
			t.Fatalf("词表不应含 N-gram 碎片 %q，实际 %v", frag, keysOf(terms))
		}
	}
}

// TestAutoCompleteMiddleMatchViaNgramField 验证中段/子串召回迁移到片段字段后不退化：
// e60p 是 mate60pro 的 4-gram，属中段匹配，应命中（由片段字段兜住召回）。
func TestAutoCompleteMiddleMatchViaNgramField(t *testing.T) {
	idx := newTestIndex(t)
	defer idx.CloseWriter()

	if err := idx.IndexFile(1, "华为Mate60Pro评测.pdf"); err != nil {
		t.Fatal(err)
	}

	hits, err := idx.AutoComplete("e60p", 10)
	if err != nil {
		t.Fatalf("AutoComplete 失败: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("中段前缀 e60p 应命中片段字段，实际为空")
	}
	if hits[0] != "华为Mate60Pro评测.pdf" {
		t.Fatalf("期望命中 华为Mate60Pro评测.pdf，实际 %v", hits)
	}
}

func keysOf(terms []suggestTerm) []string {
	out := make([]string, len(terms))
	for i, x := range terms {
		out[i] = x.term
	}
	return out
}

// scoredSuggest 用于对照实现的 (term, dist) 候选。
type scoredSuggest struct {
	suggestTerm
	dist int
}

// suggestCorrectFromTerms 复刻 SuggestCorrectKeywords 的排序选择逻辑（含长度剪枝）。
func suggestCorrectFromTerms(word string, limit int, terms []suggestTerm) []string {
	firstChar := word[0]
	var cand []scoredSuggest
	for _, t := range terms {
		if len(t.term) == 0 || t.term[0] != firstChar {
			continue
		}
		if len(t.term) > len(word)+2 || len(t.term) < len(word)-2 {
			continue
		}
		if d := editDistance(word, t.term); d <= 2 {
			cand = append(cand, scoredSuggest{t, d})
		}
	}
	return sortScored(cand, limit)
}

// suggestCorrectFromTermsNoPrune 无长度剪枝的对照实现。
func suggestCorrectFromTermsNoPrune(word string, limit int, terms []suggestTerm) []string {
	firstChar := word[0]
	var cand []scoredSuggest
	for _, t := range terms {
		if len(t.term) == 0 || t.term[0] != firstChar {
			continue
		}
		if d := editDistance(word, t.term); d <= 2 {
			cand = append(cand, scoredSuggest{t, d})
		}
	}
	return sortScored(cand, limit)
}

func sortScored(cand []scoredSuggest, limit int) []string {
	sort.SliceStable(cand, func(i, j int) bool {
		if cand[i].dist != cand[j].dist {
			return cand[i].dist < cand[j].dist
		}
		return cand[i].count > cand[j].count
	})
	out := make([]string, 0, limit)
	for i := 0; i < len(cand) && len(out) < limit; i++ {
		out = append(out, cand[i].term)
	}
	return out
}

// TestSuggestCorrectKeywordsLengthPrune 验证编辑距离的长度剪枝不改变结果。
func TestSuggestCorrectKeywordsLengthPrune(t *testing.T) {
	terms := []suggestTerm{
		{term: "readme", count: 5},
		{term: "read", count: 2},   // 长度差 2，仍可能命中
		{term: "reedme", count: 1}, // 长度差 0
		{term: "x", count: 9},      // 长度差过大，应被剪枝
		{term: "reacts", count: 3}, // 编辑距离 2
	}
	cases := []struct {
		want string
	}{
		{"reedme"}, {"read"}, {"react"},
	}
	for _, c := range cases {
		got := suggestCorrectFromTerms(c.want, 3, terms)
		// 剪枝后结果必须与"对全词表逐一算距离"一致
		want := suggestCorrectFromTermsNoPrune(c.want, 3, terms)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("word=%q 剪枝结果不一致: pruned=%v full=%v", c.want, got, want)
		}
	}
}
