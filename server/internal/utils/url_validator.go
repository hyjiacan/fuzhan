package utils

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// URLUploadConfig URL上传安全配置
type URLUploadConfig struct {
	Enabled         bool
	AllowedIPRanges []string // CIDR 网段列表，如 1.2.3.4/24；为空表示不限制（允许所有 IP）
}

// IsURLSafe 验证 URL 安全性，防止 SSRF 攻击
// enabled=true 启用限制，allowedIPRanges 允许的 IP 网段
func IsURLSafe(rawURL string, cfg URLUploadConfig) error {
	// 如果未启用安全限制，直接放行
	if !cfg.Enabled {
		return nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("无效的URL格式")
	}

	// 只允许 http/https 协议
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("只允许 http/https 协议")
	}

	// 获取主机（去除端口）
	host := parsed.Host
	if strings.Contains(host, ":") {
		host, _, _ = net.SplitHostPort(host)
	}

	// 解析 IP 地址
	ip := net.ParseIP(host)
	if ip == nil {
		// 是域名，尝试 DNS 解析
		ips, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("无法解析域名: %s", host)
		}
		if len(ips) == 0 {
			return fmt.Errorf("域名解析无结果: %s", host)
		}
		ip = ips[0] // 使用第一个 IP
	}

	// 检查 IP 是否在允许的网段内
	if err := CheckIPSafe(ip, cfg); err != nil {
		return err
	}

	return nil
}

// CheckIPSafe 验证 IP 是否在允许的网段内，用于 DNS 重绑定防护。
// 未配置任何网段（AllowedIPRanges 为空）时表示不限制，允许所有 IP。
func CheckIPSafe(ip net.IP, cfg URLUploadConfig) error {
	if !cfg.Enabled {
		return nil
	}

	// 未配置任何允许网段时表示不限制
	if len(cfg.AllowedIPRanges) == 0 {
		return nil
	}

	for _, cidr := range cfg.AllowedIPRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return nil
		}
	}
	return fmt.Errorf("IP 不在允许的网段内: %s", ip.String())
}
