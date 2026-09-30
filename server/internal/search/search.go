// Package search 提供基于 Bluge + gse 的轻量级嵌入式文件名检索模块。
//
// 设计目标：不引入外部搜索服务，单进程内即可完成文件名的
//   - 自动补全（前缀匹配，供用户输入时实时联想）
//   - 拼写纠错（模糊匹配，识别用户拼错的文件名并给出建议）
//
// 分词策略（面向中英文混排文件名，采用双字段隔离）：
//   - 完整词字段 file_name：中文语义单元（如"年度财务报表" → 年度/财务/报表）与英文/数字整词，
//     开启 store 供回读文件名；它是建议与纠错词表的唯一枚举来源。
//   - 片段字段 file_name_ngram：在完整词基础上再补英文/数字的 N-gram（长度 2~4）片段，
//     仅索引不 store，只服务需要子串/中段召回的查询。
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
	"unicode/utf8"

	"github.com/blugelabs/bluge"
	"github.com/blugelabs/bluge/search"
)

const (
	// FieldFileName 完整词字段：中文语义单元 / 英文整词；开启 store，供回读文件名
	FieldFileName = "file_name"
	// FieldFileNameNgram 片段字段：完整词 + N-gram(2~4)；仅索引不 store，供子串召回
	FieldFileNameNgram = "file_name_ngram"
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
	seg    *segmenter
	mu     sync.Mutex   // 串行化写操作
	rwMu   sync.RWMutex // 保护 writer 字段生命周期：读共享 / 热替换写独占

	// 词表缓存：建议接口（SuggestKeywords/SuggestCorrectKeywords）基于去重后的 term 词表工作，
	// 直接每次枚举全词表开销大，这里在写入后节流（debounce）重建一次，后续读复用。
	// 词表只从完整词字段 FieldFileName 枚举，天然不含 N-gram 碎片，无需再做子串清洗。
	termCacheMu       sync.Mutex // 保护 termCacheTerms 及 lastWriteAt/loaded 的重建
	lastWriteAt       int64      // 最近一次索引写入的 UnixNano 时间，用于节流判定
	termCacheLoaded   bool
	termCacheTerms    []suggestTerm
	suggestDebounceNs int64 // 写后重建的节流窗口（纳秒），0=不节流
}

// OpenIndex 打开（或创建）位于 dir 的搜索索引，并加载 gse 分词词典。
// 分词器的加载与使用见 segmenter.go，本文件不直接依赖 gse。
func OpenIndex(dir string) (*SearchIndex, error) {
	seg, err := newSegmenter()
	if err != nil {
		return nil, err
	}

	writer, err := openBlugeWriter(dir, seg)
	if err != nil {
		return nil, err
	}
	return &SearchIndex{
		writer:            writer,
		dir:               dir,
		seg:               seg,
		suggestDebounceNs: int64(suggestRebuildDebounce), // 写后重建节流窗口
	}, nil
}

// openBlugeWriter 在 dir 打开（或创建）Bluge 写端并套用自定义分析器。
// 影子重建也复用本函数在临时目录构建全新索引。
func openBlugeWriter(dir string, seg *segmenter) (*bluge.Writer, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建索引目录失败: %w", err)
	}
	config := bluge.DefaultConfig(dir)
	config.DefaultSearchField = FieldFileName
	config.DefaultSearchAnalyzer = newFullWordAnalyzer(seg)
	writer, err := bluge.OpenWriter(config)
	if err != nil {
		return nil, fmt.Errorf("打开 Bluge 索引失败: %w", err)
	}
	return writer, nil
}

// buildSearchDoc 以两个分析器分别构建同一文件名的两个字段：
//   - file_name（完整词）：开启 store，供词表枚举与命中后回读文件名
//   - file_name_ngram（完整词 + N-gram 片段）：不 store，供前缀/模糊等子串召回
//
// 与 s 无耦合，供线上与影子重建共用。
func buildSearchDoc(id, fileName string, seg *segmenter) *bluge.Document {
	doc := bluge.NewDocument(id)
	doc.AddField(bluge.NewTextField(FieldFileName, fileName).
		StoreValue().
		WithAnalyzer(newFullWordAnalyzer(seg)))
	doc.AddField(bluge.NewTextField(FieldFileNameNgram, fileName).
		WithAnalyzer(newNgramAnalyzer(seg)))
	return doc
}

