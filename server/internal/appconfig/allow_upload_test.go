package appconfig

import "testing"

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
