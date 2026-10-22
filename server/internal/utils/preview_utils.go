package utils

import (
	"path/filepath"
	"strings"
	"sync"
)

// 预览分组名常量
const (
	PreviewGroupImage    = "image"
	PreviewGroupText     = "text"
	PreviewGroupAudio    = "audio"
	PreviewGroupVideo    = "video"
	PreviewGroupDocument = "document"
	PreviewGroupCustom   = "custom"
)

// 内置预览分组的默认 MIME / 扩展名列表（逗号分隔，支持通配符），
// 供配置「未设置」时作为缺省值；后端是默认值的唯一来源，
// 前端通过 options 接口的 preview.defaults 获取，不内置默认表。
// 默认表只收录浏览器原生可渲染的类型（img/audio/video/iframe 能直接显示或播放），
// 非原生类型（如 tif/heic/wma/mkv 等）不放入默认表，需要预览时由用户自行配置分组列表。
// custom 组没有默认列表：启用后两组均留空表示不限制（所有文件均可预览），且默认处于关闭状态。
var (
	previewGroupDefaultsMimes = map[string]string{
		PreviewGroupImage: "image/png,image/jpeg,image/gif,image/webp,image/avif,image/apng," +
			"image/bmp,image/vnd.microsoft.icon,image/x-icon,image/svg+xml",
		PreviewGroupText: "text/*,application/json,application/xml,application/javascript," +
			"application/yaml,application/sql,application/x-sh",
		PreviewGroupAudio: "audio/mpeg,audio/wav,audio/x-wav,audio/ogg,audio/mp4,audio/aac," +
			"audio/flac,audio/opus,audio/webm",
		PreviewGroupVideo:    "video/mp4,video/webm,video/ogg",
		PreviewGroupDocument: "application/pdf",
		PreviewGroupCustom:   "",
	}
	previewGroupDefaultsExts = map[string]string{
		PreviewGroupImage: "png,jpg,jpeg,gif,bmp,webp,svg,ico,avif,apng,jfif,pjpeg",
		PreviewGroupText: "txt,html,htm,css,js,json,xml,md,markdown,yml,yaml,csv," +
			"go,java,py,c,cpp,h,hpp,cs,rb,php,rs,sh,bat,ps1,sql,vue,tsx,jsx,ts,scss,less," +
			"ini,conf,cfg,log,properties,config,toml,checksum,mjs,cjs,m," +
			"kt,swift,dart,lua,r,pl,ex,erl,hs,nim,zig,groovy,scala,clj,tf," +
			"graphql,proto,adoc,vb,asm,f90",
		PreviewGroupAudio:    "mp3,wav,ogg,oga,m4a,aac,flac,opus,weba",
		PreviewGroupVideo:    "mp4,webm,m4v,ogv",
		PreviewGroupDocument: "pdf",
		PreviewGroupCustom:   "",
	}
)

// DefaultPreviewGroupMimes 返回指定分组的默认 MIME 列表（未知分组返回空串）
func DefaultPreviewGroupMimes(group string) string { return previewGroupDefaultsMimes[group] }

// DefaultPreviewGroupExts 返回指定分组的默认扩展名列表（未知分组返回空串）
func DefaultPreviewGroupExts(group string) string { return previewGroupDefaultsExts[group] }

// previewGroupOrder 内置分组匹配优先序（文件同时命中多组时取先者），与前端保持一致
var previewGroupOrder = []string{PreviewGroupImage, PreviewGroupAudio, PreviewGroupVideo, PreviewGroupDocument, PreviewGroupText}

// PreviewGroupState 单个预览分组的生效配置（各字段均已解析为具体值）
type PreviewGroupState struct {
	Enabled bool
	Mimes   string // 逗号分隔，支持通配符；空串表示不命中任何文件（custom 组除外）
	Exts    string // 同上
}

// PreviewInitConfig 预览判定所需的完整运行时配置
type PreviewInitConfig struct {
	Enabled bool
	Groups  map[string]PreviewGroupState
}

// previewGroupRules 分组配置的解析结果
type previewGroupRules struct {
	enabled      bool
	mimeExact    map[string]bool
	mimeWildcard []string
	extExact     map[string]bool
	extWildcard  []string
	unrestricted bool // 仅 custom 组语义：MIME 与扩展名均未配置表示不限制
}

// 预览配置缓存（包加载时即为「内置分组开启 + 默认列表 + custom 关闭」，与配置缺省语义一致）
var (
	previewMu      sync.RWMutex
	previewEnabled = true
	previewLast    = defaultPreviewInitConfig()
	previewRules   = buildPreviewRules(defaultPreviewInitConfig())
)

