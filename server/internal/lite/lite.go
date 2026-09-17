package lite

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/filedir"
	"fuzhan/internal/resources"
	"fuzhan/internal/search"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
)

// liteName 轻量版页面的展示名称，统一用于标题等处。
const liteName = "轻量版"

// row 轻量版页面表格的一行
type row struct {
	Name  string
	Href  string
	IsDir bool
	Size  string
	Time  string
	Notes string
}

// crumb 面包屑节点；Href 为空表示当前节点（纯文本）
type crumb struct {
	Name string
	Href string
}

// page 轻量版页面渲染数据
type page struct {
	Title       string
	AppName     string
	LogoPath    string
	SearchQuery string
	Crumb       []crumb
	ResultInfo  string
	// 检索结果上方的推荐/纠错超链接（点击即以该关键词重新搜索）
	Recommends  []suggestion
	Corrections []suggestion
	Rows        []row
}

// suggestion 检索推荐/纠错项
type suggestion struct {
	Keyword string
	Href    string
}

// escapeSeg 等价于前端 JS encodeURIComponent：仅保留安全字符，其余按 UTF-8 字节转 %XX。
// 生成后可被 download 等相关 handler 的 URL 解码还原，且不受 "+" 空格换算影响。
func escapeSeg(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '!' || c == '~' ||
			c == '*' || c == '\'' || c == '(' || c == ')' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// encodeSegments 将已解码的路径段逐段转义后以 "/" 拼接成 URL 路径（不含前导斜杠）。
func encodeSegments(segs []string) string {
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		if s != "" {
			parts = append(parts, escapeSeg(s))
		}
	}
	return strings.Join(parts, "/")
}

// encodeRelPath 将根内相对路径（可含前导 /）编码为 "<rootName>/<子路径>" 的 URL 段。
func encodeRelPath(rootName, rel string) string {
	segs := []string{rootName}
	rel = strings.Trim(rel, "/")
	if rel != "" {
		segs = append(segs, strings.Split(rel, "/")...)
	}
	return encodeSegments(segs)
}

