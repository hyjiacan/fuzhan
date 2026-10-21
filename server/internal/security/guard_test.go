package security

import (
	"testing"

	"fuzhan/internal/models"
)

func TestGuardForScope(t *testing.T) {
	cases := []struct {
		scope string
		want  string
	}{
		{"webdav", models.GuardWebDAV},
		{"ftp", models.GuardFTP},
		{"share", models.GuardAccessguard},
		{"temp", models.GuardAccessguard},
		{"upload", models.GuardAccessguard},
		{"", models.GuardAccessguard},
	}
	for _, c := range cases {
		if got := GuardForScope(c.scope); got != c.want {
			t.Errorf("GuardForScope(%q) = %q，期望 %q", c.scope, got, c.want)
		}
	}
}

func TestFailLockReason(t *testing.T) {
	if got := FailLockReason("webdav"); got != "WebDAV 认证失败达阈值，已对该 IP 锁定" {
		t.Errorf("webdav 锁定文案异常：%q", got)
	}
	if got := FailLockReason("share"); got != "无效码/认证失败达阈值，已对该 IP 锁定" {
		t.Errorf("默认锁定文案异常：%q", got)
	}
}
