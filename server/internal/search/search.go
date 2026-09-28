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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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
// writer 不支持并发写，因此所有写操作必须经 mu 串行化。
//
// 热替换隔离（rebuild 与查询解耦）：
//   - rwMu 保护 writer 字段的生命周期。读（AutoComplete/SpellCheck/enumSuggestTerms/AllFileIDs）
//     持有 rwMu.RLock（读共享），全量重建完成后的原子切换持有 rwMu.Lock（写独占）。
//   - RebuildFromDB 在影子目录用独立 writer 全量重建，不触碰线上 writer，因此重建全程
//     查询都命中旧索引（完整、一致、零阻塞）；最后一次性原子替换。
type SearchIndex struct {
	writer *bluge.Writer
	dir    string // 本索引的磁盘目录（用于计算影子重建目录）
	seg    *gse.Segmenter
	mu     sync.Mutex   // 串行化写操作
	rwMu   sync.RWMutex // 保护 writer 字段生命周期：读共享 / 热替换写独占

	// 词表缓存：建议接口（SuggestKeywords/SuggestCorrectKeywords）基于去重后的 term 词表工作，
	// 直接每次枚举全词表开销大，这里在写入后节流（debounce）重建一次，后续读复用。
	// 缓存中保存的是"已剔除 N-gram 碎片"的过滤词表，过滤开销（倒排子串扫描）仅在建缓存时执行一次。
	termCacheMu       sync.Mutex // 保护 termCacheTerms 及 lastWriteAt/loaded 的重建
	lastWriteAt       int64      // 最近一次索引写入的 UnixNano 时间，用于节流判定
	termCacheLoaded   bool
	termCacheTerms    []suggestTerm
	suggestDebounceNs int64 // 写后重建的节流窗口（纳秒），0=不节流
}

// OpenIndex 打开（或创建）位于 dir 的搜索索引，并加载 gse 分词词典。
func OpenIndex(dir string) (*SearchIndex, error) {
	// 加载 gse 中文分词器；优先使用内嵌词典，避免依赖外部字典文件
	seg, err := gse.NewEmbed()
	if err != nil {
		return nil, fmt.Errorf("加载 gse 词典失败: %w", err)
	}

	writer, err := openBlugeWriter(dir, &seg)
	if err != nil {
		return nil, err
	}
	return &SearchIndex{
		writer:            writer,
		dir:               dir,
		seg:               &seg,
		suggestDebounceNs: int64(suggestRebuildDebounce), // 写后重建节流窗口
	}, nil
}

// openBlugeWriter 在 dir 打开（或创建）Bluge 写端并套用自定义分析器。
// 影子重建也复用本函数在临时目录构建全新索引。
func openBlugeWriter(dir string, seg *gse.Segmenter) (*bluge.Writer, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建索引目录失败: %w", err)
	}
	config := bluge.DefaultConfig(dir)
	config.DefaultSearchField = FieldFileName
	config.DefaultSearchAnalyzer = newMixedAnalyzer(seg)
	writer, err := bluge.OpenWriter(config)
	if err != nil {
		return nil, fmt.Errorf("打开 Bluge 索引失败: %w", err)
	}
	return writer, nil
}

// buildSearchDoc 以给定分析器构建一条文档（file_name 开启 store）。与 s 无耦合，供线上与影子重建共用。
func buildSearchDoc(id, fileName string, seg *gse.Segmenter) *bluge.Document {
	doc := bluge.NewDocument(id)
	f := bluge.NewTextField(FieldFileName, fileName).
		StoreValue().
		WithAnalyzer(newMixedAnalyzer(seg))
	doc.AddField(f)
	return doc
}

// writeEntries 将一批条目写入指定 writer（不涉及锁与词表缓存，供线上 IndexBatch 与影子重建共用）。
func writeEntries(w *bluge.Writer, seg *gse.Segmenter, entries []IndexEntry) error {
	if len(entries) == 0 {
		return nil
	}
	b := bluge.NewBatch()
	for _, e := range entries {
		if strings.TrimSpace(e.FileName) == "" {
			continue
		}
		id := fmt.Sprintf("%d", e.ID)
		b.Update(bluge.Identifier(id), buildSearchDoc(id, e.FileName, seg))
	}
	if err := w.Batch(b); err != nil {
		return fmt.Errorf("批量写入检索索引失败: %w", err)
	}
	return nil
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
	s.rwMu.RLock()
	defer s.rwMu.RUnlock()

	if s.writer == nil {
		return fmt.Errorf("索引未打开")
	}
	if strings.TrimSpace(fileName) == "" {
		return fmt.Errorf("索引的文件名为空")
	}

	id := fmt.Sprintf("%d", fileID)
	if err := s.writer.Update(bluge.Identifier(id), buildSearchDoc(id, fileName, s.seg)); err != nil {
		return fmt.Errorf("写入索引失败: %w", err)
	}
	s.invalidateTermCache()
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
	s.rwMu.RLock()
	defer s.rwMu.RUnlock()

	if s.writer == nil {
		return fmt.Errorf("索引未打开")
	}
	if err := writeEntries(s.writer, s.seg, entries); err != nil {
		return err
	}
	s.invalidateTermCache()
	return nil
}

