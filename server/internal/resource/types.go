// Package resource 提供服务器资源监控：服务器整体与本程序两层面的
// CPU、内存、磁盘占用、磁盘 IO 的实时与历史（分钟级）数据采集与统计。
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
}

// ScopePoint 带时间戳与 scope 的资源采样点
type ScopePoint struct {
	Timestamp int64  `json:"timestamp"`
	Scope     string `json:"scope"`
	Point
}

// Realtime 实时接口返回体：当前快照 + 最近一段内存采样曲线
type Realtime struct {
	// 程序是否正在运行（采集可用）
	Running bool `json:"running"`
	// 最近采样间隔（秒），供前端决定刷新频率
	SamplingInterval int          `json:"samplingInterval"`
	Server           Point        `json:"server"`
	Program          Point        `json:"program"`
	ServerRecent     []ScopePoint `json:"serverRecent"`
	ProgramRecent    []ScopePoint `json:"programRecent"`
}
