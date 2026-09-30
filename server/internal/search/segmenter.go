package search

// 本文件集中承载 gse 中文分词的全部使用（职责分离）：
//   - 加载 gse 分词器（newSegmenter）
//   - 面向 Bluge 的分析器与自定义分词器（newFullWordAnalyzer / newNgramAnalyzer / gseNgramTokenizer）
//   - 切词辅助（fullWords / isASCIIWord）
//
// 其余文件只通过本地别名 segmenter 与本文件暴露的函数使用分词能力，
// 从而把对 github.com/go-ego/gse 的依赖收敛在此一处。

import (
	"fmt"
	"strings"

	"github.com/blugelabs/bluge/analysis"
	"github.com/blugelabs/bluge/analysis/token"
	"github.com/go-ego/gse"
)

const (
	// Ngram 英文/数字片段长度范围
	ngramMin = 2
	ngramMax = 4
)

// segmenter 是 gse 分词器的本地别名，作为包内统一的分词器类型。
type segmenter = gse.Segmenter

// newSegmenter 加载 gse 中文分词器；优先使用内嵌词典，避免依赖外部字典文件。
func newSegmenter() (*segmenter, error) {
	seg, err := gse.NewEmbed()
	if err != nil {
		return nil, fmt.Errorf("加载 gse 词典失败: %w", err)
	}
	return &seg, nil
}

// newFullWordAnalyzer 构造「完整词」分析器：gse 中文分词 + 英文/数字整词，不补 N-gram 片段。
// 供 file_name 字段使用，是建议/纠错词表的唯一来源。
func newFullWordAnalyzer(seg *segmenter) *analysis.Analyzer {
	return &analysis.Analyzer{
		Tokenizer: &gseNgramTokenizer{
			seg:         seg,
			ngramMin:    ngramMin,
			ngramMax:    ngramMax,
			enableNgram: false,
		},
		TokenFilters: []analysis.TokenFilter{
			token.NewLowerCaseFilter(),
		},
	}
}

// newNgramAnalyzer 构造「片段」分析器：完整词 + N-gram(2~4) 片段。
// 供 file_name_ngram 字段使用，只服务需要子串/中段召回的查询。
func newNgramAnalyzer(seg *segmenter) *analysis.Analyzer {
	return &analysis.Analyzer{
		Tokenizer: &gseNgramTokenizer{
			seg:         seg,
			ngramMin:    ngramMin,
			ngramMax:    ngramMax,
			enableNgram: true,
		},
		TokenFilters: []analysis.TokenFilter{
			token.NewLowerCaseFilter(),
		},
	}
}

// gseNgramTokenizer 自定义分词器：先 gse 切词，完整词始终保留，再按开关决定是否对英文/数字段补 N-gram。
type gseNgramTokenizer struct {
	seg         *segmenter
	ngramMin    int
	ngramMax    int
	enableNgram bool
}

// Tokenize 将输入文本转换为 token 流。
func (t *gseNgramTokenizer) Tokenize(input []byte) analysis.TokenStream {
	text := strings.ToLower(string(input))
	words := t.seg.Cut(text, true)

	var stream analysis.TokenStream
	pos := 0
	// 借助 map 去重，避免同一位置产生重复 token
	emitted := make(map[string]struct{})

	emit := func(term string) {
		if _, ok := emitted[term]; ok {
			return
		}
		emitted[term] = struct{}{}
		pos++
		stream = append(stream, &analysis.Token{
			Start:        0,
			End:          len(term),
			Term:         []byte(term),
			PositionIncr: 1,
			Type:         analysis.AlphaNumeric,
		})
	}

	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if isASCIIWord(w) && len(w) >= 2 {
			// 英文/数字：完整词始终保留（供精确前缀/模糊匹配），仅按开关补充 N-gram 片段
			emit(w)
			if t.enableNgram {
				for n := t.ngramMin; n <= t.ngramMax && n <= len(w); n++ {
					for i := 0; i+n <= len(w); i++ {
						emit(w[i : i+n])
					}
				}
			}
		} else {
			// 中文：gse 已切成语义单元，直接作为 token
			emit(w)
		}
	}
	return stream
}

// isASCIIWord 判断字符串是否仅由 ASCII 字母/数字组成
func isASCIIWord(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// fullWords 将文件名切分为完整词（中文语义单元 / 英文数字整词），不含 N-gram 片段。
// 与索引写入共用同一分词器，保证词形与倒排一致。
func fullWords(seg *segmenter, fileName string) []string {
	words := seg.Cut(strings.ToLower(fileName), true)
	out := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		out = append(out, w)
	}
	return out
}
