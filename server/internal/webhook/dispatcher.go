package webhook

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"
)

// Dispatcher 进程内 Webhook 事件派发器。
// 采用「有界 channel + 单消费者 goroutine」轻量进程内消息队列：
//   - Send 非阻塞投递，队列满载时丢弃并记录告警日志，绝不阻塞业务请求；
//   - 消费者依次从队列取事件，发送前实时读取配置（支持热更新）；
//   - 生命周期与进程绑定：bootstrap 启动阶段 Start，进程退出时 Stop 排空已排队事件。
//     配置热重启不重建派发器，仅通过 Sync 同步启用开关。
type Dispatcher struct {
	ch      chan Event
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	once    sync.Once
	seq     atomic.Uint64
	enabled atomic.Bool
}

// webhookDrainTimeout 进程退出时等待队列排空的最长时间，超时后取消在途请求。
const webhookDrainTimeout = 5 * time.Second

// NewDispatcher 创建派发器。
// 队列容量取 max(config.QueueSize, 1)，同时记录是否启用，避免每次投递读配置加锁。
func NewDispatcher(cfg appconfig.WebhookConfig) *Dispatcher {
	queue := cfg.QueueSize
	if queue <= 0 {
		queue = 1024
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &Dispatcher{
		ch:     make(chan Event, queue),
		done:   make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
	d.enabled.Store(cfg.Enabled)
	// 事件权限为空表示不接收任何事件（默认拒绝）；启动时提示，避免端点静默失效
	for _, ep := range cfg.EffectiveEndpoints() {
		if ep.IsEnabled() && len(ep.Events) == 0 {
			utils.Warn("Webhook 端点未配置事件权限，将不接收任何事件",
				utils.String("name", ep.Name),
				utils.String("url", redactURL(ep.URL)))
		}
	}
	return d
}

// Enabled 返回当前是否启用 Webhook 推送（供触发点低成本判断，避免构造无用事件）。
func (d *Dispatcher) Enabled() bool {
	return d.enabled.Load()
}

// currentConfig 实时读取最新配置（支持配置热更新后立即生效）。
func (d *Dispatcher) currentConfig() appconfig.WebhookConfig {
	cfg := appconfig.GetWebhookConfig()
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 10
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	return cfg
}

// Send 将一条事件异步投入队列（非阻塞）。未启用/已停止/队列满载时返回 false。
// event 字段与时间戳由 Send 统一补全，调用方只需提供事件类型与负载。
func (d *Dispatcher) Send(eventType, source string, payload interface{}) bool {
	if !d.enabled.Load() {
		return false
	}
	ev := Event{
		ID:        d.seq.Add(1),
		Event:     eventType,
		Source:    source,
		Timestamp: time.Now(),
		Payload:   payload,
	}
	select {
	case d.ch <- ev:
		return true
	case <-d.done:
		return false
	default:
		utils.Warn("Webhook 事件队列满载，丢弃事件以避免背压",
			utils.String("event", eventType),
			utils.Int64("id", int64(ev.ID)))
		return false
	}
}

// subscribe 从队列取一条事件；未停止但通道已关闭或 Stop 完成时返回 false。
func (d *Dispatcher) subscribe(ev *Event) bool {
	select {
	case e, ok := <-d.ch:
		if !ok {
			return false
		}
		*ev = e
		return true
	case <-d.done:
		return false
	}
}

// Start 启动消费 goroutine（bootstrap 阶段调用一次；Stop 前常驻运行）。
func (d *Dispatcher) Start() {
	d.wg.Add(1)
	go d.run()
}

// run 消费循环：从队列取事件逐条发送；Stop 关闭 done 后先排空剩余事件再退出。
func (d *Dispatcher) run() {
	defer d.wg.Done()
	var ev Event
	for d.subscribe(&ev) {
		d.dispatch(ev)
	}
	// done 已关闭：排空已排队事件，尽量不丢事件
	for {
		select {
		case e, ok := <-d.ch:
			if !ok {
				return
			}
			d.dispatch(e)
		default:
			return
		}
	}
}

// dispatch 将单条事件发送到当前配置的全部启用端点。
// 逐端点独立构造 sender 以应用端点级 secret/headers/query 与超时配置；空 URL、
// 禁用或不匹配事件过滤规则的端点跳过。
func (d *Dispatcher) dispatch(ev Event) {
	cfg := d.currentConfig()
	if !cfg.Enabled {
		return // 派发期间被热更新关闭，直接丢弃本轮已取出的事件
	}
	ctx := d.ctx
	timeout := cfg.TimeoutSeconds
	maxRetries := cfg.MaxRetries
	// 每次构造 client 以实时读取超时配置（代价可忽略）
	for _, ep := range cfg.EffectiveEndpoints() {
		if !ep.IsEnabled() || strings.TrimSpace(ep.URL) == "" {
			continue
		}
		if !ep.MatchesEvent(ev.Event) {
			continue
		}
		epTimeout := ep.TimeoutSeconds
		if epTimeout <= 0 {
			epTimeout = timeout
		}
		epRetries := ep.MaxRetries
		if epRetries <= 0 {
			epRetries = maxRetries
		}
		client := &http.Client{Timeout: time.Duration(epTimeout) * time.Second}
		s := &sender{
			client:     client,
			secret:     cfg.Secret,
			maxRetries: epRetries,
			endpoint:   &ep,
		}
		if _, err := s.send(ctx, ep.URL, ev); err != nil {
			utils.Error("Webhook 推送最终失败",
				utils.String("url", redactURL(ep.URL)),
				utils.String("event", ev.Event),
				utils.Int64("id", int64(ev.ID)),
				utils.Err(err))
		}
	}
}

// Sync 应用配置热更新：重读启用开关（由保存后的 AddOnSaveHook 触发）。
func (d *Dispatcher) Sync(cfg appconfig.WebhookConfig) {
	d.enabled.Store(cfg.Enabled)
}

// Stop 停止派发：关闭投递口并等待已排队事件排空后退出（幂等，进程退出时调用）。
// 为避免关机被长时间阻塞（重试/超时叠加），排空最多等待 webhookDrainTimeout，
// 超时后取消在途请求并等待消费协程退出。
func (d *Dispatcher) Stop() {
	d.once.Do(func() { close(d.done) })
	finished := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(webhookDrainTimeout):
		utils.Warn("Webhook 派发器排空超时，取消在途发送",
			utils.Duration("timeout", webhookDrainTimeout))
		d.cancel()
		<-finished
	}
	d.cancel()
}
