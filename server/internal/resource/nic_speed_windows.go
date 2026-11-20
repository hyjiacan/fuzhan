//go:build windows

package resource

import (
	"context"
	"unsafe"

	"fuzhan/internal/utils"
	"golang.org/x/sys/windows"
)

// nicLinkSpeeds 通过 IP Helper API（GetIfTable2Ex）读取各网卡的链路速率（Mbps）。
// MIB_IF_ROW2.Alias 与 Go 标准库 net.Interface.Name（网卡友好名）一致，可直接作为 map key。
// 纯系统调用，不启动外部进程，Windows 7 及以上均可用。
func nicLinkSpeeds(_ context.Context) map[string]int {
	result := map[string]int{}
	var table *windows.MibIfTable2
	if err := windows.GetIfTable2Ex(windows.MibIfTableNormal, &table); err != nil {
		utils.Warn("探测网卡速率失败", utils.Err(err))
		return result
	}
	if table == nil || table.NumEntries == 0 {
		return result
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))

	rows := unsafe.Slice(&table.Table[0], table.NumEntries)
	for i := range rows {
		row := &rows[i]
		// TransmitLinkSpeed 单位为 bps；网卡未连接时为 0，跳过
		if row.TransmitLinkSpeed == 0 {
			continue
		}
		name := windows.UTF16ToString(row.Alias[:])
		if name == "" {
			continue
		}
		result[name] = int(row.TransmitLinkSpeed / 1_000_000)
	}
	return result
}
