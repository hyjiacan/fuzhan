package resource

import (
	"context"
	"os"
	"sync"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/utils"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"gorm.io/gorm"
)

const (
	defaultSamplingInterval = 5  // 内存实时采样间隔（秒），不落库
	defaultCollectInterval  = 60 // 历史采集间隔（秒），默认每分钟一条
	defaultRetentionDays    = 7  // 历史数据保留天数

	// maxBufferPoints 每个 scope 内存中保留的最大采样点（5 秒间隔约 1 小时），仅用于实时曲线
	maxBufferPoints = 720
	// footprintRefreshInterval 程序托管存储占用刷新间隔（秒），依赖索引表求和，采集节奏刷新即可
	footprintRefreshInterval = 60
	// retentionCheckInterval 历史数据清理检查间隔
	retentionCheckInterval = 1 * time.Hour
)

// skipFSType 服务器整体磁盘统计时跳过的伪文件系统类型（Linux）
var skipFSType = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "proc": true, "sysfs": true,
	"devpts": true, "cgroup": true, "cgroup2": true, "overlay": true,
	"autofs": true, "binfmt_misc": true, "securityfs": true, "squashfs": true,
	"ramfs": true, "configfs": true, "debugfs": true, "mqueue": true,
	"sockfs": true, "nsfs": true, "hugetlbfs": true, "pstore": true,
	"fusectl": true, "tracefs": true, "bpf": true, "rpc_pipefs": true,
	"nfsd": true,
}

// NewCollector 创建资源采集器。
// getDB 返回当前活跃的数据库连接（热切换后仍指向最新连接）。
func NewCollector(getDB func() *gorm.DB) *Collector {
	pid := int32(os.Getpid())
	proc, err := process.NewProcess(pid)
	if err != nil {
		utils.Warn("获取当前进程句柄失败，程序级资源不可用", utils.Int("pid", int(pid)), utils.Err(err))
	}
	return &Collector{
		getDB: getDB,
		pid:   pid,
		proc:  proc,
		buffer: map[string][]ScopePoint{
			ScopeServer:  {},
			ScopeProgram: {},
		},
		lastSample: time.Now(),
	}
}

// Collector 资源采集器：负责内存实时采样、按采集间隔落库历史、定期清理过期历史。
type Collector struct {
	getDB func() *gorm.DB
	pid   int32
	proc  *process.Process

	mu     sync.RWMutex
	buffer map[string][]ScopePoint
	server Point
	prog   Point

	prevServerCPU      []cpu.TimesStat
	prevServerDiskIO   map[string]disk.IOCountersStat
	prevProgDiskIO     *process.IOCountersStat
	lastSample         time.Time
	lastCollect        time.Time
	lastFootprint      time.Time
	programFootprint   uint64
	programVolumeTotal uint64
}

// resolveConfig 读取配置；未配置时使用默认值
func (c *Collector) resolveConfig() (enabled bool, sampling, collect, retention int) {
	cfg := appconfig.GetConfig().Resource
	enabled = cfg.Enabled
	sampling = cfg.SamplingInterval
	collect = cfg.CollectInterval
	retention = cfg.RetentionDays
	if sampling <= 0 {
		sampling = defaultSamplingInterval
	}
	if collect <= 0 {
		collect = defaultCollectInterval
	}
	if collect < sampling {
		collect = sampling
	}
	if retention <= 0 {
		retention = defaultRetentionDays
	}
	return
}

// Run 采集主循环（作为 worker 运行，配置热变更触发热重启后重新读取配置）
func (c *Collector) Run(ctx context.Context) {
	enabled, sampling, collect, retention := c.resolveConfig()
	if !enabled {
		utils.Warn("服务器资源监控功能未启用（resource.enabled=false）")
		return
	}

	c.lastCollect = time.Now().Add(-time.Duration(collect) * time.Second)
	lastCleanup := time.Now()
	c.cleanup(retention)

	utils.Info("服务器资源监控已启动",
		utils.Int("sampling_seconds", sampling),
		utils.Int("collect_seconds", collect),
		utils.Int("retention_days", retention))

	ticker := time.NewTicker(time.Duration(sampling) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			utils.Info("服务器资源监控 worker 已停止")
			return
		case now := <-ticker.C:
			c.sample(now)
			if int(now.Sub(c.lastCollect).Seconds()) >= collect {
				c.persist(now)
				c.lastCollect = now
			}
			if now.Sub(lastCleanup) >= retentionCheckInterval {
				c.cleanup(retention)
				lastCleanup = now
			}
		}
	}
}

