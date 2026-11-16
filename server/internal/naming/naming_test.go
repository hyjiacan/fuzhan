package naming

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/file"
	"fuzhan/internal/index"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/setup"
)

// snakeKey 合法 API 字段名：小写字母/数字，可用下划线分隔。
var snakeKey = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// checkStruct 递归检查结构体 json tag。
// path 记录当前递归路径上的结构体类型，避免自引用（如依赖树节点）导致无限递归，
// 同时保证同一类型在不同字段下重复出现时仍会被逐字段检查。
func checkStruct(t *testing.T, name string, typ reflect.Type, skip map[string]bool) {
	t.Helper()
	checkStructPath(t, name, typ, skip, map[reflect.Type]bool{})
}

func checkStructPath(t *testing.T, name string, typ reflect.Type, skip map[string]bool, path map[reflect.Type]bool) {
	t.Helper()
	if path[typ] {
		return
	}
	path[typ] = true
	defer delete(path, typ)
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" && !f.Anonymous {
			continue // 未导出字段
		}
		if skip[f.Name] {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		key := strings.Split(tag, ",")[0]
		if key == "" || key == "-" {
			continue
		}
		if !snakeKey.MatchString(key) {
			t.Errorf("%s.%s: json tag %q 不是 snake_case", name, f.Name, key)
			continue
		}
		ft := f.Type
		for ft.Kind() == reflect.Ptr || ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && ft.PkgPath() != "time" {
			checkStructPath(t, name+"."+f.Name, ft, nil, path)
		}
	}
}

func TestRequestAndResponseTagsAreSnakeCase(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"services.RegisterRequest", services.RegisterRequest{}},
		{"services.LoginRequest", services.LoginRequest{}},
		{"services.AuthResponse", services.AuthResponse{}},
		{"file.FileChecksumRequest", file.FileChecksumRequest{}},
		{"setup.SetupConfigRequest", setup.SetupConfigRequest{}},
		{"appconfig.ConfigDTO", appconfig.ConfigDTO{}},
		{"models.UploadRequest", models.UploadRequest{}},
		{"models.UploadChunkRequest", models.UploadChunkRequest{}},
		{"models.FinalizeUploadRequest", models.FinalizeUploadRequest{}},
		{"models.ExtendTempFileRequest", models.ExtendTempFileRequest{}},
		{"index.ScanProgressView", index.ScanProgressView{}},
		{"index.DuplicateGroup", index.DuplicateGroup{}},
		{"index.DependencyTreeNode", index.DependencyTreeNode{}},
		{"index.RecordStatsResponse", index.RecordStatsResponse{}},
		{"index.ScanStatusResponse", index.ScanStatusResponse{}},
	}
	for _, c := range cases {
		checkStruct(t, c.name, reflect.TypeOf(c.v), nil)
	}
}
