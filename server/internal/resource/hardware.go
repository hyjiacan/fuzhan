package resource

import (
	"context"
	"strings"

	"fuzhan/internal/utils"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
)

// collectHardware 采集服务器硬件配置（CPU/内存/磁盘/网卡）。
// 该信息变化频率极低，由采集器低频刷新并缓存，避免每次采样都做昂贵的系统调用。
func collectHardware(ctx context.Context) Hardware {
	hw := Hardware{Disks: []DiskInfo{}, NICs: []NICInfo{}}

	if info, err := host.Info(); err == nil {
		hw.Hostname = info.Hostname
		hw.OS = strings.TrimSpace(strings.Join([]string{info.Platform, info.PlatformVersion}, " "))
		if hw.OS == "" {
			hw.OS = info.OS
		}
		hw.Arch = info.KernelArch
		hw.Kernel = info.KernelVersion
	} else {
		utils.Warn("采集主机信息失败", utils.Err(err))
	}

	hw.CPU = collectCPUInfo()
	hw.Memory = collectMemoryInfo()
	hw.Disks, hw.DiskTotal = collectDiskInfo()
	hw.NICs = collectNICInfo(ctx)
	return hw
}

// collectCPUInfo 采集 CPU 型号、核心数、主频与缓存
func collectCPUInfo() CPUInfo {
	info := CPUInfo{}
	// 物理核心数与逻辑线程数：Info() 在部分平台 Cores 为 0，优先用 Counts 保证准确
	if n, err := cpu.Counts(false); err == nil {
		info.Cores = n
	}
	if n, err := cpu.Counts(true); err == nil {
		info.Threads = n
	}
	stats, err := cpu.Info()
	if err != nil || len(stats) == 0 {
		if err != nil {
			utils.Warn("采集 CPU 信息失败", utils.Err(err))
		}
		return info
	}
	// 多路 CPU 时 Info() 按物理 CPU 返回多条，型号取首条，主频取最大值，缓存累加
	for _, s := range stats {
		if info.Model == "" && s.ModelName != "" {
			info.Model = strings.TrimSpace(s.ModelName)
		}
		if s.Mhz > info.FreqMHz {
			info.FreqMHz = s.Mhz
		}
		info.CacheSize += s.CacheSize
	}
	if info.Cores == 0 && stats[0].Cores > 0 {
		info.Cores = int(stats[0].Cores) * len(stats)
	}
	if info.Threads == 0 && info.Cores > 0 {
		info.Threads = info.Cores
	}
	return info
}

// collectMemoryInfo 采集内存与交换分区容量
func collectMemoryInfo() MemoryInfo {
	info := MemoryInfo{}
	vm, err := mem.VirtualMemory()
	if err != nil {
		utils.Warn("采集内存信息失败", utils.Err(err))
	} else {
		info.Total = vm.Total
		info.Available = vm.Available
	}
	if sw, err := mem.SwapMemory(); err == nil {
		info.SwapTotal = sw.Total
	}
	return info
}

// collectDiskInfo 采集磁盘（分区）数量、容量与总容量合计。
// 复用 skipFSType 过滤伪文件系统，并按设备去重（同一设备多挂载点只计一次容量）。
func collectDiskInfo() ([]DiskInfo, uint64) {
	parts, err := disk.Partitions(false)
	if err != nil {
		utils.Warn("采集磁盘分区信息失败", utils.Err(err))
		return []DiskInfo{}, 0
	}
	list := make([]DiskInfo, 0, len(parts))
	seen := make(map[string]bool)
	var total uint64
	for _, p := range parts {
		if skipFSType[p.Fstype] || p.Mountpoint == "" {
			continue
		}
		if seen[p.Device] {
			continue
		}
		seen[p.Device] = true
		item := DiskInfo{Device: p.Device, Mountpoint: p.Mountpoint, FSType: p.Fstype}
		if u, err := disk.Usage(p.Mountpoint); err == nil {
			item.Total = u.Total
			item.Free = u.Free
			total += u.Total
		}
		list = append(list, item)
	}
	return list, total
}

// ListPartitions 实时获取磁盘分区列表（含总容量与剩余容量），供设置页分区下拉使用。
func ListPartitions() []DiskInfo {
	list, _ := collectDiskInfo()
	return list
}

// collectNICInfo 采集物理网卡列表（名称、MAC、MTU、链路速率）。
// 跳过回环网卡与无 MAC 的虚拟接口，避免列表被无意义条目占满。
func collectNICInfo(ctx context.Context) []NICInfo {
	ifaces, err := gnet.Interfaces()
	if err != nil {
		utils.Warn("采集网卡信息失败", utils.Err(err))
		return []NICInfo{}
	}
	speeds := nicLinkSpeeds(ctx)
	list := make([]NICInfo, 0, len(ifaces))
	for _, iface := range ifaces {
		if isLoopbackFlags(iface.Flags) || iface.HardwareAddr == "" {
			continue
		}
		list = append(list, NICInfo{
			Name:      iface.Name,
			MAC:       iface.HardwareAddr,
			MTU:       iface.MTU,
			SpeedMbps: speeds[iface.Name],
		})
	}
	return list
}

// isLoopbackFlags 判断网卡标志中是否包含回环
func isLoopbackFlags(flags []string) bool {
	for _, f := range flags {
		if f == "loopback" {
			return true
		}
	}
	return false
}