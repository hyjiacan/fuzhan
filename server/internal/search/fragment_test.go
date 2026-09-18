package search

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// oldFilterTermFragments 是优化前的 O(n²) 实现，用于行为等价性对照。
func oldFilterTermFragments(terms []suggestTerm) []suggestTerm {
	out := make([]suggestTerm, 0, len(terms))
	for _, t := range terms {
		if isASCIIWord(t.term) && len(t.term) < 5 && containsInOld(t.term, terms) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func containsInOld(s string, terms []suggestTerm) bool {
	for _, t := range terms {
		if !isASCIIWord(t.term) || len(t.term) <= len(s) {
			continue
		}
		if len(s) > 0 && len(t.term) >= len(s)+2 && containsStr(t.term, s) {
			return true
		}
	}
	return false
}

func containsStr(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestFilterTermFragmentsEquivalent 验证新倒排子串实现与旧 O(n²) 实现在多种词表上行为一致。
func TestFilterTermFragmentsEquivalent(t *testing.T) {
	corpora := [][]suggestTerm{
		// 空 / 单元素
		{},
		{{"readme", 3}},
		// 真实文件名风格
		{{"readme", 5}, {"read", 2}, {"me", 2}, {"md", 3}, {"iso9001", 4}, {"iso", 1}, {"iso9000", 2}},
		// 中文词汇不应被 ASCII 宿主误删
		{{"年度", 7}, {"财务报表", 5}, {"报表", 3}, {"readme", 2}, {"read", 1}},
		// 边界长度 4 的词若是更长词子串则应被删
		{{"book", 4}, {"textbook", 6}, {"bo", 2}, {"ok", 2}},
		// 长度差不足(host=len(s)+1)不应判为碎片
		{{"mars", 9}, {"marsx", 2}, {"ma", 1}},
	}

	// 随机生成一批 ASCII 词组合
	rng := rand.New(rand.NewSource(20260918))
	alphabet := []byte("abcdefghij")
	for i := 0; i < 40; i++ {
		var terms []suggestTerm
		seen := map[string]bool{}
		for _, wd := range randomWords(rng, 2+ (i%6), alphabet) {
			if !seen[wd] {
				seen[wd] = true
				terms = append(terms, suggestTerm{term: wd, count: uint64(1 + rng.Intn(50))})
			}
		}
		corpora = append(corpora, terms)
	}

	for ci, c := range corpora {
		got := filterTermFragments(c)
		want := oldFilterTermFragments(c)
		if !reflect.DeepEqual(keysOf(got), keysOf(want)) {
			t.Fatalf("corpus#%d 行为不一致:\n  old=%v\n  new=%v", ci, keysOf(want), keysOf(got))
		}
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

func randomWords(rng *rand.Rand, n int, alphabet []byte) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		l := 2 + rng.Intn(6) // 2..7
		b := make([]byte, l)
		for j := range b {
			b[j] = alphabet[rng.Intn(len(alphabet))]
		}
		out = append(out, string(b))
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