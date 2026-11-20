//go:build linux

package resource

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// nicLinkSpeeds 读取 /sys/class/net/<iface>/speed 获取各网卡链路速率（Mbps）。
// 纯文件读取，不启动外部进程；未连接时 speed 可能为 -1，跳过。
func nicLinkSpeeds(_ context.Context) map[string]int {
	const sysfsNet = "/sys/class/net"
	result := map[string]int{}
	entries, err := os.ReadDir(sysfsNet)
	if err != nil {
		return result
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(sysfsNet, e.Name(), "speed"))
		if err != nil {
			continue
		}
		mbps, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil || mbps <= 0 {
			continue
		}
		result[e.Name()] = mbps
	}
	return result
}
