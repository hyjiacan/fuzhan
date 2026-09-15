package services

import (
	"fmt"
	"path/filepath"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"

	"github.com/zeebo/xxh3"
)

// TestResolveUploadingPath 验证按 targetType 推导落盘残留文件路径的正确性，
// 覆盖公开/私有/临时三类及越界防护。
func TestResolveUploadingPath(t *testing.T) {
	base := t.TempDir()

	// 配置私有与临时存储根路径
	if appconfig.GlobalConfig.Storage.Private.Path == "" {
		appconfig.GlobalConfig.Storage.Private.Path = filepath.Join(base, "private")
	}
	if appconfig.GlobalConfig.Storage.Temp.Path == "" {
		appconfig.GlobalConfig.Storage.Temp.Path = filepath.Join(base, "temp")
	}

	// 公开存储根目录
	rootDir := filepath.Join(base, "public")
	appconfig.RootNames["dupes_test_root"] = rootDir

	cases := []struct {
		name    string
		session *models.UploadSession
		want    func() string
		ok      bool
	}{
		{
			name: "公开-根级",
			session: &models.UploadSession{
				TargetType: models.TargetTypeRegular,
				TargetRoot: "dupes_test_root",
				TargetPath: "",
				FileName:   "a.txt",
			},
			want: func() string { return filepath.Join(rootDir, ".a.txt.uploading") },
			ok:   true,
		},
		{
			name: "公开-子目录",
			session: &models.UploadSession{
				TargetType: models.TargetTypeRegular,
				TargetRoot: "dupes_test_root",
				TargetPath: "sub/dir",
				FileName:   "b.txt",
			},
			want: func() string { return filepath.Join(rootDir, "sub", "dir", ".b.txt.uploading") },
			ok:   true,
		},
		{
			name: "私有-子目录",
			session: &models.UploadSession{
				TargetType: models.TargetTypePrivate,
				UserID:     "u1",
				TargetPath: "sub",
				FileName:   "p.txt",
			},
			want: func() string {
				return filepath.Join(filepath.Join(base, "private"), "users", "u1", "sub", ".p.txt.uploading")
			},
			ok: true,
		},
		{
			name: "私有-越界跳转",
			session: &models.UploadSession{
				TargetType: models.TargetTypePrivate,
				UserID:     "u1",
				TargetPath: "../escape",
				FileName:   "p.txt",
			},
			want: nil,
			ok:   false,
		},
		{
			name: "临时-按码哈希",
			session: &models.UploadSession{
				TargetType: models.TargetTypeTemp,
				TargetPath: "",
				Code:       "ABCDEF0123456789ABCDEF0123456789",
			},
			want: func() string {
				hash := fmt.Sprintf("%016x", xxh3.Hash([]byte("ABCDEF0123456789ABCDEF0123456789"+"fuzhan-secret")))
				return filepath.Join(filepath.Join(base, "temp"), "."+hash+".uploading")
			},
			ok: true,
		},
		{
			name: "公开-上级跳转越界",
			session: &models.UploadSession{
				TargetType: models.TargetTypeRegular,
				TargetRoot: "dupes_test_root",
				TargetPath: "..",
				FileName:   "evil.txt",
			},
			want: nil,
			ok:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveUploadingPath(tc.session)
			if tc.ok {
				if err != nil {
					t.Fatalf("expect ok, got err: %v", err)
				}
				want := tc.want()
				if got != want {
					t.Fatalf("path mismatch:\n got  %s\n want %s", got, want)
				}
			} else {
				if err == nil {
					t.Fatalf("expect error, got path %s", got)
				}
			}
		})
	}
}
