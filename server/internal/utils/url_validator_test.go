package utils

import (
	"net"
	"testing"
)

// 空网段 = 安全默认：拒绝内网/回环/链路本地等保留地址，仅放行公网
// （防 SSRF，不允许隐式放行内网）
func TestCheckIPSafe_EmptyRangesBlocksInternal(t *testing.T) {
	cfg := URLUploadConfig{Enabled: true}
	internalIPs := []string{"10.0.0.1", "192.168.1.5", "172.16.0.1", "127.0.0.1", "169.254.169.254"}
	for _, s := range internalIPs {
		if err := CheckIPSafe(net.ParseIP(s), cfg); err == nil {
			t.Fatalf("空网段应拒绝内网/回环/链路本地地址 %s, got nil", s)
		}
	}
	if err := CheckIPSafe(net.ParseIP("8.8.8.8"), cfg); err != nil {
		t.Fatalf("空网段应放行公网地址 8.8.8.8, got err=%v", err)
	}
}

func TestCheckIPSafe_DisabledAllowsAll(t *testing.T) {
	cfg := URLUploadConfig{Enabled: false}
	if err := CheckIPSafe(net.ParseIP("10.0.0.1"), cfg); err != nil {
		t.Fatalf("disabled 状态应放行, got err=%v", err)
	}
}

func TestCheckIPSafe_ConfiguredRangesEnforced(t *testing.T) {
	cfg := URLUploadConfig{
		Enabled:         true,
		AllowedIPRanges: []string{"192.168.1.0/24"},
	}
	if err := CheckIPSafe(net.ParseIP("192.168.1.5"), cfg); err != nil {
		t.Fatalf("网段内 IP 应通过, got err=%v", err)
	}
	if err := CheckIPSafe(net.ParseIP("10.0.0.1"), cfg); err == nil {
		t.Fatalf("网段外 IP 应被拒绝, got nil")
	}
	// 非法 CIDR 条目应被忽略，仅静默跳过
	cfg.AllowedIPRanges = []string{"not-a-cidr"}
	if err := CheckIPSafe(net.ParseIP("10.0.0.1"), cfg); err == nil {
		t.Fatalf("仅含非法 CIDR 时应拒绝所有 IP, got nil")
	}
}
