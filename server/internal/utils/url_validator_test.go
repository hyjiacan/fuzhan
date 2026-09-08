package utils

import (
	"net"
	"testing"
)

func TestCheckIPSafe_EmptyRangesMeansUnrestricted(t *testing.T) {
	ip := net.ParseIP("10.0.0.1")
	cfg := URLUploadConfig{Enabled: true}
	if err := CheckIPSafe(ip, cfg); err != nil {
		t.Fatalf("空网段应放行所有 IP, got err=%v", err)
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
