package security

import (
	"net/http"
	"testing"

	"fuzhan/internal/models"
)

func TestNoteLoginFailure_SingleFailureNotEvent(t *testing.T) {
	ResetAuthFailureState()
	if d := NoteLoginFailure("198.51.100.10", "alice", "test-agent", false); d != nil {
		t.Fatalf("单次认证失败不应构成安全事件，got %+v", d)
	}
}

func TestNoteLoginFailure_AccountThresholdAggregatesOnce(t *testing.T) {
	ResetAuthFailureState()
	const ip, user = "198.51.100.11", "bob"
	var d *AuthEventDecision
	for i := 0; i < authAccountFailureLimit; i++ {
		d = NoteLoginFailure(ip, user, "test-agent", false)
	}
	if d == nil || d.EventType != models.EventAuthFailedLogin || d.Severity != models.SeverityHigh {
		t.Fatalf("账号失败达阈值应聚合为 1 条高危 auth.failed_login，got %+v", d)
	}
	// 同一窗口内后续失败不再重复入库
	if d := NoteLoginFailure(ip, user, "test-agent", false); d != nil {
		t.Fatalf("阈值后同窗口不应重复入库，got %+v", d)
	}
}

func TestNoteLoginFailure_PasswordSprayingAcrossUsernames(t *testing.T) {
	ResetAuthFailureState()
	const ip = "198.51.100.12"
	var d *AuthEventDecision
	for _, user := range []string{"u1", "u2", "u3"} {
		d = NoteLoginFailure(ip, user, "test-agent", false)
	}
	if d == nil || d.EventType != models.EventAuthSpraying || d.Severity != models.SeverityHigh {
		t.Fatalf("同 IP 多账号失败应记 auth.spraying 高危，got %+v", d)
	}
}

func TestNoteLoginFailure_SameAccountAcrossIPs(t *testing.T) {
	ResetAuthFailureState()
	const user = "carol"
	var d *AuthEventDecision
	for _, ip := range []string{"198.51.100.13", "198.51.100.14", "198.51.100.15"} {
		d = NoteLoginFailure(ip, user, "test-agent", false)
	}
	if d == nil || d.EventType != models.EventAuthSpraying || d.Severity != models.SeverityHigh {
		t.Fatalf("同账号多 IP 失败应记 auth.spraying 高危，got %+v", d)
	}
}

func TestNoteLoginSuccess_AfterFailuresFromDifferentSource(t *testing.T) {
	ResetAuthFailureState()
	user := "dave"
	for i := 0; i < authSuccessAfterFails; i++ {
		NoteLoginFailure("198.51.100.16", user, "agent-a", false)
	}
	d := NoteLoginSuccess("198.51.100.17", user, "agent-b")
	if d == nil || d.EventType != models.EventAuthSuspiciousLogin || d.Severity != models.SeverityHigh {
		t.Fatalf("失败后从不同来源成功应记 auth.suspicious_login 高危，got %+v", d)
	}
}

func TestNoteLoginSuccess_SameSourceNotEvent(t *testing.T) {
	ResetAuthFailureState()
	user := "erin"
	for i := 0; i < authSuccessAfterFails; i++ {
		NoteLoginFailure("198.51.100.18", user, "agent-a", false)
	}
	if d := NoteLoginSuccess("198.51.100.18", user, "agent-a"); d != nil {
		t.Fatalf("同 IP/UA 输错后成功属于正常操作，不应构成事件，got %+v", d)
	}
	// 成功后失败计数已清空，再次单次失败不构成事件
	if d := NoteLoginFailure("198.51.100.18", user, "agent-a", false); d != nil {
		t.Fatalf("成功后计数应重置，got %+v", d)
	}
}

func TestNoteLoginFailure_AdminEscalation(t *testing.T) {
	ResetAuthFailureState()
	const ip, user = "198.51.100.19", "admin"
	first := NoteLoginFailure(ip, user, "test-agent", true)
	if first == nil || first.Severity != models.SeverityMedium {
		t.Fatalf("管理员账号首次失败应为中危，got %+v", first)
	}
	second := NoteLoginFailure(ip, user, "test-agent", true)
	if second == nil || second.Severity != models.SeverityHigh {
		t.Fatalf("管理员账号连续失败应升级为高危，got %+v", second)
	}
	if third := NoteLoginFailure(ip, user, "test-agent", true); third != nil {
		t.Fatalf("升级后同窗口不应重复入库，got %+v", third)
	}
}

func TestEvaluateUnauthorized(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		guard  string
		want   bool
	}{
		{"普通读取不构成事件", http.MethodGet, "/api/v1/files", models.GuardAuth, false},
		{"未认证访问管理接口", http.MethodGet, "/api/v1/web/admin/users", models.GuardAuth, true},
		{"未认证写操作", http.MethodPost, "/api/v1/web/private/uploads/session", models.GuardAuth, true},
		{"会话维护接口豁免", http.MethodPost, "/api/v1/web/auth/refresh", models.GuardAuth, false},
		{"WebDAV 认证失败", http.MethodGet, "/api/v1/webdav/x", models.GuardWebDAV, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := EvaluateUnauthorized(tc.method, tc.path, tc.guard)
			if got != tc.want {
				t.Fatalf("EvaluateUnauthorized(%s %s, guard=%s) = %v, want %v", tc.method, tc.path, tc.guard, got, tc.want)
			}
		})
	}
}
