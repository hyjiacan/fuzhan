package appconfig

import "testing"

// TestDirectoryConfig_SecurityExemptions 覆盖"按操作粒度豁免"的访问器默认语义：
// 未配置时一律沿用全局安全限制（false），显式开启才放行。
func TestDirectoryConfig_SecurityExemptions(t *testing.T) {
	cases := []struct {
		name          string
		dir           DirectoryConfig
		wantOverwrite bool
		wantRename    bool
		wantMove      bool
		wantDelete    bool
	}{
		{"未配置默认沿用全局限制", DirectoryConfig{Path: "x"}, false, false, false, false},
		{
			"显式开启全部豁免",
			DirectoryConfig{Path: "x", AllowOverwrite: true, AllowRename: true, AllowMove: true, AllowDelete: true},
			true, true, true, true,
		},
		{
			"仅开启重命名",
			DirectoryConfig{Path: "x", AllowRename: true},
			false, true, false, false,
		},
		{
			"仅开启移动",
			DirectoryConfig{Path: "x", AllowMove: true},
			false, false, true, false,
		},
		{
			"仅开启删除",
			DirectoryConfig{Path: "x", AllowDelete: true},
			false, false, false, true,
		},
	}
	for _, c := range cases {
		if got := c.dir.OverwriteAllowed(); got != c.wantOverwrite {
			t.Errorf("%s：OverwriteAllowed()=%v，期望 %v", c.name, got, c.wantOverwrite)
		}
		if got := c.dir.RenameAllowed(); got != c.wantRename {
			t.Errorf("%s：RenameAllowed()=%v，期望 %v", c.name, got, c.wantRename)
		}
		if got := c.dir.MoveAllowed(); got != c.wantMove {
			t.Errorf("%s：MoveAllowed()=%v，期望 %v", c.name, got, c.wantMove)
		}
		if got := c.dir.DeleteAllowed(); got != c.wantDelete {
			t.Errorf("%s：DeleteAllowed()=%v，期望 %v", c.name, got, c.wantDelete)
		}
	}
}

// TestPublicDirSecurityExemptions 验证按 rootName 查询目录豁免开关，未知目录一律不放行。
func TestPublicDirSecurityExemptions(t *testing.T) {
	orig := GlobalConfig.Storage.Public.RootDirs
	GlobalConfig.Storage.Public.RootDirs = []DirectoryConfig{
		{Path: "/srv/shares/open", AllowOverwrite: true, AllowRename: true, AllowMove: true, AllowDelete: true},
		{Path: "/srv/shares/strict"}, // 未配置，沿用全局限制
	}
	defer func() { GlobalConfig.Storage.Public.RootDirs = orig }()

	cases := []struct {
		root          string
		wantOverwrite bool
		wantRename    bool
		wantMove      bool
		wantDelete    bool
	}{
		{"open", true, true, true, true},
		{"strict", false, false, false, false},
		{"unknown", false, false, false, false}, // 未知目录保持安全默认
	}
	for _, c := range cases {
		if got := PublicDirOverwriteAllowed(c.root); got != c.wantOverwrite {
			t.Errorf("PublicDirOverwriteAllowed(%q)=%v，期望 %v", c.root, got, c.wantOverwrite)
		}
		if got := PublicDirRenameAllowed(c.root); got != c.wantRename {
			t.Errorf("PublicDirRenameAllowed(%q)=%v，期望 %v", c.root, got, c.wantRename)
		}
		if got := PublicDirMoveAllowed(c.root); got != c.wantMove {
			t.Errorf("PublicDirMoveAllowed(%q)=%v，期望 %v", c.root, got, c.wantMove)
		}
		if got := PublicDirDeleteAllowed(c.root); got != c.wantDelete {
			t.Errorf("PublicDirDeleteAllowed(%q)=%v，期望 %v", c.root, got, c.wantDelete)
		}
	}
}

func TestDirectoryConfig_UploadAllowed(t *testing.T) {
	falseVal := false
	trueVal := true

	cases := []struct {
		name string
		dir  DirectoryConfig
		want bool
	}{
		{"未配置（nil）默认允许", DirectoryConfig{Path: "x"}, true},
		{"显式允许", DirectoryConfig{Path: "x", AllowUpload: &trueVal}, true},
		{"显式禁止", DirectoryConfig{Path: "x", AllowUpload: &falseVal}, false},
	}
	for _, c := range cases {
		if got := c.dir.UploadAllowed(); got != c.want {
			t.Errorf("%s：UploadAllowed()=%v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestPublicDirUploadAllowed(t *testing.T) {
	falseVal := false
	RootDirs := []DirectoryConfig{
		{Path: "/srv/shares/readonly", AllowUpload: &falseVal},
		{Path: "/srv/shares/writable"}, // 未配置，默认允许
	}
	orig := GlobalConfig.Storage.Public.RootDirs
	GlobalConfig.Storage.Public.RootDirs = RootDirs
	defer func() { GlobalConfig.Storage.Public.RootDirs = orig }()

	cases := []struct {
		root string
		want bool
	}{
		{"readonly", false}, // 禁止上传的目录
		{"writable", true},  // 未配置的目录默认允许
		{"unknown", true},   // 未知目录按允许处理，避免误伤既有能力
	}
	for _, c := range cases {
		if got := PublicDirUploadAllowed(c.root); got != c.want {
			t.Errorf("PublicDirUploadAllowed(%q)=%v，期望 %v", c.root, got, c.want)
		}
	}
}

// TestReservedRootNameReason 覆盖公共根目录名与系统命名空间的保留名判定：
// private/temp 及其大小写变体、带空白的输入都应被判定为冲突。
func TestReservedRootNameReason(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"private", "私有存储命名空间"},
		{"PRIVATE", "私有存储命名空间"},
		{" private ", "私有存储命名空间"},
		{"temp", "临时文件命名空间"},
		{"Temp", "临时文件命名空间"},
		{"documents", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := ReservedRootNameReason(c.name); got != c.want {
			t.Errorf("ReservedRootNameReason(%q)=%q，期望 %q", c.name, got, c.want)
		}
	}
}