// sample 采集一次服务器与程序的资源快照，存入内存缓冲，供实时接口读取
func (c *Collector) sample(now time.Time) {
	server := Point{}
	prog := Point{}

	// 服务器 CPU（基于两次采样差值计算）
	if cur, err := cpu.Times(false); err == nil {
		server.CPU = cpuPercent(c.prevServerCPU, cur)
		c.prevServerCPU = cur
	}

	// 内存
	if vm, err := mem.VirtualMemory(); err == nil {
		server.Memory = vm.Used
		server.MemoryTotal = vm.Total
		prog.MemoryTotal = vm.Total
	}

	// 服务器磁盘：整体占用
	server.DiskUsed, server.DiskTotal = c.serverDiskUsage()

	// 服务器磁盘 IO
	if io, err := disk.IOCounters(); err == nil {
		server.DiskIORead, server.DiskIOWrite = c.ioRates(c.prevServerDiskIO, io, now)
		c.prevServerDiskIO = io
	} else {
		server.DiskIORead, server.DiskIOWrite = 0, 0
	}

	// 程序资源
	if c.proc != nil {
		if p, err := c.proc.CPUPercent(); err == nil {
			prog.CPU = p
		}
		if mi, err := c.proc.MemoryInfo(); err == nil {
			prog.Memory = mi.RSS
		}
		if io, err := c.proc.IOCounters(); err == nil {
			prog.DiskIORead, prog.DiskIOWrite = c.procIORates(c.prevProgDiskIO, io, now)
			c.prevProgDiskIO = io
		}
	}

	// 程序磁盘占用：托管存储占用按采集节奏刷新，避免高频查询索引表
	if now.Sub(c.lastFootprint) >= time.Duration(footprintRefreshInterval)*time.Second {
		c.programFootprint = c.queryFootprint()
		c.lastFootprint = now
	}
	prog.DiskUsed = c.programFootprint
	if c.programVolumeTotal == 0 {
		c.programVolumeTotal = programVolumeTotal()
	}
	prog.DiskTotal = c.programVolumeTotal

	c.setSnapshot(ScopeServer, server)
	c.setSnapshot(ScopeProgram, prog)
	c.push(ScopeServer, server, now)
	c.push(ScopeProgram, prog, now)
	c.lastSample = now
}

// setSnapshot 更新某 scope 的最新快照
func (c *Collector) setSnapshot(scope string, p Point) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if scope == ScopeServer {
		c.server = p
	} else {
		c.prog = p
	}
}

// push 将采样点写入内存环形缓冲（每个 scope 独立）
func (c *Collector) push(scope string, p Point, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lst := c.buffer[scope]
	lst = append(lst, ScopePoint{Timestamp: now.UnixMilli(), Scope: scope, Point: p})
	if len(lst) > maxBufferPoints {
		lst = lst[len(lst)-maxBufferPoints:]
	}
	c.buffer[scope] = lst
}

// Snapshot 返回实时快照（当前值 + 最近内存曲线）
func (c *Collector) Snapshot() Realtime {
	c.mu.RLock()
	defer c.mu.RUnlock()
	rt := Realtime{
		Running:          c.proc != nil,
		SamplingInterval: c.currentSampling(),
		Server:           c.server,
		Program:          c.prog,
		ServerRecent:     append([]ScopePoint(nil), c.buffer[ScopeServer]...),
		ProgramRecent:    append([]ScopePoint(nil), c.buffer[ScopeProgram]...),
	}
	return rt
}

func (c *Collector) currentSampling() int {
	cfg := appconfig.GetConfig().Resource
	if cfg.SamplingInterval > 0 {
		return cfg.SamplingInterval
	}
	return defaultSamplingInterval
}

