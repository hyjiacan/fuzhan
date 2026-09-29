// Package resource 提供服务器资源监控：服务器整体与本程序两层面的
// CPU、内存、磁盘占用、磁盘 IO、网络流量的实时与历史（分钟级）数据采集与统计。
package resource

const (
	// ScopeServer 服务器整体资源
	ScopeServer = "server"
	// ScopeProgram 本程序资源
	ScopeProgram = "program"
)

// Point 单个时刻的资源快照指标
type Point struct {
	CPU         float64 `json:"cpu"`         // CPU 占用率（%）
	Memory      uint64  `json:"memory"`      // 内存已用（字节），程序级为进程 RSS
	MemoryTotal uint64  `json:"memoryTotal"` // 内存总量（字节）
	DiskUsed    uint64  `json:"diskUsed"`    // 磁盘已用（字节）
	DiskTotal   uint64  `json:"diskTotal"`   // 磁盘总量（字节）
	DiskIORead  float64 `json:"diskIoRead"`  // 磁盘读 IO（字节/秒）
	DiskIOWrite float64 `json:"diskIoWrite"` // 磁盘写 IO（字节/秒）
	NetRecv     float64 `json:"netRecv"`     // 网络接收速率（字节/秒），服务器为全部物理网卡合计
	NetSent     float64 `json:"netSent"`     // 网络发送速率（字节/秒），服务器为全部物理网卡合计
}

// ScopePoint 带时间戳与 scope 的资源采样点
type ScopePoint struct {
	Timestamp int64  `json:"timestamp"`
	Scope     string `json:"scope"`
	Point
}

// CPUInfo 服务器 CPU 配置
type CPUInfo struct {
	Model     string  `json:"model"`     // 型号名称
	Cores     int     `json:"cores"`     // 物理核心数
	Threads   int     `json:"threads"`   // 逻辑核心（线程）数
	FreqMHz   float64 `json:"freqMhz"`   // 主频（MHz）
	CacheSize int32   `json:"cacheSize"` // 缓存大小（KB）
}

// MemoryInfo 服务器内存配置
type MemoryInfo struct {
	Total     uint64 `json:"total"`     // 总容量（字节）
	Available uint64 `json:"available"` // 可用容量（字节）
	SwapTotal uint64 `json:"swapTotal"` // 交换分区总容量（字节）
}

// DiskInfo 单块磁盘（分区）配置
type DiskInfo struct {
	Device     string `json:"device"`     // 设备名 / 盘符
	Mountpoint string `json:"mountpoint"` // 挂载点
	FSType     string `json:"fsType"`     // 文件系统类型
	Total      uint64 `json:"total"`      // 总容量（字节）
}

// NICInfo 单块网卡配置
type NICInfo struct {
	Name      string `json:"name"`      // 网卡名称
	MAC       string `json:"mac"`       // MAC 地址
	MTU       int    `json:"mtu"`       // 最大传输单元
	SpeedMbps int    `json:"speedMbps"` // 链路速率（Mbps），无法探测时为 0
}

// Hardware 服务器硬件配置信息（变化频率低，采集侧低频刷新并缓存）
type Hardware struct {
	Hostname  string     `json:"hostname"`  // 主机名
	OS        string     `json:"os"`        // 操作系统
	Arch      string     `json:"arch"`      // 架构
	Kernel    string     `json:"kernel"`    // 内核版本
	CPU       CPUInfo    `json:"cpu"`       // CPU 配置
	Memory    MemoryInfo `json:"memory"`    // 内存配置
	Disks     []DiskInfo `json:"disks"`     // 磁盘（分区）列表
	DiskTotal uint64     `json:"diskTotal"` // 磁盘总容量合计（字节）
	NICs      []NICInfo  `json:"nics"`      // 网卡列表
}

// NICRate 单块网卡的实时流量速率
type NICRate struct {
	Name string  `json:"name"` // 网卡名称
	Recv float64 `json:"recv"` // 接收速率（字节/秒）
	Sent float64 `json:"sent"` // 发送速率（字节/秒）
}

// Realtime 实时接口返回体：硬件配置 + 当前快照 + 最近一段内存采样曲线
type Realtime struct {
	// 程序是否正在运行（采集可用）
	Running bool `json:"running"`
	// 最近采样间隔（秒），供前端决定刷新频率
	SamplingInterval int          `json:"samplingInterval"`
	Hardware         Hardware     `json:"hardware"`
	Server           Point        `json:"server"`
	Program          Point        `json:"program"`
	ServerRecent     []ScopePoint `json:"serverRecent"`
	ProgramRecent    []ScopePoint `json:"programRecent"`
	// NetNICs 各网卡当前流量速率（仅实时，不落库），用于网络流量卡片明细
	NetNICs []NICRate `json:"netNICs"`
}
