package file

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"
)

// URLDownloadDispatcher URL 上传（下载）任务的后台队列调度器。
// 全局单例：所有用户共享同一个并行上限，超出部分按优先级与入队顺序等待；
// 并行上限在每次调度时从配置读取，因此后台修改「并行上传数」无需重启即可生效。
// 任务持久化在 url_download_tasks 表，关闭浏览器甚至进程重启后仍可继续执行。
//
// 调度完全由事件驱动：新任务入队、槽位释放、配置保存都会 Notify 唤醒；
// 重试退避的到期则由 armTimer 按最近到期时刻设置一次性定时器精确唤醒，不做周期轮询。
type URLDownloadDispatcher struct {
	repo     *repositories.URLDownloadTaskRepository
	handler  *UploadSessionHandler
	fallback int   // 配置缺失时的兜底并行数
	inFlight int32 // 当前执行中的任务数（原子计数）
	notify   chan struct{}
	stop     chan struct{}
	stopOnce sync.Once

	// timer 退避到期的按需唤醒定时器，仅在 run 协程内读写。
	timer *time.Timer

	// cancels 记录执行中任务的取消函数，供「取消/删除任务」真正中断进行中的下载。
	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

// minWakeDelay 按需唤醒的最小延迟，避免到期时刻恰在当前时刻时造成忙转。
const minWakeDelay = 100 * time.Millisecond

// errRetryDelay 领取任务失败（多为数据库瞬时故障）后的重试延迟，避免任务停滞。
// 仅在出错时触发，不是周期轮询。
const errRetryDelay = 5 * time.Second

// NewURLDownloadDispatcher 创建调度器。workers<=0 时回退为默认并行数。
func NewURLDownloadDispatcher(repo *repositories.URLDownloadTaskRepository, handler *UploadSessionHandler, workers int) *URLDownloadDispatcher {
	if workers <= 0 {
		workers = appconfig.DefaultURLUploadMaxConcurrent
	}
	d := &URLDownloadDispatcher{
		repo:     repo,
		handler:  handler,
		fallback: workers,
		notify:   make(chan struct{}, 1),
		stop:     make(chan struct{}),
		cancels:  make(map[string]context.CancelFunc),
	}
	// 初始为一个已停止的定时器，首次调度时会由 armTimer 重置或保持停止。
	d.timer = time.NewTimer(time.Hour)
	d.timer.Stop()
	return d
}

// currentLimit 读取当前配置的全局并行上限（支持运行期修改）。
// 通过 GetConfig 加读锁读取，避免与配置热更新的写操作产生数据竞争。
func (d *URLDownloadDispatcher) currentLimit() int {
	limit := appconfig.GetConfig().Upload.URLUpload.MaxConcurrent
	if limit <= 0 {
		limit = d.fallback
	}
	if limit <= 0 {
		limit = appconfig.DefaultURLUploadMaxConcurrent
	}
	return limit
}

// Start 启动调度循环
func (d *URLDownloadDispatcher) Start() {
	go d.run()
	utils.Info("URL上传队列调度器已启动", utils.Int("workers", d.currentLimit()))
}

// Notify 通知调度器有新任务入队或槽位释放。非阻塞：信号满时丢弃，
// 调度循环每次处理会一次性领满并行额度，因此丢弃不会造成任务积压。
func (d *URLDownloadDispatcher) Notify() {
	select {
	case d.notify <- struct{}{}:
	default:
	}
}

// Stop 停止调度器（幂等，可安全重复调用）。
func (d *URLDownloadDispatcher) Stop() {
	d.stopOnce.Do(func() { close(d.stop) })
}

// RegisterCancel 登记某个执行中任务的取消函数，供外部取消/删除时中断下载。
func (d *URLDownloadDispatcher) RegisterCancel(taskID string, cancel context.CancelFunc) {
	d.cancelMu.Lock()
	d.cancels[taskID] = cancel
	d.cancelMu.Unlock()
}

// UnregisterCancel 注销任务的取消函数（worker 结束时调用）。
func (d *URLDownloadDispatcher) UnregisterCancel(taskID string) {
	d.cancelMu.Lock()
	delete(d.cancels, taskID)
	d.cancelMu.Unlock()
}

// CancelTask 中断指定任务的下载。任务未在执行中时返回 false（调用方据此决定是否清理临时文件）。
func (d *URLDownloadDispatcher) CancelTask(taskID string) bool {
	d.cancelMu.Lock()
	cancel, ok := d.cancels[taskID]
	d.cancelMu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// run 调度循环：启动即调度一次；此后由入队通知、任务完成通知、配置保存通知
// 或退避到期定时器触发。不再使用周期轮询。
func (d *URLDownloadDispatcher) run() {
	d.dispatch()
	for {
		select {
		case <-d.stop:
			return
		case <-d.notify:
			d.dispatch()
		case <-d.timer.C:
			d.dispatch()
		}
	}
}

// dispatch 在并行上限内尽可能多地领取任务并异步执行。
// 仅由 run 协程调用，故对 inFlight 的读改写不存在并发竞争。
func (d *URLDownloadDispatcher) dispatch() {
	limit := int32(d.currentLimit())
	claimFailed := false
	for atomic.LoadInt32(&d.inFlight) < limit {
		task, err := d.repo.ClaimNextPending()
		if err != nil {
			utils.Warn("URL上传队列领取任务失败", utils.Err(err))
			claimFailed = true
			break
		}
		if task == nil {
			break
		}
		atomic.AddInt32(&d.inFlight, 1)
		go func(t *models.URLDownloadTask) {
			defer func() {
				atomic.AddInt32(&d.inFlight, -1)
				d.Notify() // 槽位释放，继续调度后续任务
			}()
			d.handler.executeURLDownloadTask(t)
		}(task)
	}
	if claimFailed {
		// 领取失败：短暂延后重试，避免数据库瞬时故障导致任务永久停滞
		d.resetTimer(errRetryDelay)
		return
	}
	d.armTimer()
}

// armTimer 决定下一次按需唤醒。
// 仅在「取不出任务」时，按最近未到期任务的入队时间设置一次性定时器；
// 若刚才是因并行额度已满而停止，则不需要定时器（槽位释放会 Notify）。
// 关键点：「取不出」不等于「没有任务」——退避中的任务需按到期时刻精确唤醒，否则将永远等待。
func (d *URLDownloadDispatcher) armTimer() {
	// 额度已满：等槽位释放的 Notify 即可
	if atomic.LoadInt32(&d.inFlight) >= int32(d.currentLimit()) {
		d.stopTimer()
		return
	}
	dueAt, ok, err := d.repo.NextPendingQueuedAt()
	if err != nil {
		utils.Warn("URL上传队列查询下一任务到期时间失败", utils.Err(err))
		d.resetTimer(errRetryDelay)
		return
	}
	if !ok {
		// 队列确实为空：等待事件（入队 / 重试 / 配置保存）
		d.stopTimer()
		return
	}
	delay := time.Until(dueAt)
	if delay < minWakeDelay {
		delay = minWakeDelay
	}
	d.resetTimer(delay)
}

// resetTimer 安全重置按需唤醒定时器（仅 run 协程调用）。
func (d *URLDownloadDispatcher) resetTimer(delay time.Duration) {
	if !d.timer.Stop() {
		select {
		case <-d.timer.C:
		default:
		}
	}
	d.timer.Reset(delay)
}

// stopTimer 停止按需唤醒定时器（仅 run 协程调用）。
func (d *URLDownloadDispatcher) stopTimer() {
	if !d.timer.Stop() {
		select {
		case <-d.timer.C:
		default:
		}
	}
}
