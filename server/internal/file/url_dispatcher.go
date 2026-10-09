package file

import (
	"sync/atomic"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/repositories"
	"fuzhan/internal/utils"
)

// URLDownloadDispatcher URL 上传（下载）任务的后台队列调度器。
// 全局单例：所有用户共享同一个并行上限，超出部分按入队顺序（created_at 升序）等待；
// 并行上限在每次调度时从配置读取，因此后台修改「并行上传数」无需重启即可生效。
// 任务持久化在 url_download_tasks 表，关闭浏览器甚至进程重启后仍可继续执行。
type URLDownloadDispatcher struct {
	repo     *repositories.URLDownloadTaskRepository
	handler  *UploadSessionHandler
	fallback int   // 配置缺失时的兜底并行数
	inFlight int32 // 当前执行中的任务数（原子计数）
	notify   chan struct{}
	stop     chan struct{}
}

// NewURLDownloadDispatcher 创建调度器。workers<=0 时回退为默认并行数。
func NewURLDownloadDispatcher(repo *repositories.URLDownloadTaskRepository, handler *UploadSessionHandler, workers int) *URLDownloadDispatcher {
	if workers <= 0 {
		workers = appconfig.DefaultURLUploadMaxConcurrent
	}
	return &URLDownloadDispatcher{
		repo:     repo,
		handler:  handler,
		fallback: workers,
		notify:   make(chan struct{}, 1),
		stop:     make(chan struct{}),
	}
}

// currentLimit 读取当前配置的全局并行上限（支持运行期修改）。
func (d *URLDownloadDispatcher) currentLimit() int {
	limit := appconfig.GlobalConfig.Upload.URLUpload.MaxConcurrent
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

// Stop 停止调度器
func (d *URLDownloadDispatcher) Stop() {
	close(d.stop)
}

// run 调度循环：启动即调度一次；此后由入队通知、任务完成通知或兜底定时器触发。
// 定时器保证后台调大并行上限后即使没有新事件也能在 1s 内生效。
func (d *URLDownloadDispatcher) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	d.dispatch()
	for {
		select {
		case <-d.stop:
			return
		case <-d.notify:
			d.dispatch()
		case <-ticker.C:
			d.dispatch()
		}
	}
}

// dispatch 在并行上限内尽可能多地领取任务并异步执行。
// 仅由 run 协程调用，故对 inFlight 的读改写不存在并发竞争。
func (d *URLDownloadDispatcher) dispatch() {
	limit := int32(d.currentLimit())
	for atomic.LoadInt32(&d.inFlight) < limit {
		task, err := d.repo.ClaimNextPending()
		if err != nil {
			utils.Warn("URL上传队列领取任务失败", utils.Err(err))
			return
		}
		if task == nil {
			return
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
}