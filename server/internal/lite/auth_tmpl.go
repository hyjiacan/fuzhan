package lite

import (
	"html/template"
	"os"
	"path/filepath"
	"sync"

	"fuzhan/internal/resources"
	"fuzhan/internal/utils"
)

// authTemplateFuncs 认证页模板的自定义函数。
// AllowRegistration 供模板在登录页控制「注册入口」显隐：
// （是否启用私有存储、是否启用强制登录都不影响注册入口是否开放）。
func authTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"AllowRegistration": AllowRegistration,
	}
}

// 轻量版认证页（登录/注册）模板：与浏览页模板独立加载。
// 磁盘模板存在（internal/resources/auth_page.html）则每次重新解析，支持运行时免重启修改；
// 无磁盘模板时使用嵌入模板并缓存解析结果。

// authPageDiskCandidates 返回磁盘上 auth_page.html 的候选路径（与浏览页模板一致：
// 优先二进制所在目录，其次当前工作目录）。
func authPageDiskCandidates() []string {
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
		out = append(out, filepath.Join(d, "internal", "resources", "auth_page.html"))
	}
	return out
}

var (
	authTmplOnce sync.Once
	authTmplCached *template.Template
	authTmplErr   error
)

// authTmpl 返回认证页模板：磁盘模板存在则每次重新解析，否则返回缓存的嵌入模板。
func authTmpl() (*template.Template, error) {
	for _, p := range authPageDiskCandidates() {
		if data, err := os.ReadFile(p); err == nil {
			t, perr := template.New("auth").Funcs(authTemplateFuncs()).Parse(string(data))
			if perr != nil {
				utils.Error("解析磁盘认证页模板失败", utils.Err(perr), utils.String("path", p))
				continue
			}
			return t, nil
		}
	}
	authTmplOnce.Do(func() {
		authTmplCached, authTmplErr = template.New("auth").Funcs(authTemplateFuncs()).Parse(resources.LiteAuthPageTpl)
	})
	return authTmplCached, authTmplErr
}
