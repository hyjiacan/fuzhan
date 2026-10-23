package webdav

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"strings"

	"gorm.io/gorm"

	"fuzhan/internal/models"
	"fuzhan/internal/utils"
)

// searchResult 单条搜索结果（对齐 WebDAV 目录浏览语义）。
type searchResult struct {
	Href  string // 相对服务器根的完整资源地址（带 Prefix），可被客户端直接访问
	Name  string // 展示名（文件名/目录名）
	IsDir bool
}

// SearchableHandler 在标准 WebDAV Handler 外层接管 SEARCH（DASL / RFC 5323）
// 与 REPORT（ownCloud 非标准检索）两类请求，其余方法原样透传给标准 Handler。
//
// SearchFilter 仅按目录/文件名做 LIKE 匹配，不支持文件内容检索。
type SearchableHandler struct {
	Standard http.Handler
	DB       *gorm.DB
	Prefix   string // 与标准 Handler 的 Prefix 保持一致，用于拼接结果 href
}

// searchResultLimit 单表检索结果上限，避免大目录返回过大的 multistatus。
const searchResultLimit = 200

// readSearchBodyLimit 检索请求体上限（防止极端构造的大请求体）。
const readSearchBodyLimit = 1 << 20 // 1 MiB

// ServeHTTP 分流检索方法与常规 WebDAV 方法。
func (h *SearchableHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "SEARCH":
		h.handleSEARCH(w, r)
	case "REPORT":
		h.handleREPORT(w, r)
	default:
		h.Standard.ServeHTTP(w, r)
	}
}

// handleSEARCH 处理 DASL 的 SEARCH 请求：解析请求体中的 <D:literal> 得到关键字。
func (h *SearchableHandler) handleSEARCH(w http.ResponseWriter, r *http.Request) {
	body, _ := readLimitedBody(r)
	keyword := extractFirstElementText(body, "literal")
	h.searchAndRespond(w, r, keyword)
}

// handleREPORT 处理 ownCloud 非标准的 REPORT 请求：解析 <oc:pattern> 得到关键字。
// 遇到不认识的 REPORT body 时返回空 multistatus，而非报错，避免客户端异常。
func (h *SearchableHandler) handleREPORT(w http.ResponseWriter, r *http.Request) {
	body, _ := readLimitedBody(r)
	keyword := extractFirstElementText(body, "pattern", "literal")
	h.searchAndRespond(w, r, keyword)
}

// searchAndRespond 按关键字检索索引表并输出 DASL multistatus 响应。
// 空关键字直接返回空结果集。
func (h *SearchableHandler) searchAndRespond(w http.ResponseWriter, r *http.Request, keyword string) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		writeMultiStatus(w, nil)
		return
	}
	results := h.searchFiles(r.Context(), currentUserUUID(r), keyword)
	writeMultiStatus(w, results)
}

// currentUserUUID 从请求上下文读取已认证用户 UUID（由 OptionalBasicAuthMiddleware 注入），
// 匿名请求为空串。
func currentUserUUID(r *http.Request) string {
	uuid, _ := r.Context().Value(ContextKeyUserUUID).(string)
	return uuid
}

