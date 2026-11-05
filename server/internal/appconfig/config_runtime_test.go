package appconfig

import "testing"

func TestLocalhostWhenSetup(t *testing.T) {
	// H5：系统未初始化时，任意接口监听（空/0.0.0.0/::）强制回退本机 127.0.0.1；
	// 已初始化或显式具体 IP 时原样返回。
	cases := []struct {
		name         string
		initialized  bool
		host         string
		want         string
		wantRestrict bool
	}{
		{name: "uninit empty host restrict", initialized: false, host: "", want: "127.0.0.1", wantRestrict: true},
		{name: "uninit 0.0.0.0 restrict", initialized: false, host: "0.0.0.0", want: "127.0.0.1", wantRestrict: true},
		{name: "uninit :: restrict", initialized: false, host: "::", want: "127.0.0.1", wantRestrict: true},
		{name: "uninit explicit ip untouched", initialized: false, host: "192.168.1.10", want: "192.168.1.10", wantRestrict: false},
		{name: "initialized any host untouched", initialized: true, host: "", want: "", wantRestrict: false},
		{name: "initialized 0.0.0.0 untouched", initialized: true, host: "0.0.0.0", want: "0.0.0.0", wantRestrict: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			configMu.Lock()
			GlobalConfig = Config{App: APPConfig{Initialized: tc.initialized}}
			configMu.Unlock()

			got, restricted := LocalhostWhenSetup(tc.host)
			if got != tc.want || restricted != tc.wantRestrict {
				t.Fatalf("LocalhostWhenSetup(%q) = (%q, %v), want (%q, %v)",
					tc.host, got, restricted, tc.want, tc.wantRestrict)
			}
		})
	}
}