// writeEntries 将一批条目写入指定 writer（不涉及锁与词表缓存，供线上 IndexBatch 与影子重建共用）。
func writeEntries(w *bluge.Writer, seg *segmenter, entries []IndexEntry) error {
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

// IndexFile 将一条文件名写入索引。
// fileID 作为文档唯一标识，同名文件再次调用即更新（覆盖旧数据）。
// 文档写入完整词字段（store，供回读文件名）与片段字段（不 store，供子串召回）两个字段。
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

// enumSuggestTerms 返回去重后的建议词表（含频率），供建议接口复用。
// 词表只从完整词字段 FieldFileName 枚举，天然不含 N-gram 碎片，无需再做子串清洗。
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
	// 词表来自完整词字段，已是纯完整词，直接作为缓存词表
	s.termCacheTerms = raw
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
// 只枚举完整词字段 FieldFileName：该字段不含 N-gram 片段，枚举结果即纯完整词词表。
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

// SuggestKeywords 自动补全关键词：返回以 prefix 开头的最常见关键词（term），而非完整文件名。
// 基于词表枚举（只含完整词，无 N-gram 碎片），按出现频率排序；返回最多 limit 个。英文为小写，中文为语义词。
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

	// 词表只含完整词，直接用于距离比较，无需再排除 N-gram 噪声。
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

// relatedDocsLimit 相关词推荐参与共现统计的命中文档上限，用于约束单次请求的分词与聚合开销。
const relatedDocsLimit = 200

// SuggestRelatedKeywords 相关词推荐：统计与已输入各段（空格分隔）同文件共现的其它关键词，
// 按共现文档数降序返回，供"输入空格后"给出下一段候选。只做共现、不要求词序相邻。
// 关键词取自命中文件名的完整词（中文语义单元 / 英文整词），不含 N-gram 碎片；
// 已输入段本身及其补全（如输入"年度"时的"年度报告"）不计入候选。
func (s *SearchIndex) SuggestRelatedKeywords(prefix string, limit int) ([]string, error) {
	terms := strings.Fields(strings.ToLower(strings.TrimSpace(prefix)))
	if len(terms) == 0 {
		return []string{}, nil
	}

	s.rwMu.RLock()
	defer s.rwMu.RUnlock()
	if s.writer == nil {
		return []string{}, nil
	}

	reader, err := s.writer.Reader()
	if err != nil {
		return nil, fmt.Errorf("打开读端失败: %w", err)
	}
	defer reader.Close()

	// 各输入段以前缀查询求交（Must），兼容用户当前段尚未输完的情况。
	// 命中片段字段以保留 ASCII 中段匹配；回读仍取完整词字段存储的原始文件名。
	bq := bluge.NewBooleanQuery()
	for _, t := range terms {
		bq.AddMust(bluge.NewPrefixQuery(t).SetField(FieldFileNameNgram))
	}

	dmi, err := reader.Search(context.Background(), bluge.NewTopNSearch(relatedDocsLimit, bq))
	if err != nil {
		return nil, fmt.Errorf("相关词查询失败: %w", err)
	}
	names, err := collectFileNames(dmi)
	if err != nil {
		return nil, err
	}

	// 按文档去重计数（文档频次），避免同一文件内重复出现的词被高估
	docFreq := make(map[string]int)
	for _, name := range names {
		seen := make(map[string]struct{})
		for _, w := range fullWords(s.seg, name) {
			if utf8.RuneCountInString(w) < 2 {
				continue
			}
			// 跳过已输入段本身及其补全：用户敲下空格即视该段已完整，不再推荐同段延伸
			// （如输入"年度"后不再推荐"年度报告"）
			if hasAnyPrefix(w, terms) {
				continue
			}
			if _, dup := seen[w]; dup {
				continue
			}
			seen[w] = struct{}{}
			docFreq[w]++
		}
	}

	cand := make([]suggestTerm, 0, len(docFreq))
	for w, c := range docFreq {
		cand = append(cand, suggestTerm{term: w, count: uint64(c)})
	}
	sort.Slice(cand, func(i, j int) bool {
		if cand[i].count != cand[j].count {
			return cand[i].count > cand[j].count
		}
		return cand[i].term < cand[j].term
	})

	out := make([]string, 0, limit)
	for i := 0; i < len(cand) && len(out) < limit; i++ {
		out = append(out, cand[i].term)
	}
	return out, nil
}

// hasAnyPrefix 判断 s 是否以 prefixes 中任一前缀开头（含完全相等）。
func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
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
// 命中片段字段 file_name_ngram（含 N-gram），保留中段匹配能力；回读完整词字段存储的文件名。
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
		SetField(FieldFileNameNgram)
	req := bluge.NewTopNSearch(limit, q)

	dmi, err := reader.Search(context.Background(), req)
	if err != nil {
		return nil, fmt.Errorf("前缀查询失败: %w", err)
	}
	return collectFileNames(dmi)
}

// SpellCheck 拼写纠错：基于模糊查询。
// 命中片段字段 file_name_ngram（含 N-gram），与自动补全召回口径一致；回读完整词字段存储的文件名。
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
		SetField(FieldFileNameNgram)
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