// defaultPreviewInitConfig 内置分组开启并使用默认列表、custom 组关闭（配置缺省时的行为）。
// custom 组默认关闭：避免「两组留空 = 不限制」的兜底语义在未显式启用时让所有文件都可预览。
func defaultPreviewInitConfig() PreviewInitConfig {
	groups := make(map[string]PreviewGroupState, 6)
	for _, g := range append(append([]string{}, previewGroupOrder...), PreviewGroupCustom) {
		groups[g] = PreviewGroupState{
			Enabled: g != PreviewGroupCustom,
			Mimes:   previewGroupDefaultsMimes[g],
			Exts:    previewGroupDefaultsExts[g],
		}
	}
	return PreviewInitConfig{Enabled: true, Groups: groups}
}

// buildPreviewRules 把分组配置解析为匹配规则
func buildPreviewRules(cfg PreviewInitConfig) map[string]*previewGroupRules {
	rules := make(map[string]*previewGroupRules, len(cfg.Groups))
	for name, g := range cfg.Groups {
		r := &previewGroupRules{enabled: g.Enabled, mimeExact: map[string]bool{}, extExact: map[string]bool{}}
		for _, m := range strings.Split(g.Mimes, ",") {
			m = strings.ToLower(strings.TrimSpace(m))
			if m == "" {
				continue
			}
			if strings.HasSuffix(m, "/*") {
				r.mimeWildcard = append(r.mimeWildcard, m[:len(m)-1])
			} else {
				r.mimeExact[m] = true
			}
		}
		for _, e := range strings.Split(g.Exts, ",") {
			e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
			if e == "" {
				continue
			}
			if strings.HasSuffix(e, "*") {
				r.extWildcard = append(r.extWildcard, e[:len(e)-1])
			} else {
				r.extExact[e] = true
			}
		}
		if name == PreviewGroupCustom {
			r.unrestricted = len(r.mimeExact) == 0 && len(r.mimeWildcard) == 0 &&
				len(r.extExact) == 0 && len(r.extWildcard) == 0
		}
		rules[name] = r
	}
	return rules
}

// ClassifyPreviewGroup 按各内置分组「已启用 + 已配置」的 MIME/扩展名列表归类文件：
// 按优先序（image > audio > video > document > text）返回首个命中的分组名；
// 未命中任何已启用内置分组返回 ""（仅由自定义组参与判定）。
func ClassifyPreviewGroup(ext, mimeType string) string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	mimeType = strings.ToLower(mimeType)
	previewMu.RLock()
	defer previewMu.RUnlock()
	for _, g := range previewGroupOrder {
		if previewGroupMatchedLocked(g, ext, mimeType) {
			return g
		}
	}
	return ""
}

// previewGroupMatchedLocked 在已持有读锁的前提下检查文件是否命中指定分组
func previewGroupMatchedLocked(name, ext, mimeType string) bool {
	r := previewRules[name]
	if r == nil || !r.enabled {
		return false
	}
	if r.unrestricted {
		return true
	}
	if mimeType != "" {
		if r.mimeExact[mimeType] {
			return true
		}
		for _, p := range r.mimeWildcard {
			if strings.HasPrefix(mimeType, p) {
				return true
			}
		}
	}
	if ext != "" {
		if r.extExact[ext] {
			return true
		}
		for _, p := range r.extWildcard {
			if strings.HasPrefix(ext, p) {
				return true
			}
		}
	}
	return false
}

// 用户自定义「扩展名 -> MIME 类型」运行时表，GetMimeType 优先于内置表匹配。
var (
	mimeTableMu   sync.RWMutex
	userMimeTable map[string]string
)

// InitMimeTable 加载用户自定义 MIME 表（扩展名均转小写存储，与内置表一致）。
// 配置为空（nil 或空 map）时重置为空表，不影响内置 fallback 行为。
func InitMimeTable(cfg map[string]string) {
	mimeTableMu.Lock()
	defer mimeTableMu.Unlock()
	userMimeTable = make(map[string]string, len(cfg))
	for ext, mt := range cfg {
		ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
		mt = strings.TrimSpace(mt)
		if ext == "" || mt == "" {
			continue
		}
		userMimeTable[ext] = strings.ToLower(mt)
	}
}

// InitPreviewConfig 初始化预览配置（全局开关 + 各分组生效配置）
func InitPreviewConfig(cfg PreviewInitConfig) {
	previewMu.Lock()
	defer previewMu.Unlock()
	previewEnabled = cfg.Enabled
	previewLast = cfg
	previewRules = buildPreviewRules(cfg)
}

