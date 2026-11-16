package models

import "time"

// ResourceMetric 服务器资源历史记录
// 按采集间隔落库（默认每分钟一条），历史仅保留 retention_days 天，超出部分定期清理。
// scope 取值：server（服务器整体）/ program（本程序）。
type ResourceMetric struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Timestamp   time.Time `gorm:"index:idx__resource_metrics__timestamp_scope" json:"timestamp"`
	Scope       string    `gorm:"size:16;index:idx__resource_metrics__timestamp_scope" json:"scope"`
	CPU         float64   `json:"cpu"`           // CPU 占用率（%）
	Memory      uint64    `json:"memory"`        // 内存已用（字节），程序级为进程 RSS
	MemoryTotal uint64    `json:"memory_total"`  // 内存总量（字节），程序级为服务器总量
	DiskUsed    uint64    `json:"disk_used"`     // 磁盘已用（字节）
	DiskTotal   uint64    `json:"disk_total"`    // 磁盘总量（字节）
	DiskIORead  float64   `json:"disk_io_read"`  // 磁盘读 IO（字节/秒）
	DiskIOWrite float64   `json:"disk_io_write"` // 磁盘写 IO（字节/秒）
	NetRecv     float64   `json:"net_recv"`      // 网络接收速率（字节/秒）
	NetSent     float64   `json:"net_sent"`      // 网络发送速率（字节/秒）
}

func (ResourceMetric) TableName() string {
	return "resource_metrics"
}
