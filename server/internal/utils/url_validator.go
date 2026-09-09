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
	AllowedIPRanges []string // CIDR 网段列表，如 1.2.3.4/24；为空时默认拒绝内网/回环/保留地址（不表示放行所有）
}

// IsURLSafe 验证 URL 安全性，防止 SSRF 攻击
// enabled=true 启用限制；allowedIPRanges 显式允许的 IP 网段
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
		// 遍历全部解析 IP，任一不安全即拒绝（防 DNS 重绑定/多 A 记录绕过）
		for _, resolvedIP := range ips {
			if err := CheckIPSafe(resolvedIP, cfg); err != nil {
				return fmt.Errorf("SSRF 防护: %s (%s) 不安全", host, resolvedIP.String())
			}
		}
		return nil
	}

	return CheckIPSafe(ip, cfg)
}

// CheckIPSafe 验证 IP 是否允许访问，用于 DNS 重绑定防护。
// - 显式配置 AllowedIPRanges 时作为纯白名单，仅允许列出的网段（含用户明确放行的内网段）；
// - 未配置网段时采用安全默认：拒绝内网/回环/链路本地等保留地址。
func CheckIPSafe(ip net.IP, cfg URLUploadConfig) error {
	if !cfg.Enabled {
		return nil
	}

	// 显式白名单：仅允许列出的网段
	if len(cfg.AllowedIPRanges) > 0 {
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

	// 空白名单：默认拒绝内网/回环/链路本地/元数据地址
	if isInternalIP(ip) {
		return fmt.Errorf("SSRF 防护: 禁止访问内网/回环/链路本地地址 %s", ip.String())
	}
	return nil
}

// isInternalIP 判断 IP 是否为内网/回环/链路本地等危险目标
func isInternalIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		b := ip4
		return b[0] == 0 || // 0.0.0.0/8 本机
			b[0] == 10 || // 10/8 私网
			b[0] == 127 || // 127/8 回环
			(b[0] == 100 && b[1]&0xc0 == 0x40) || // 100.64/10 CGNAT
			(b[0] == 169 && b[1] == 254) || // 169.254/16 链路本地/云元数据
			(b[0] == 172 && b[1]&0xf0 == 16) || // 172.16/12 私网
			(b[0] == 192 && b[1] == 168) // 192.168/16 私网
	}
	return ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast()
}