// Delete 从索引删除指定 fileID 的文档（文件删除/移动后需同步调用）。
func (s *SearchIndex) Delete(fileID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rwMu.RLock()
	defer s.rwMu.RUnlock()

	if s.writer == nil {
		return nil
	}
	id := bluge.Identifier(fmt.Sprintf("%d", fileID))
	if err := s.writer.Delete(id); err != nil {
		return fmt.Errorf("删除索引失败: %w", err)
	}
	s.invalidateTermCache()
	return nil
}

// DeleteBatch 批量删除多个 fileID 的文档，供对齐/清理场景使用。
func (s *SearchIndex) DeleteBatch(fileIDs []int64) error {
	if len(fileIDs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rwMu.RLock()
	defer s.rwMu.RUnlock()

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
	s.invalidateTermCache()
	return nil
}

// AllFileIDs 枚举当前索引中全部文档的 fileID（用于与索引表对齐，判定缺失/孤儿）。
func (s *SearchIndex) AllFileIDs() ([]int64, error) {
	s.rwMu.RLock()
	defer s.rwMu.RUnlock()
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

// suggestTerm 索引中一个关键词（去重后的 term）及其在全部文档中的出现次数。
type suggestTerm struct {
	term  string
	count uint64
}

// suggestRebuildDebounce 词表缓存写后重建的节流窗口：写入后若尚有后续写入或高频读，
// 暂不重建，等待该窗口内无新写入且用户敲击有间隙时再重建，避免频繁全量枚举。
var suggestRebuildDebounce = 500 * time.Millisecond

// invalidateTermCache 标记词表缓存失效并记录写入时刻。写入索引后调用；
// 实际重建由下次读在节流窗口过后执行。
func (s *SearchIndex) invalidateTermCache() {
	s.lastWriteAt = time.Now().UnixNano()
}

// enumSuggestTerms 返回去重后的建议词表（已剔除 N-gram 碎片，含频率），供建议接口复用。
// 带惰性+节流缓存：首读重建一次；写入后不立即重建，待节流窗口（suggestRebuildDebounce）
// 过后再重建，窗口内的高频读直接复用旧缓存，避免每次键入都枚举全词表。
// termCacheMu 互斥串行化所有读与重建，保证同一时刻仅一个 goroutine 枚举词表。
func (s *SearchIndex) enumSuggestTerms() ([]suggestTerm, error) {
	// RLock 与热替换互斥：重建期间词表缓存不被替换，保证缓存数据与当前 writer 一致。
	s.rwMu.RLock()
	defer s.rwMu.RUnlock()
	if s.writer == nil {
		return nil, nil
	}
	s.termCacheMu.Lock()
	defer s.termCacheMu.Unlock()

	// 缓存未失效且有结果，直接返回
	if s.termCacheLoaded && s.lastWriteAt == 0 {
		return s.termCacheTerms, nil
	}

	// 缓存曾有数据但刚写入（仍在节流窗口内）：复用旧缓存，不打断高频读
	if s.termCacheLoaded && s.writeStillRecent() {
		return s.termCacheTerms, nil
	}

	// 需要重建；CAS 抢到重建权才执行，其余并发调用复用旧缓存
	if s.termCacheLoaded {
		s.lastWriteAt = 0 // 允许后续本次重建被接住后再判定
	}

	raw, err := s.enumSuggestTermsUncached()
	if err != nil {
		return nil, err
	}
	// 倒排子串过滤在此一次性完成，后续读直接复用过滤结果
	s.termCacheTerms = filterTermFragments(raw)
	s.termCacheLoaded = true
	s.lastWriteAt = 0
	return s.termCacheTerms, nil
}

// writeStillRecent 判断最近一次写入是否仍在节流窗口内。（须持有 termCacheMu）
func (s *SearchIndex) writeStillRecent() bool {
	if s.suggestDebounceNs <= 0 || s.lastWriteAt == 0 {
		return false
	}
	return time.Now().UnixNano()-s.lastWriteAt < s.suggestDebounceNs
}

// enumSuggestTermsUncached 实际枚举词表（无缓存）。
func (s *SearchIndex) enumSuggestTermsUncached() ([]suggestTerm, error) {
	reader, err := s.writer.Reader()
	if err != nil {
		return nil, fmt.Errorf("打开读端失败: %w", err)
	}
	defer reader.Close()

	dict, err := reader.DictionaryIterator(FieldFileName, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("打开词表迭代器失败: %w", err)
	}
	defer dict.Close()

	var terms []suggestTerm
	for {
		entry, err := dict.Next()
		if err != nil {
			return nil, fmt.Errorf("遍历词表失败: %w", err)
		}
		if entry == nil {
			break
		}
		terms = append(terms, suggestTerm{term: entry.Term(), count: entry.Count()})
	}
	return terms, nil
}

// filterTermFragments 过滤 N-gram 碎片（英文/数字片段被索引为 2~4 长度的子串）。
// 规则：仅当某 ASCII term 是某"更长 ASCII term"（长度 >= len(term)+2）的连续子串时判定为碎片并丢弃，
// 中文词汇不受影响。完整词（如 readme、mate60pro）因无更长宿主不会被误删。
//
// 倒排化：先用宿主子串建索引，把原先对每个 term 全表扫宿主（O(n²)）降为近线性。
// 覆盖集 covered 只收录"宿主长度 >= 子串长度+2"的子串，与规则语义完全等价，仅保留在
// 确有意义时把碎片判为丢弃的判定项。过滤仍只执行一次（见 enumSuggestTerms 缓存）。
func filterTermFragments(terms []suggestTerm) []suggestTerm {
	// 第一遍：对每个 ASCII 宿主词，把其长度为 2..maxN 的连续子串写入 covered 集合。
	// 宿主为长度 >= len(s)+2 的词，即长度分别不低于 4/5/6（对应碎片长 2/3/4）。
	covered := make(map[string]struct{}, len(terms))
	for _, t := range terms {
		if !isASCIIWord(t.term) || len(t.term) < 4 {
			continue
		}
		word := t.term
		L := len(word)
		maxN := L - 2 // 所需宿主长度 >= 子串长+2
		if maxN > ngramMax {
			maxN = ngramMax
		}
		for n := 2; n <= maxN; n++ {
			for i := 0; i+n <= L; i++ {
				covered[word[i:i+n]] = struct{}{}
			}
		}
	}

	// 第二遍：长度 < ngramMax+1（即 <5）的 ASCII term 若命中覆盖集，即为其更长宿主词的碎片，丢弃。
	out := make([]suggestTerm, 0, len(terms))
	for _, t := range terms {
		if isASCIIWord(t.term) && len(t.term) < ngramMax+1 {
			if _, ok := covered[t.term]; ok {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// SuggestKeywords 自动补全关键词：返回以 prefix 开头的最常见关键词（term），而非完整文件名。
// 基于词表枚举（已剔除 N-gram 碎片），按出现频率排序；返回最多 limit 个。英文为小写，中文为语义词。
func (s *SearchIndex) SuggestKeywords(prefix string, limit int) ([]string, error) {
	terms, err := s.enumSuggestTerms()
	if err != nil {
		return nil, err
	}

	want := strings.ToLower(strings.TrimSpace(prefix))
	var cand []suggestTerm
	for _, t := range terms {
		if strings.HasPrefix(t.term, want) {
			cand = append(cand, t)
		}
	}
	sort.SliceStable(cand, func(i, j int) bool { return cand[i].count > cand[j].count })

	out := make([]string, 0, limit)
	for i := 0; i < len(cand) && len(out) < limit; i++ {
		out = append(out, cand[i].term)
	}
	return out, nil
}

// SuggestCorrectKeywords 拼写纠错关键词：在词表中做编辑距离模糊匹配（fuzziness=2、prefix_length=1），
// 返回最相似的关键词，按（距离升序→频率降序）排序，最多 limit 个。
func (s *SearchIndex) SuggestCorrectKeywords(word string, limit int) ([]string, error) {
	terms, err := s.enumSuggestTerms()
	if err != nil {
		return nil, err
	}
	want := strings.ToLower(strings.TrimSpace(word))
	if want == "" {
		return []string{}, nil
	}

	// 词表已剔除 N-gram 碎片，直接用于距离比较。
	firstChar := want[0]
	type scored struct {
		suggestTerm
		dist int
	}
	var cand []scored
	for _, t := range terms {
		if len(t.term) == 0 || t.term[0] != firstChar {
			continue
		}
		// 编辑距离 <=2 必然要求长度差 <=2，先按长度剪枝，跳过注定不能命中的候选，省去较贵的 DP
		if len(t.term) > len(want)+2 || len(t.term) < len(want)-2 {
			continue
		}
		d := editDistance(want, t.term)
		if d <= 2 {
			cand = append(cand, scored{suggestTerm: t, dist: d})
		}
	}
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
	return out, nil
}

// editDistance 计算两个小写字符串的 Levenshtein 编辑距离（字节级，英文与中文 UTF-8 序列均适用）。
func editDistance(a, b string) int {
	la, lb := len(a), len(b)
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}

// min3 返回三个整数中的最小值
func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// AutoComplete 自动补全：基于前缀查询。
// 返回最多 limit 个匹配的 file_name。前缀为人正常输入的内容，会经分析器处理。
func (s *SearchIndex) AutoComplete(prefix string, limit int) ([]string, error) {
	s.rwMu.RLock()
	defer s.rwMu.RUnlock()
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
	s.rwMu.RLock()
	defer s.rwMu.RUnlock()
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
	s.rwMu.Lock()
	defer s.rwMu.Unlock()

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