// searchFiles 检索公开（file_records_public）与私有（file_records_private）索引表。
// 私有结果仅在已认证时返回，且只返回当前用户自己拥有的文件，避免泄露他人私有数据。
// 临时文件（file_records_temp）不在统一 WebDAV 命名空间暴露，故不参与检索，
// 以保证返回的 href 均可直接访问。
func (h *SearchableHandler) searchFiles(ctx context.Context, userUUID, keyword string) []searchResult {
	if h.DB == nil {
		return nil
	}
	pattern := "%" + keyword + "%"
	results := make([]searchResult, 0, 32)
	seen := make(map[string]struct{})

	add := func(href, name string, isDir bool) {
		if href == "" {
			return
		}
		if _, dup := seen[href]; dup {
			return
		}
		seen[href] = struct{}{}
		results = append(results, searchResult{Href: href, Name: name, IsDir: isDir})
	}

	// 公开文件：统一命名空间下位于 /public/<root>/<path>
	var pubs []models.FileRecordPublic
	if err := h.DB.WithContext(ctx).
		Model(&models.FileRecordPublic{}).
		Where("status = ? AND LOWER(file_name) LIKE LOWER(?)", models.FileStatusActive, pattern).
		Order("id").
		Limit(searchResultLimit).
		Find(&pubs).Error; err != nil {
		utils.WebDAVWarn("WebDAV 检索公开文件失败",
			utils.String("keyword", keyword), utils.Err(err))
	} else {
		for _, p := range pubs {
			add(h.Prefix+"/public"+p.FullPath, p.FileName, p.IsDir)
		}
	}

	// 私有文件：统一命名空间下位于 /private/<relPath>，仅当前用户本人可见
	if userUUID != "" {
		var privs []models.FileRecordPrivate
		if err := h.DB.WithContext(ctx).
			Model(&models.FileRecordPrivate{}).
			Where("status = ? AND owner_id = ? AND LOWER(file_name) LIKE LOWER(?)",
				models.FileStatusActive, userUUID, pattern).
			Order("id").
			Limit(searchResultLimit).
			Find(&privs).Error; err != nil {
			utils.WebDAVWarn("WebDAV 检索私有文件失败",
				utils.String("keyword", keyword), utils.Err(err))
		} else {
			for _, p := range privs {
				add(h.Prefix+p.FullPath, p.FileName, p.IsDir)
			}
		}
	}

	return results
}

// readLimitedBody 以限定上限读取请求体，避免超大请求体导致内存膨胀。
func readLimitedBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(io.LimitReader(r.Body, readSearchBodyLimit))
}

// extractFirstElementText 从 XML 中提取首个匹配的 local name 元素的文本内容。
// 以 local name（忽略命名空间前缀）匹配，兼容 DASL 的 <D:literal> 与
// ownCloud 的 <oc:pattern> 等不同命名空间写法。
func extractFirstElementText(data []byte, localNames ...string) string {
	if len(data) == 0 {
		return ""
	}
	want := make(map[string]bool, len(localNames))
	for _, n := range localNames {
		want[n] = true
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		start, ok := tok.(xml.StartElement)
		if !ok || !want[start.Name.Local] {
			continue
		}
		// 读取该元素内的全部字符数据（含嵌套），直到对应 EndElement
		var sb strings.Builder
		for {
			inner, err := dec.Token()
			if err != nil {
				return strings.TrimSpace(sb.String())
			}
			switch t := inner.(type) {
			case xml.CharData:
				sb.Write(t)
			case xml.EndElement:
				if t.Name.Local == start.Name.Local {
					return strings.TrimSpace(sb.String())
				}
			}
		}
	}
}

// writeMultiStatus 输出 DASL 检索结果的 multistatus 响应（HTTP 207）。
// 每个结果一个 <D:response>，resourcetype 按是否目录输出 <D:collection/>。
func writeMultiStatus(w http.ResponseWriter, results []searchResult) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.Header().Set("DAV", "1")
	w.WriteHeader(http.StatusMultiStatus)

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	sb.WriteString(`<D:multistatus xmlns:D="DAV:">`)
	for _, res := range results {
		sb.WriteString(`<D:response><D:href>`)
		sb.WriteString(xmlEscape(res.Href))
		sb.WriteString(`</D:href><D:propstat><D:prop><D:resourcetype>`)
		if res.IsDir {
			sb.WriteString(`<D:collection/>`)
		}
		sb.WriteString(`</D:resourcetype><D:displayname>`)
		sb.WriteString(xmlEscape(res.Name))
		sb.WriteString(`</D:displayname></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>`)
	}
	sb.WriteString(`</D:multistatus>`)
	_, _ = w.Write([]byte(sb.String()))
}

// xmlEscape 转义 XML 特殊字符。
func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}
