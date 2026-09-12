package appconfig

import "testing"

func TestGetSecurityConfig_EmptyOriginsMeansUnrestricted(t *testing.T) {
	cases := []struct {
		name    string
		origins []string
		wantAll bool
		wantLen int
	}{
		{name: "nil origins allows all", origins: nil, wantAll: true, wantLen: 1},
		{name: "empty origins allows all", origins: []string{}, wantAll: true, wantLen: 1},
		{name: "configured whitelist kept", origins: []string{"https://a.example"}, wantAll: false, wantLen: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configMu.Lock()
			GlobalConfig = Config{Security: SecurityConfig{AllowedOrigins: tc.origins}}
			configMu.Unlock()

			got := GetSecurityConfig()
			if tc.wantAll {
				if len(got.AllowedOrigins) != 1 || got.AllowedOrigins[0] != "*" {
					t.Fatalf("AllowedOrigins = %v, want [*]", got.AllowedOrigins)
				}
				return
			}
			if got.AllowedOrigins[0] != "https://a.example" {
				t.Fatalf("AllowedOrigins = %v, want configured origin", got.AllowedOrigins)
			}
			_ = tc.wantLen
		})
	}
}

func TestGetReservedUsernames_EmptyMeansOnlyAnonymous(t *testing.T) {
	cases := []struct {
		name         string
		reserved     []string
		anon         string
		wantReserved bool // 是否额外出现 anonymous
		wantLen      int
	}{
		{name: "nil reserved means unlimited", reserved: nil, anon: "public", wantReserved: true, wantLen: 2},
		{name: "empty reserved means unlimited", reserved: []string{}, anon: "public", wantReserved: true, wantLen: 2},
		{name: "configured list kept plus anonymous", reserved: []string{"admin", "root"}, anon: "public", wantReserved: true, wantLen: 4},
		{name: "anonymous alias always reserved", reserved: nil, anon: "", wantReserved: true, wantLen: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configMu.Lock()
			GlobalConfig = Config{
				Account: AccountConfig{
					Anonymous:         AnonymousConfig{Username: tc.anon},
					ReservedUsernames: tc.reserved,
				},
			}
			configMu.Unlock()

			got := GetReservedUsernames()
			if len(got) != tc.wantLen {
				t.Fatalf("len(got) = %d (%v), want %d", len(got), got, tc.wantLen)
			}
			// 空配置时不应出现默认的 admin/root
			if tc.reserved == nil || len(tc.reserved) == 0 {
				for _, u := range got {
					if u == "admin" || u == "root" {
						t.Fatalf("空配置不应保留默认用户名, got %v", got)
					}
				}
			}
			if !contains(got, "anonymous") {
				t.Fatalf("got %v, want contains anonymous", got)
			}
			if tc.wantReserved && tc.anon != "" && !contains(got, tc.anon) {
				t.Fatalf("got %v, want contains %s", got, tc.anon)
			}
		})
	}
}

func contains(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}
