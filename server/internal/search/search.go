// Package search 提供基于 Bluge + gse 的轻量级嵌入式文件名检索模块。
//
// 设计目标：不引入外部搜索服务，单进程内即可完成文件名的
//   - 自动补全（前缀匹配，供用户输入时实时联想）
//   - 拼写纠错（模糊匹配，识别用户拼错的文件名并给出建议）
//
// 分词策略（面向中英文混排文件名）：
//   - 中文：使用 gse 分词，切分成语义单元（如"年度财务报表" → 年度/财务/报表）
//   - 英文/数字：使用 N-gram（长度 2~4）生成片段，支持只记得部分名称的检索
//   - 统一小写化：README / readme / Readme 等价
package search

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/blugelabs/bluge"
	"github.com/blugelabs/bluge/analysis"
	"github.com/blugelabs/bluge/analysis/token"
	"github.com/blugelabs/bluge/search"
	"github.com/go-ego/gse"
)

const (
	// FieldFileName 文件名索引字段名
	FieldFileName = "file_name"

	// Ngram 英文/数字片段长度范围
	ngramMin = 2
	ngramMax = 4
)

// SearchIndex 封装 Bluge 索引 Writer，负责文件名的写入与检索。
// writer 不支持并发写，因此所有写操作必须经 mutex 串行化。
type SearchIndex struct {
	writer *bluge.Writer
	seg    *gse.Segmenter
	mu     sync.Mutex // 串行化写操作
}

// OpenIndex 打开（或创建）位于 dir 的搜索索引，并加载 gse 分词词典。
func OpenIndex(dir string) (*SearchIndex, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建索引目录失败: %w", err)
	}

	// 加载 gse 中文分词器；优先使用内嵌词典，避免依赖外部字典文件
	seg, err := gse.NewEmbed()
	if err != nil {
		return nil, fmt.Errorf("加载 gse 词典失败: %w", err)
	}

	config := bluge.DefaultConfig(dir)
	config.DefaultSearchField = FieldFileName
	config.DefaultSearchAnalyzer = newMixedAnalyzer(&seg)

	writer, err := bluge.OpenWriter(config)
	if err != nil {
		return nil, fmt.Errorf("打开 Bluge 索引失败: %w", err)
	}
	return &SearchIndex{writer: writer, seg: &seg}, nil
}

// newMixedAnalyzer 构造组合分析器：
//   - gse 处理中文分词
//   - N-gram(2~4) 处理英文/数字片段
//   - LowerCase 过滤器统一小写
func newMixedAnalyzer(seg *gse.Segmenter) *analysis.Analyzer {
	return &analysis.Analyzer{
		Tokenizer: &gseNgramTokenizer{
			seg:      seg,
			ngramMin: ngramMin,
			ngramMax: ngramMax,
		},
		TokenFilters: []analysis.TokenFilter{
			token.NewLowerCaseFilter(),
		},
	}
}