// History 查询某 scope 从 from 到 to 的历史数据（按时间升序）
func (c *Collector) History(scope string, from, to time.Time) ([]ScopePoint, error) {
	db := c.getDB()
	if db == nil {
		return []ScopePoint{}, nil
	}
	var rows []models.ResourceMetric
	if err := db.Where("scope = ? AND timestamp >= ? AND timestamp <= ?", scope, from, to).
		Order("timestamp ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	pts := make([]ScopePoint, 0, len(rows))
	for _, r := range rows {
		pts = append(pts, ScopePoint{
			Timestamp: r.Timestamp.UnixMilli(),
			Scope:     r.Scope,
			Point: Point{
				CPU:         r.CPU,
				Memory:      r.Memory,
				MemoryTotal: r.MemoryTotal,
				DiskUsed:    r.DiskUsed,
				DiskTotal:   r.DiskTotal,
				DiskIORead:  r.DiskIORead,
				DiskIOWrite: r.DiskIOWrite,
			},
		})
	}
	return pts, nil
}

// persist 将当前快照按采集间隔落库（以分钟对齐，幂等）
func (c *Collector) persist(now time.Time) {
	db := c.getDB()
	if db == nil {
		return
	}
	ts := now.Truncate(time.Minute)
	c.mu.RLock()
	snapshots := []struct {
		scope string
		p     Point
	}{
		{ScopeServer, c.server},
		{ScopeProgram, c.prog},
	}
	c.mu.RUnlock()

	for _, s := range snapshots {
		// 幂等：同一分钟同一 scope 只保留最新一条
		if err := db.Where("timestamp = ? AND scope = ?", ts, s.scope).
			Delete(&models.ResourceMetric{}).Error; err != nil {
			utils.Warn("清理资源监控同分钟重复记录失败", utils.Err(err))
		}
		rec := &models.ResourceMetric{
			Timestamp:   ts,
			Scope:       s.scope,
			CPU:         s.p.CPU,
			Memory:      s.p.Memory,
			MemoryTotal: s.p.MemoryTotal,
			DiskUsed:    s.p.DiskUsed,
			DiskTotal:   s.p.DiskTotal,
			DiskIORead:  s.p.DiskIORead,
			DiskIOWrite: s.p.DiskIOWrite,
		}
		if err := db.Create(rec).Error; err != nil {
			utils.Warn("写入资源监控历史失败", utils.String("scope", s.scope), utils.Err(err))
		}
	}
}

// cleanup 清理超过保留天数的历史数据
func (c *Collector) cleanup(retentionDays int) {
	db := c.getDB()
	if db == nil {
		return
	}
	cutoff := utils.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	res := db.Where("timestamp < ?", cutoff).Delete(&models.ResourceMetric{})
	if res.Error != nil {
		utils.Warn("清理资源监控历史数据失败", utils.Err(res.Error))
	} else if res.RowsAffected > 0 {
		utils.Info("已清理资源监控历史数据", utils.Int64("rows", res.RowsAffected))
	}
}

// queryFootprint 查询程序托管存储占用（三张索引表 active 记录的文件大小之和）
func (c *Collector) queryFootprint() uint64 {
	db := c.getDB()
	if db == nil {
		return 0
	}
	var total uint64
	for _, table := range []string{"file_records_public", "file_records_temp", "file_records_private"} {
		var s int64
		if err := db.Raw("SELECT COALESCE(SUM(file_size),0) FROM "+table+
			" WHERE status = ? AND deleted_at IS NULL", models.FileStatusActive).Scan(&s).Error; err != nil {
			utils.Warn("查询程序托管存储占用失败", utils.String("table", table), utils.Err(err))
			continue
		}
		total += uint64(s)
	}
	return total
}

// serverDiskUsage 汇总服务器整体磁盘占用（所有本地分区；按设备去重）
func (c *Collector) serverDiskUsage() (used, total uint64) {
	parts, err := disk.Partitions(false)
	if err != nil {
		return 0, 0
	}
	seen := make(map[string]bool)
	for _, p := range parts {
		if skipFSType[p.Fstype] {
			continue
		}
		if seen[p.Device] {
			continue
		}
		seen[p.Device] = true
		if u, err := disk.Usage(p.Mountpoint); err == nil {
			used += u.Used
			total += u.Total
		}
	}
	return used, total
}

// programVolumeTotal 程序磁盘总量：数据目录所在卷的容量
func programVolumeTotal() uint64 {
	dataDir := appconfig.GetDataDir()
	u, err := disk.Usage(dataDir)
	if err != nil {
		return 0
	}
	return u.Total
}

// ioRates 计算服务器磁盘 IO 速率（字节/秒）
func (c *Collector) ioRates(prev map[string]disk.IOCountersStat, cur map[string]disk.IOCountersStat, now time.Time) (r, w float64) {
	elapsed := now.Sub(c.lastSample).Seconds()
	if prev == nil || elapsed <= 0 {
		return 0, 0
	}
	for name, s := range cur {
		p, ok := prev[name]
		if !ok {
			continue
		}
		if s.ReadBytes >= p.ReadBytes {
			r += float64(s.ReadBytes-p.ReadBytes) / elapsed
		}
		if s.WriteBytes >= p.WriteBytes {
			w += float64(s.WriteBytes-p.WriteBytes) / elapsed
		}
	}
	return r, w
}

// procIORates 计算进程磁盘 IO 速率（字节/秒）
func (c *Collector) procIORates(prev *process.IOCountersStat, cur *process.IOCountersStat, now time.Time) (r, w float64) {
	if prev == nil {
		return 0, 0
	}
	elapsed := now.Sub(c.lastSample).Seconds()
	if elapsed <= 0 {
		return 0, 0
	}
	if cur.ReadBytes >= prev.ReadBytes {
		r = float64(cur.ReadBytes-prev.ReadBytes) / elapsed
	}
	if cur.WriteBytes >= prev.WriteBytes {
		w = float64(cur.WriteBytes-prev.WriteBytes) / elapsed
	}
	return r, w
}

// cpuPercent 基于两次 CPU 时钟采样计算 CPU 占用率（%）
func cpuPercent(prev, cur []cpu.TimesStat) float64 {
	if len(prev) == 0 || len(cur) == 0 {
		return 0
	}
	sum := func(ts cpu.TimesStat) float64 {
		return ts.User + ts.System + ts.Nice + ts.Idle + ts.Iowait +
			ts.Irq + ts.Softirq + ts.Steal + ts.Guest + ts.GuestNice
	}
	dAll := sum(cur[0]) - sum(prev[0])
	if dAll <= 0 {
		return 0
	}
	dIdle := (cur[0].Idle + cur[0].Iowait) - (prev[0].Idle + prev[0].Iowait)
	p := (1 - dIdle/dAll) * 100
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return p
}
