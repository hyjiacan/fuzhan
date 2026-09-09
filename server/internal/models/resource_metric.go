package models

import "time"

// ResourceMetric 服务器资源历史记录
// 按采集间隔落库（默认每分钟一条），历史仅保留 retention_days 天，超出部分定期清理。
// scope 取值：server（服务器整体）/ program（本程序）。
type ResourceMetric struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Timestamp   time.Time `gorm:"index:idx__resource_metrics__timestamp_scope" json:"timestamp"`
	Scope       string    `gorm:"size:16;index:idx__resource_metrics__timestamp_scope" json:"scope"`
	CPU         float64   `json:"cpu"`         // CPU 占用率（%）
	Memory      uint64    `json:"memory"`      // 内存已用（字节），程序级为进程 RSS
	MemoryTotal uint64    `json:"memoryTotal"` // 内存总量（字节），程序级为服务器总量
	DiskUsed    uint64    `json:"diskUsed"`    // 磁盘已用（字节）
	DiskTotal   uint64    `json:"diskTotal"`   // 磁盘总量（字节）
	DiskIORead  float64   `json:"diskIoRead"`  // 磁盘读 IO（字节/秒）
	DiskIOWrite float64   `json:"diskIoWrite"` // 磁盘写 IO（字节/秒）
}

func (ResourceMetric) TableName() string {
	return "resource_metrics"
}
