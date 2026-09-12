package resources

import (
	"os"
	"path/filepath"
)

// litePageDiskCandidates 返回磁盘上 lite_page.html 的候选路径。
// 与配置文件的解析顺序一致：优先二进制所在目录，其次当前工作目录。
func litePageDiskCandidates() []string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		if d, derr := filepath.Abs(filepath.Dir(exe)); derr == nil {
			dirs = append(dirs, d)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if a, aerr := filepath.Abs(cwd); aerr == nil {
			dirs = append(dirs, a)
		}
	}
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, filepath.Join(d, "internal", "resources", "lite_page.html"))
	}
	return out
}

// LitePageContent 返回 /lite 页面模板内容及来源：磁盘模板存在时 (内容, true)，
// 否则回退到程序内嵌入的模板返回 (嵌入内容, false)，供调用方按来源决定是否缓存解析。
func LitePageContent() (content string, fromDisk bool) {
	for _, p := range litePageDiskCandidates() {
		if data, err := os.ReadFile(p); err == nil {
			return string(data), true
		}
	}
	return LitePageTpl, false
}

// LoadLitePageTpl 读取 /lite 页面模板：运行时若磁盘上存在
// <binaryDir|CWD>/internal/resources/lite_page.html，则直接读取磁盘内容
// （便于免重启修改模板）；不存在时回退到程序内嵌入的 LitePageTpl。
func LoadLitePageTpl() string {
	content, _ := LitePageContent()
	return content
}