// formatSize 将字节数格式化为易读的大小
func formatSize(n int64) string {
	const (
		kb = int64(1024)
		mb = kb * 1024
		gb = mb * 1024
		tb = gb * 1024
	)
	switch {
	case n >= tb:
		return fmt.Sprintf("%.2f TB", float64(n)/float64(tb))
	case n >= gb:
		return fmt.Sprintf("%.2f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.2f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.2f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// formatTime 格式化修改时间为本地可读秒级形式
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

// formatModText 兼容 "2006-01-02T15:04:05" 与 RFC3339 两种来源的格式化展示
func formatModText(s string) string {
	if s == "" {
		return "-"
	}
	// DB 索引来源
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t.Local().Format("2006-01-02 15:04:05")
	}
	// RFC3339（文件系统 os.Stat）来源
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Local().Format("2006-01-02 15:04:05")
	}
	return s
}

// resolveSegments 解析 /lite 之后的路由路径段（已解码）。
func resolveSegments(r *http.Request) []string {
	p := strings.TrimPrefix(r.URL.Path, "/lite")
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	raw := strings.Split(p, "/")
	segs := make([]string, 0, len(raw))
	for _, s := range raw {
		dec, err := url.PathUnescape(s)
		if err != nil {
			dec = s
		}
		if dec != "" {
			segs = append(segs, dec)
		}
	}
	return segs
}

// browseTitle 生成随当前路径变化的浏览页标题
func browseTitle(head string, segs []string) string {
	if len(segs) == 0 {
		return head + " - " + liteName
	}
	return head + " - " + liteName + " / " + strings.Join(segs, "/")
}

// Handle 处理 /lite 服务器渲染浏览/检索请求
func Handle(w http.ResponseWriter, r *http.Request, svc *services.SearchService, idx *search.SearchIndex) {
	utils.PrintRequestInfo(r)

	head := appconfig.GlobalConfig.App.Name
	if head == "" {
		head = "Fuzhan"
	}
	page := page{
		Title:   head + " - " + liteName,
		AppName: head,
		// logo.png 为嵌入/磁盘资源，统一以 /assets/icons/logo.png 引用
		LogoPath: "/assets/icons/logo.png",
	}

	segs := resolveSegments(r)

	// 检索模式：search/?q=xxx 或 GET /lite?q=xxx
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		page.SearchQuery = q
		page.Title = head + " - 搜索: " + q
		renderSearch(w, page, svc, idx, q)
		return
	}

	// 根级：列出所有公开根目录
	if len(segs) == 0 {
		page.Crumb = []crumb{{Name: "根目录"}}
		var rootNames []string
		for rn := range appconfig.RootNames {
			rootNames = append(rootNames, rn)
		}
		sort.Strings(rootNames)
		for _, rn := range rootNames {
			row := row{
				Name:  rn,
				IsDir: true,
				Href:  "/lite/" + escapeSeg(rn),
				Size:  "-",
				Time:  "-",
				Notes: "-",
			}
			if rootPath, ok := appconfig.RootNames[rn]; ok {
				if fi, err := os.Stat(rootPath); err == nil {
					row.Time = formatTime(fi.ModTime())
				}
			}
			page.Rows = append(page.Rows, row)
		}
		render(w, page)
		return
	}

	rootName := segs[0]
	targetRoot, exists := appconfig.RootNames[rootName]
	if !exists {
		http.Error(w, "指定的目录不存在: "+rootName, http.StatusBadRequest)
		return
	}
	subParts := segs[1:]

	// 拒绝路径遍历段
	for _, seg := range subParts {
		if seg == "." || seg == ".." || strings.Contains(seg, "/") || strings.Contains(seg, "\\") {
			http.Error(w, "路径越权", http.StatusForbidden)
			return
		}
	}

	subPath := strings.Join(subParts, "/")
	targetPath := targetRoot
	if subPath != "" {
		targetPath = filepath.Join(targetRoot, filepath.FromSlash(subPath))
	}

	validator := utils.NewPathValidatorWithRoots(rootName, appconfig.RootNames)
	if validator != nil {
		if err := validator.Validate(targetPath); err != nil {
			http.Error(w, "路径越权", http.StatusForbidden)
			return
		}
	}

	info, err := os.Stat(targetPath)
	if err != nil || !info.IsDir() {
		http.Error(w, "指定的目录不存在", http.StatusNotFound)
		return
	}

	page.Title = browseTitle(head, segs)

	// 面包屑：根目录 -> ... -> 当前（当前为纯文本）
	page.Crumb = []crumb{{Name: "根目录", Href: "/lite"}}
	acc := make([]string, 0, len(segs))
	for i, seg := range segs {
		acc = append(acc, seg)
		if i == len(segs)-1 {
			page.Crumb = append(page.Crumb, crumb{Name: seg})
		} else {
			page.Crumb = append(page.Crumb, crumb{Name: seg, Href: "/lite/" + encodeSegments(acc)})
		}
	}

	// 枚举目录内容（复用统一过滤：忽略 .uploading、限制扩展名）
	items := filedir.GetDirectoryItems(targetPath, rootName, nil)

	for _, it := range items {
		// it.Path 为 "<rootName>/<相对路径>"，去掉前缀后即带单个前导 "/" 的相对路径，
		// 与 LoadDirectoryNotes 的 map 键格式一致；encodeRelPath 会 trim 前导斜杠所以 Href 不受影响
		rel := strings.TrimPrefix(it.Path, rootName)
		row := row{Name: it.Name, Time: formatModText(it.ModifiedTime), Notes: "-"}
		if it.Type == "directory" {
			row.IsDir = true
			row.Href = "/lite/" + encodeRelPath(rootName, rel)
			row.Size = "-"
		} else {
			row.Href = "/download/" + encodeRelPath(rootName, rel)
			row.Size = formatSize(it.Size)
		}
		page.Rows = append(page.Rows, row)
	}

	// 目录在前，文件在后；组内按名称排序
	sort.SliceStable(page.Rows, func(i, j int) bool {
		a, b := page.Rows[i], page.Rows[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return compareName(a.Name, b.Name)
	})

	render(w, page)
}

// renderSearch 渲染检索结果（关键词写法复用 SearchService.SearchFiles，与文件页一致），
// 并在结果上方渲染基于检索索引的推荐（自动补全）与纠错（模糊匹配）超链接。
func renderSearch(w http.ResponseWriter, page page, svc *services.SearchService, idx *search.SearchIndex, q string) {
	page.Crumb = []crumb{
		{Name: "根目录", Href: "/lite"},
		{Name: "搜索 " + q},
	}
	page.Recommends, page.Corrections = buildSuggestions(idx, q)
	if svc == nil {
		page.ResultInfo = "检索服务不可用"
		render(w, page)
		return
	}
	results, err := svc.SearchFiles(q, appconfig.GlobalConfig.Storage.Public.RootDirs, 30*time.Second)
	if err != nil {
		utils.Error("轻量版检索失败", utils.String("query", q), utils.Err(err))
		page.ResultInfo = "检索失败：" + err.Error()
		render(w, page)
		return
	}
	page.ResultInfo = fmt.Sprintf("搜索“%s”，共 %d 个结果", q, len(results))
	for _, res := range results {
		rootName := res.RootName
		rel := searchRel(res.Path, rootName)
		if rel == "" {
			continue
		}
		row := row{Name: res.Name, Time: formatModText(res.ModifiedTime), Notes: "-"}
		if res.Type == "directory" {
			row.IsDir = true
			row.Href = "/lite/" + encodeRelPath(rootName, rel)
			row.Size = "-"
		} else {
			row.Href = "/download/" + encodeRelPath(rootName, rel)
			row.Size = formatSize(res.Size)
		}
		if res.Notes != "" {
			row.Notes = res.Notes
		}
		page.Rows = append(page.Rows, row)
	}
	render(w, page)
}

// searchRel 将检索结果的完整路径（"/<rootName>/<相对路径>"）还原为带前导 "/" 的相对路径。
// 检索结果的 Path 已含 rootName，若直接交给 encodeRelPath 前置 rootName 会造成根目录名重复；
// 还原失败（rootName 为空或前缀不匹配）时返回空串，调用方跳过该行。
func searchRel(fullPath, rootName string) string {
	if rootName == "" {
		return ""
	}
	prefix := "/" + rootName + "/"
	if !strings.HasPrefix(fullPath, prefix) {
		return ""
	}
	return strings.TrimPrefix(fullPath, "/"+rootName)
}

// buildSuggestions 基于检索索引生成关键词推荐（前缀匹配）与纠错（模糊匹配），
// 返回的是索引中的关键词 term 而非完整文件名。二者共用一个去重集合，且排除当前查询词。
// 索引不可用时返回空切片。
func buildSuggestions(idx *search.SearchIndex, q string) (recommends, corrections []suggestion) {
	if idx == nil {
		return nil, nil
	}
	seen := map[string]struct{}{}
	add := func(dst []suggestion, names []string) []suggestion {
		for _, s := range names {
			s = strings.TrimSpace(s)
			if s == "" || s == q {
				continue
			}
			key := strings.ToLower(s)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			dst = append(dst, suggestion{
				Keyword: s,
				Href:    "/lite?q=" + url.QueryEscape(s),
			})
		}
		return dst
	}
	if names, err := idx.SuggestKeywords(q, 10); err == nil {
		recommends = add(nil, names)
	}
	if names, err := idx.SuggestCorrectKeywords(q, 5); err == nil {
		corrections = add(nil, names)
	}
	if recommends == nil {
		recommends = []suggestion{}
	}
	if corrections == nil {
		corrections = []suggestion{}
	}
	return recommends, corrections
}

// compareName 排序：ASCII 在前，组内按拼音
func compareName(a, b string) bool {
	aAscii := a != "" && a[0] < 0x80
	bAscii := b != "" && b[0] < 0x80
	if aAscii != bAscii {
		return aAscii
	}
	return strings.ToLower(a) < strings.ToLower(b)
}

// templateFuncs 页面模板的自定义函数
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"crumbSep": func(i, n int) bool { return i < n-1 },
	}
}

