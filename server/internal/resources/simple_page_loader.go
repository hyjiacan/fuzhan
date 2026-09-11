package resources

import (
	"os"
	"path/filepath"
)

// simplePageDiskCandidates 返回磁盘上 simple_page.html 的候选路径。
// 与配置文件的解析顺序一致：优先二进制所在目录，其次当前工作目录。
func simplePageDiskCandidates() []string {
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
		out = append(out, filepath.Join(d, "internal", "resources", "simple_page.html"))
	}
	return out
}

// LoadSimplePageTpl 读取 /simple 页面模板：运行时若磁盘上存在
// <binaryDir|CWD>/internal/resources/simple_page.html，则直接读取磁盘内容
// （便于免重启修改模板）；不存在时回退到程序内嵌入的 SimplePageTpl。
func LoadSimplePageTpl() string {
	for _, p := range simplePageDiskCandidates() {
		if data, err := os.ReadFile(p); err == nil {
			return string(data)
		}
	}
	return SimplePageTpl
}