// gseNgramTokenizer 自定义分词器：先 gse 切词，再对英文/数字段补 N-gram。
type gseNgramTokenizer struct {
	seg      *gse.Segmenter
	ngramMin int
	ngramMax int
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
			// 英文/数字：保留完整词（供精确前缀/模糊匹配），并补充 N-gram 片段
			emit(w)
			for n := t.ngramMin; n <= t.ngramMax && n <= len(w); n++ {
				for i := 0; i+n <= len(w); i++ {
					emit(w[i : i+n])
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

// IndexFile 将一条文件名写入索引。
// fileID 作为文档唯一标识，同名文件再次调用即更新（覆盖旧数据）。
// file_name 字段调用 StoreValue，查询时可回读文件名。
func (s *SearchIndex) IndexFile(fileID int64, fileName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writer == nil {
		return fmt.Errorf("索引未打开")
	}
	if strings.TrimSpace(fileName) == "" {
		return fmt.Errorf("索引的文件名为空")
	}

	id := fmt.Sprintf("%d", fileID)
	if err := s.writer.Update(bluge.Identifier(id), s.buildDoc(id, fileName)); err != nil {
		return fmt.Errorf("写入索引失败: %w", err)
	}
	return nil
}

// IndexEntry 批量索引的一条记录
type IndexEntry struct {
	ID       int64
	FileName string
}

// IndexBatch 批量写入索引（一次性多个文档），适合全量构建时的批量导入。
func (s *SearchIndex) IndexBatch(entries []IndexEntry) error {
	if len(entries) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writer == nil {
		return fmt.Errorf("索引未打开")
	}
	b := bluge.NewBatch()
	for _, e := range entries {
		if strings.TrimSpace(e.FileName) == "" {
			continue
		}
		id := fmt.Sprintf("%d", e.ID)
		b.Update(bluge.Identifier(id), s.buildDoc(id, e.FileName))
	}
	if err := s.writer.Batch(b); err != nil {
		return fmt.Errorf("批量写入索引失败: %w", err)
	}
	return nil
}

// buildDoc 构建文档：file_name 字段开启 store，供检索后直接返回；索引端使用自定义分析器
func (s *SearchIndex) buildDoc(id, fileName string) *bluge.Document {
	doc := bluge.NewDocument(id)
	f := bluge.NewTextField(FieldFileName, fileName).
		StoreValue().
		WithAnalyzer(newMixedAnalyzer(s.seg))
	doc.AddField(f)
	return doc
}

// Delete 从索引删除指定 fileID 的文档（文件删除/移动后需同步调用）。
func (s *SearchIndex) Delete(fileID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writer == nil {
		return nil
	}
	id := bluge.Identifier(fmt.Sprintf("%d", fileID))
	if err := s.writer.Delete(id); err != nil {
		return fmt.Errorf("删除索引失败: %w", err)
	}
	return nil
}

// DeleteBatch 批量删除多个 fileID 的文档，供对齐/清理场景使用。
func (s *SearchIndex) DeleteBatch(fileIDs []int64) error {
	if len(fileIDs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writer == nil {
		return nil
	}
	b := bluge.NewBatch()
	for _, id := range fileIDs {
		b.Delete(bluge.Identifier(fmt.Sprintf("%d", id)))
	}
	if err := s.writer.Batch(b); err != nil {
		return fmt.Errorf("批量删除索引失败: %w", err)
	}
	return nil
}

// AllFileIDs 枚举当前索引中全部文档的 fileID（用于与索引表对齐，判定缺失/孤儿）。
func (s *SearchIndex) AllFileIDs() ([]int64, error) {
	if s.writer == nil {
		return nil, nil
	}
	reader, err := s.writer.Reader()
	if err != nil {
		return nil, fmt.Errorf("打开读端失败: %w", err)
	}
	defer reader.Close()

	// 用 MatchAll 查询命中全部文档，再回读存储的 _id 字段
	req := bluge.NewTopNSearch(1<<20, bluge.NewMatchAllQuery())
	dmi, err := reader.Search(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("遍历索引文档失败: %w", err)
	}

	var ids []int64
	for {
		match, err := dmi.Next()
		if err != nil {
			return nil, fmt.Errorf("遍历索引 ID 失败: %w", err)
		}
		if match == nil {
			break
		}
		_ = match.VisitStoredFields(func(fieldName string, value []byte) bool {
			if fieldName == "_id" {
				if id, perr := strconv.ParseInt(string(value), 10, 64); perr == nil {
					ids = append(ids, id)
				}
				return false
			}
			return true
		})
	}
	return ids, nil
}

// AutoComplete 自动补全：基于前缀查询。
// 返回最多 limit 个匹配的 file_name。前缀为人正常输入的内容，会经分析器处理。
func (s *SearchIndex) AutoComplete(prefix string, limit int) ([]string, error) {
	if s.writer == nil {
		return nil, nil
	}
	reader, err := s.writer.Reader()
	if err != nil {
		return nil, fmt.Errorf("打开读端失败: %w", err)
	}
	defer reader.Close()

	q := bluge.NewPrefixQuery(strings.ToLower(strings.TrimSpace(prefix))).
		SetField(FieldFileName)
	req := bluge.NewTopNSearch(limit, q)

	dmi, err := reader.Search(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("前缀查询失败: %w", err)
	}
	return collectFileNames(dmi)
}

// SpellCheck 拼写纠错：基于模糊查询。
// 设置 fuzziness=2、prefix_length=1（首字符必须精确，减少误纠音近词），
// 返回最多 limit 个最相似的文件名候选。
func (s *SearchIndex) SpellCheck(word string, limit int) ([]string, error) {
	if s.writer == nil {
		return nil, nil
	}
	reader, err := s.writer.Reader()
	if err != nil {
		return nil, fmt.Errorf("打开读端失败: %w", err)
	}
	defer reader.Close()

	q := bluge.NewFuzzyQuery(strings.ToLower(strings.TrimSpace(word))).
		SetFuzziness(2). // 编辑距离容差
		SetPrefix(1).    // 固定首字符，避免离谱建议
		SetField(FieldFileName)
	req := bluge.NewTopNSearch(limit, q)

	dmi, err := reader.Search(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("模糊查询失败: %w", err)
	}
	return collectFileNames(dmi)
}

// CloseWriter 优雅关闭，确保内存索引数据安全刷盘后关闭句柄。
// 程序退出前必须调用，否则可能丢失最近写入的数据。
func (s *SearchIndex) CloseWriter() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.writer == nil {
		return nil
	}
	err := s.writer.Close()
	s.writer = nil
	return err
}

// collectFileNames 遍历迭代器，回读每条命中的 file_name 存储值。
func collectFileNames(dmi search.DocumentMatchIterator) ([]string, error) {
	result := make([]string, 0, 10)
	var visitErr error
	for {
		match, err := dmi.Next()
		if err != nil {
			return result, fmt.Errorf("遍历查询结果失败: %w", err)
		}
		if match == nil {
			break
		}
		// 命中即回读存储的文件名
		_ = match.VisitStoredFields(func(fieldName string, value []byte) bool {
			if fieldName == FieldFileName {
				result = append(result, string(value))
				return false // 已取到文件名，停止回调
			}
			return true
		})
		if visitErr != nil {
			return result, visitErr
		}
	}
	return result, nil
}