// embedTmplOnce 缓存的嵌入模板解析结果；磁盘模板存在时不用缓存。
var (
	embedTmplOnce sync.Once
	embedTmpl     *template.Template
	embedTmplErr  error
)

// loadPageTmpl 返回 /lite 页面模板：磁盘模板存在则每次重新解析（支持运行时免重启修改），
// 否则返回按嵌入内容缓存好的模板，避免每次请求重复解析。
func loadPageTmpl() (*template.Template, error) {
	content, fromDisk := resources.LitePageContent()
	if fromDisk {
		return template.New("lite").Funcs(templateFuncs()).Parse(content)
	}
	embedTmplOnce.Do(func() {
		embedTmpl, embedTmplErr = template.New("lite").Funcs(templateFuncs()).Parse(content)
	})
	return embedTmpl, embedTmplErr
}

// render 渲染 /lite 页面。模板内容优先读取磁盘上 resources/lite_page.html，
// 无则用嵌入模板（见 resources.LitePageContent），以便运行时免重启修改模板。
func render(w http.ResponseWriter, page page) {
	tmpl, err := loadPageTmpl()
	if err != nil {
		utils.Error("解析轻量版模板失败", utils.Err(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, page); err != nil {
		utils.Error("渲染轻量版页面失败", utils.Err(err))
	}
}