// ReloadPreviewConfig 按最近一次配置重新初始化预览规则
func ReloadPreviewConfig() {
	previewMu.RLock()
	cfg := previewLast
	previewMu.RUnlock()
	InitPreviewConfig(cfg)
}

// IsPreviewable 检查文件是否可预览：
// 全局开关关闭时一律不可预览；开启时，文件命中任一已启用的内置分组
// （image/audio/video/document/text，按各自配置的 MIME/扩展名列表匹配）
// 或命中已启用的自定义组（custom：MIME 与扩展名列表均留空表示不限制）即可预览。
func IsPreviewable(filePath string, mimeType string) bool {
	previewMu.RLock()
	defer previewMu.RUnlock()
	if !previewEnabled {
		return false
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
	mimeType = strings.ToLower(mimeType)

	for _, g := range previewGroupOrder {
		if previewGroupMatchedLocked(g, ext, mimeType) {
			return true
		}
	}
	return previewGroupMatchedLocked(PreviewGroupCustom, ext, mimeType)
}

// GetMimeType 获取扩展名对应的 MIME 类型（用于预览识别的内置兜底）
// 查找顺序：内置常见类型表 -> 默认 application/octet-stream。
// 注意：用户自定义 MIME 表仅用于下载时 Content-Type（GetDownloadContentType），不影响预览识别。
func GetMimeType(ext string) string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))

	mimeMap := map[string]string{
		"txt":  "text/plain",
		"html": "text/html",
		"htm":  "text/html",
		"css":  "text/css",
		"js":   "application/javascript",
		"json": "application/json",
		"xml":  "application/xml",
		"md":   "text/markdown",
		"yml":  "text/yaml",
		"yaml": "text/yaml",
		"csv":  "text/csv",
		"go":   "text/plain",
		"java": "text/plain",
		"py":   "text/plain",
		"c":    "text/plain",
		"cpp":  "text/plain",
		"h":    "text/plain",
		"hpp":  "text/plain",
		"cs":   "text/plain",
		"rb":   "text/plain",
		"php":  "text/plain",
		"rs":   "text/plain",
		"sh":   "text/plain",
		"bat":  "text/plain",
		"ps1":  "text/plain",
		"sql":  "text/plain",
		"vue":  "text/plain",
		"tsx":  "text/plain",
		"jsx":  "text/plain",
		"ts":   "text/plain",
		"scss": "text/plain",
		"less": "text/plain",
		"ini":  "text/plain",
		"conf": "text/plain",
		"cfg":  "text/plain",
		"log":  "text/plain",
		"png":  "image/png",
		"jpg":  "image/jpeg",
		"jpeg": "image/jpeg",
		"gif":  "image/gif",
		"webp": "image/webp",
		"svg":  "image/svg+xml",
		"bmp":  "image/bmp",
		"ico":  "image/x-icon",
		"pdf":  "application/pdf",
		// 音频
		"mp3":  "audio/mpeg",
		"wav":  "audio/wav",
		"ogg":  "audio/ogg",
		"oga":  "audio/ogg",
		"m4a":  "audio/mp4",
		"aac":  "audio/aac",
		"flac": "audio/flac",
		"opus": "audio/opus",
		"mid":  "audio/midi",
		"weba": "audio/webm",
		// 视频
		"mp4":  "video/mp4",
		"webm": "video/webm",
		"mov":  "video/quicktime",
		"m4v":  "video/x-m4v",
		"avi":  "video/x-msvideo",
		"mkv":  "video/x-matroska",
		"ogv":  "video/ogg",
		"mpg":  "video/mpeg",
		"flv":  "video/x-flv",
		"wmv":  "video/x-ms-wmv",
		"3gp":  "video/3gpp",
	}
	if mime, ok := mimeMap[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}

// mimeTableLookup 在用户自定义 MIME 表中查询扩展名对应的 MIME 类型，未命中返回 ""。
func mimeTableLookup(ext string) string {
	mimeTableMu.RLock()
	defer mimeTableMu.RUnlock()
	return userMimeTable[ext]
}

// GetDownloadContentType 获取下载时应返回的 Content-Type。
// 仅查用户自定义 MIME 表：命中返回配置的 MIME，未配置返回 ""（由调用方回退原有行为，避免影响下载类型推断）。
func GetDownloadContentType(ext string) string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	if ext == "" {
		return ""
	}
	return mimeTableLookup(ext)
}

// GetPreviewMimeType 获取预览识别用的 MIME 类型（仅查用户自定义表）。
// 让设置页配置的「扩展名 -> MIME」能参与预览识别；未命中返回 ""，由调用方回退 Go 内置与内置兜底表。
func GetPreviewMimeType(ext string) string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	if ext == "" {
		return ""
	}
	return mimeTableLookup(ext)
}
