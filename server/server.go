package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/index"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	temph "fuzhan/internal/temp"
	"fuzhan/internal/utils"
	"fuzhan/pkg/jwt"

	"github.com/robfig/cron/v3"
)

// runFTPWorker 运行 FTP 服务器，响应上下文取消时优雅关闭
func (rt *runCtx) runFTPWorker(ctx context.Context, ftpHandler interface {
	Start() error
	Stop()
}) {
	if err := ftpHandler.Start(); err != nil {
		utils.Warn("FTP服务器启动失败", utils.Err(err))
	}
	<-ctx.Done()
	ftpHandler.Stop()
}

// runCleanupWorker 清理过期上传会话、临时上传分片会话与过期临时文件（启动一次 + 每小时一次）
func (rt *runCtx) runCleanupWorker(ctx context.Context) {
	taskService := rt.taskService
	uploadSessionHandler := rt.uploadSessionHandler
	tempHandler := rt.tempHandler

	// 执行一次临时存储清理（上传会话、临时上传分片会话、过期临时文件），并写入任务记录
	runCleanupOnce := func(trigger string) {
		task, err := taskService.CreateTask("cleanup", "临时存储清理", trigger)
		if err != nil {
			utils.Warn("创建临时存储清理任务失败", utils.Err(err))
		} else {
			_ = taskService.StartTask(task.ID)
		}

		utils.Info("开始临时存储清理", utils.String("trigger", trigger))
		var cleaned []string
		if n, cerr := uploadSessionHandler.CleanupExpiredSessions(); cerr != nil {
			utils.Warn("清理过期上传会话失败", utils.Err(cerr))
		} else {
			cleaned = append(cleaned, fmt.Sprintf("清理上传会话 %d 个", n))
		}
		cleaned = append(cleaned, fmt.Sprintf("清理临时上传会话 %d 个", tempHandler.CleanupTempSessions()))
		cleaned = append(cleaned, fmt.Sprintf("清理临时文件 %d 个", tempHandler.CleanupExpiredFiles()))
		details := strings.Join(cleaned, "，")
		if task != nil {
			_ = taskService.UpdateTaskDetails(task.ID, details)
		}
		utils.Info("临时存储清理完成", utils.String("trigger", trigger))

		if task != nil {
			_ = taskService.CompleteTask(task.ID)
		}
	}

	// 启动时立即执行一次清理
	runCleanupOnce("startup")

	// 定时清理：每 1 小时一次
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			runCleanupOnce("timer")
		case <-ctx.Done():
			return
		}
	}
}

// runIndexScanWorker 启动时按 scope 检测索引表是否为空，为空则触发全量扫描
func (rt *runCtx) runIndexScanWorker(ctx context.Context) {
	// 启动时检测各 scope 的索引表是否为空，为空则触发全量扫描
	// 应用配置的扫描延迟，避免阻塞启动
	scanDelay := rt.cfg.Index.ScanStartDelaySeconds
	if scanDelay <= 0 {
		scanDelay = 10 // 默认延迟 10 秒
	}
	utils.Info("[启动] 等待索引扫描延迟",
		utils.Int("delay_seconds", scanDelay))

	// 等待延迟或上下文取消
	timer := time.NewTimer(time.Duration(scanDelay) * time.Second)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
		return
	}
	timer.Stop()

	utils.Info("[启动] 开始索引扫描检查")

	scopes := []struct {
		scope index.ScanScope
		name  string
	}{
		{index.ScanScopePublic, "公开文件"},
		{index.ScanScopeTemp, "临时文件"},
		{index.ScanScopePrivate, "私有文件"},
	}

	for _, s := range scopes {
		// 检查 scope 对应的功能是否已启用
		switch s.scope {
		case index.ScanScopeTemp:
			if !appconfig.GetConfig().Storage.Temp.Enabled {
				utils.Info("临时文件索引扫描已跳过（临时文件功能未启用）")
				continue
			}
		case index.ScanScopePrivate:
			if !appconfig.GetConfig().Storage.Private.Enabled {
				utils.Info("私有文件索引扫描已跳过（私有存储功能未启用）")
				continue
			}
		}
		// 检查扫描是否正在进行中
		progress := rt.indexService.GetScanProgressByScope(s.scope)
		if progress.Status == index.ScanStatusRunning {
			utils.Info(s.name + "索引扫描正在进行中, 跳过")
			continue
		}

		utils.Info(s.name + "索引表, 启动初始扫描")
		if err := rt.indexService.StartScanByScope(s.scope, "startup"); err != nil {
			utils.Error(s.name+"初始扫描启动失败", utils.Err(err))
		}
	}
}

// runScanTimerWorker 等待初始扫描完成后启动定时扫描与文件监听器
func (rt *runCtx) runScanTimerWorker(ctx context.Context) {
	// 等待初始扫描完成后再启动定时扫描
	pollTicker := time.NewTicker(5 * time.Second)
	defer pollTicker.Stop()

	for {
		progress := rt.indexService.GetScanProgress()
		if progress.Status == index.ScanStatusIdle ||
			progress.Status == index.ScanStatusCompleted ||
			progress.Status == index.ScanStatusFailed {
			break
		}
		select {
		case <-pollTicker.C:
			continue
		case <-ctx.Done():
			return
		}
	}

	rt.indexService.StartScanTimer()

	// 启动文件监听器（实时增量索引）
	rt.indexService.StartWatcher()

	// 注册配置变更回调（热生效：设置页面保存后自动重启定时器和监听器）
	appconfig.AddOnSaveHook(func() {
		rt.indexService.RestartScanTimer()
		rt.indexService.RestartWatcher()
	})

	<-ctx.Done()
	rt.indexService.StopScanTimer()
}

// runConsistencyCheckWorker 每 24 小时执行一次索引与文件系统的一致性校验
func (rt *runCtx) runConsistencyCheckWorker(ctx context.Context) {
	// 等待初始扫描完成后再启动一致性校验（不设超时）
	pollTicker := time.NewTicker(5 * time.Second)
	defer pollTicker.Stop()

	for {
		progress := rt.indexService.GetScanProgress()
		if progress.Status == index.ScanStatusIdle ||
			progress.Status == index.ScanStatusCompleted ||
			progress.Status == index.ScanStatusFailed {
			break
		}
		select {
		case <-pollTicker.C:
			continue
		case <-ctx.Done():
			return
		}
	}

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// 检查是否正在扫描, 如果是则跳过本次校验
			progress := rt.indexService.GetScanProgress()
			if progress.Status == index.ScanStatusRunning {
				utils.Info("一致性校验跳过: 全量扫描进行中")
				continue
			}
			utils.Info("开始定期一致性校验")
			if _, err := rt.indexService.RunConsistencyCheck(ctx); err != nil {
				utils.Error("一致性校验失败", utils.Err(err))
			}
		case <-ctx.Done():
			return
		}
	}
}

// runSearchReconcileWorker 按 cron 表达式定期对文件名检索索引执行对齐
func (rt *runCtx) runSearchReconcileWorker(ctx context.Context) {
	idxSearch := rt.idxSearch
	taskService := rt.taskService

	// 等待初始扫描完成后再启动检索索引对齐（依赖索引表已就绪）
	pollTicker := time.NewTicker(5 * time.Second)
	defer pollTicker.Stop()

	for {
		progress := rt.indexService.GetScanProgress()
		if progress.Status == index.ScanStatusIdle ||
			progress.Status == index.ScanStatusCompleted ||
			progress.Status == index.ScanStatusFailed {
			break
		}
		select {
		case <-pollTicker.C:
			continue
		case <-ctx.Done():
			return
		}
	}

	// 检索索引未打开时跳过（启动时初始化失败）
	if idxSearch == nil {
		utils.Info("检索索引未可用，检索索引对齐任务不启动")
		<-ctx.Done()
		return
	}

	// 执行一次检索索引对齐（默认每天 05:00，与定时扫描错开，可在设置页配置）
	getReconcileCron := func() string {
		expr := appconfig.GlobalConfig.Index.SearchReconcileCronExpression
		if expr == "" {
			return "0 5 * * *"
		}
		return expr
	}

	runReconcileOnce := func(trigger string) {
		// 全量扫描进行中时跳过，避免与扫描写盘竞争
		if p := rt.indexService.GetScanProgress(); p.Status == index.ScanStatusRunning {
			utils.Info("检索索引对齐跳过: 全量扫描进行中")
			return
		}
		task, err := taskService.CreateTask("search_reconcile", "检索索引对齐", "timer")
		if err != nil {
			utils.Warn("创建检索索引对齐任务失败", utils.Err(err))
			return
		}
		if err := taskService.StartTask(task.ID); err != nil {
			utils.Warn("启动检索索引对齐任务失败", utils.Err(err))
		}
		utils.Info("开始检索索引对齐", utils.String("trigger", trigger))
		res, rerr := idxSearch.ReconcileIndex(rt.db, func(done, total int64) {
			p := 0
			if total > 0 {
				p = int(done * 100 / total)
			}
			_ = taskService.UpdateTaskProgress(task.ID, p, done, total)
		})
		if rerr != nil {
			_ = taskService.FailTask(task.ID, rerr.Error())
			utils.Error("检索索引对齐失败", utils.Err(rerr))
			return
		}
		_ = taskService.UpdateTaskDetails(task.ID, fmt.Sprintf(
			"建立索引 %d，补充索引 %d，移除孤儿 %d", res.IndexedTotal, res.MissingIndexed, res.OrphansRemoved))
		_ = taskService.CompleteTask(task.ID)
		utils.Info("检索索引对齐完成",
			utils.Int64("indexed_total", res.IndexedTotal),
			utils.Int64("missing_indexed", res.MissingIndexed),
			utils.Int64("orphans_removed", res.OrphansRemoved))
	}

	// 定时循环（配置变更会触发 worker 热重启，从而重新读取 cron）
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	cronExpr := getReconcileCron()
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		utils.Warn("检索索引对齐 cron 表达式无效，使用默认值",
			utils.String("cron", cronExpr), utils.Err(err))
		cronExpr = "0 5 * * *"
		schedule, _ = parser.Parse(cronExpr)
	}
	nextTime := schedule.Next(time.Now())
	utils.Info("检索索引对齐下次执行时间",
		utils.String("cron", cronExpr),
		utils.String("next", nextTime.Format("2006-01-02 15:04:05")))

	for {
		select {
		case <-ctx.Done():
			utils.Info("检索索引对齐 worker 已停止")
			return
		default:
		}
		if !time.Now().Before(nextTime) {
			runReconcileOnce(cronExpr)
			nextTime = schedule.Next(time.Now())
			utils.Info("检索索引对齐下次执行时间",
				utils.String("next", nextTime.Format("2006-01-02 15:04:05")))
		}
		waitDuration := time.Until(nextTime)
		if waitDuration < 0 {
			waitDuration = 0
		}
		timer := time.NewTimer(min(waitDuration, 30*time.Second))
		select {
		case <-ctx.Done():
			timer.Stop()
			utils.Info("检索索引对齐 worker 已停止")
			return
		case <-timer.C:
		}
	}
}

// run 主循环：热重启驱动 Worker 生命周期，处理配置热更新与关闭信号
func (rt *runCtx) run() {
	// 配置变更通知通道（缓冲1，非阻塞发送）
	configCh := make(chan struct{}, 1)

	// 创建主上下文（用于 worker 生命周期管理）
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 注册热更新钩子（内联处理无需重启的逻辑，信号通知主循环重启 worker）
	appconfig.AddOnSaveHook(func() {
		// 1. JWT 密钥热更新（简单内联处理，无需重启）
		if s := appconfig.GlobalConfig.Server.JWTSecret; len(s) >= 32 && s != string(jwt.GetJWTSecret()) {
			jwt.UpdateJWTSecret([]byte(s))
			utils.Info("JWT密钥已热更新")
		}

		// 2. 日志配置热重载（简单内联处理，无需重启）
		if err := utils.ReinitLogger(appconfig.GlobalConfig.Log); err != nil {
			utils.Warn("日志热重载失败", utils.Err(err))
		} else {
			utils.Info("日志配置已热重载")
		}

		// 通知主循环处理需要重启的 worker
		select {
		case configCh <- struct{}{}:
		default:
		}
	})

	// 信号通道
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// workerManager 封装单轮迭代的 Worker 生命周期（每轮重建，避免跨迭代 WaitGroup 竞争）
	type workerManager struct {
		wg      sync.WaitGroup
		cancels map[string]context.CancelFunc
		mu      sync.Mutex
	}

	newWorkerManager := func() *workerManager {
		return &workerManager{cancels: make(map[string]context.CancelFunc)}
	}

	// start 在当前迭代中启动一个 worker goroutine
	workerStart := func(wm *workerManager, name string, fn func(ctx context.Context)) {
		workerCtx, workerCancel := context.WithCancel(ctx)
		wm.mu.Lock()
		wm.cancels[name] = workerCancel
		wm.mu.Unlock()
		wm.wg.Add(1)
		go func() {
			defer wm.wg.Done()
			defer func() {
				wm.mu.Lock()
				delete(wm.cancels, name)
				wm.mu.Unlock()
			}()
			fn(workerCtx)
		}()
	}

	// stopAllWorkers 取消当前迭代的所有 worker
	stopAllWorkers := func(wm *workerManager) {
		wm.mu.Lock()
		for name, cancel := range wm.cancels {
			cancel()
			delete(wm.cancels, name)
			utils.Info("已停止 worker", utils.String("name", name))
		}
		wm.mu.Unlock()
	}

	// waitWorkersWithTimeout 等待当前迭代的所有 worker 退出，超时则记录警告并继续
	waitWorkersWithTimeout := func(wm *workerManager) {
		c := make(chan struct{}, 1)
		go func() {
			wm.wg.Wait()
			c <- struct{}{}
		}()
		select {
		case <-c:
			utils.Info("所有 worker 已退出")
		case <-time.After(30 * time.Second):
			utils.Warn("等待 workers 超时 (30s)，强制继续")
		}
	}

	currentDbDriver := rt.cfg.Database.Driver
	currentDbDSN := rt.cfg.Database.DSN

	// 数据库热切换
	hotSwapDB := func() {
		dbCfg := appconfig.GetConfig().Database
		if dbCfg.Driver == currentDbDriver && dbCfg.DSN == currentDbDSN {
			return // 数据库配置未变更
		}
		utils.Info("数据库配置已变更，正在热切换...")
		oldDB := appconfig.GetDB()
		_, err := appconfig.InitDBWithAutoMigrate(
			&dbCfg,
			&models.UploadSession{}, &models.UploadedChunk{}, &models.OperationRecord{},
			&models.User{}, &models.TempFile{},
			&models.URLDownloadTask{},
			&models.FileRecordPublic{}, &models.FileRecordTemp{}, &models.FileRecordPrivate{},
			&temph.TempUploadSession{}, &temph.ChunkUploadRecord{},
			&models.FileDependency{},
			&models.ApiKey{},
			&models.OAuthClient{},
			&models.AuthRecord{},
			&models.ResourceMetric{},
		)
		if err != nil {
			utils.Error("数据库切换失败，保留原连接", utils.Err(err))
			return
		}
		// InitDBWithAutoMigrate 已调用 dbAtomic.Store(newDB)
		// 关闭旧连接
		if oldDB != nil {
			if sqlDB, err := oldDB.DB(); err == nil {
				sqlDB.Close()
			}
		}
		currentDbDriver = dbCfg.Driver
		currentDbDSN = dbCfg.DSN
		utils.Info("数据库已切换到新连接")
	}

	// Worker 热重启循环
	for {
		// 每轮迭代创建独立的 WorkerManager，防止跨迭代 WaitGroup 竞争
		wm := newWorkerManager()

		// 每次循环重新创建 Gin Engine 和 FTP Handler，确保配置变更后使用最新值
		r, ftpHandler := rt.newRouter()
		loopCfg := appconfig.GetConfig()
		address := fmt.Sprintf("%s:%d", loopCfg.Server.Host, loopCfg.Server.HTTP.Port)

		// HTTP 服务器 worker（需要捕获循环内的 r 和 address）
		runHTTPServer := func(workerCtx context.Context) {
			srv := &http.Server{
				Addr:    address,
				Handler: r,
			}
			go func() {
				<-workerCtx.Done()
				shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer shutdownCancel()
				srv.Shutdown(shutdownCtx)
			}()
			utils.Info("HTTP 服务器启动", utils.String("address", address))
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				utils.Error("HTTP 服务器启动失败，服务不可用", utils.String("address", address), utils.Err(err))
			}
		}

		// 启动所有 worker（使用当前迭代的 WorkerManager）
		workerStart(wm, "http", runHTTPServer)
		workerStart(wm, "ftp", func(ctx context.Context) { rt.runFTPWorker(ctx, ftpHandler) })
		workerStart(wm, "cleanup", rt.runCleanupWorker)
		workerStart(wm, "index-scan", rt.runIndexScanWorker)
		workerStart(wm, "scan-timer", rt.runScanTimerWorker)
		workerStart(wm, "consistency-check", rt.runConsistencyCheckWorker)
		workerStart(wm, "search-reconcile", rt.runSearchReconcileWorker)
		workerStart(wm, "resource-monitor", func(ctx context.Context) { rt.resourceCollector.Run(ctx) })

		// ===== 启动完成 =====
		utils.Info("========================================")
		utils.Info("[启动] 启动完成，服务已就绪")
		utils.Info("========================================")

		// 等待信号或配置变更
		select {
		case <-sigCh:
			utils.Info("正在关闭服务器...")
			cancel()
			stopAllWorkers(wm)
			waitWorkersWithTimeout(wm)
			if rt.idxSearch != nil {
				if cerr := rt.idxSearch.CloseWriter(); cerr != nil {
					utils.Warn("关闭文件名检索索引失败", utils.Err(cerr))
				}
			}
			// 停止有界记录器，排空并写完全部排队操作记录
			services.StopRecordWriter()
			utils.Info("服务器已关闭")
			return
		case <-configCh:
			utils.Info("检测到配置变更，正在热重启 worker...")
			stopAllWorkers(wm)
			waitWorkersWithTimeout(wm)
			hotSwapDB()
			// 检查重启期间是否收到关闭信号
			select {
			case <-sigCh:
				utils.Info("重启期间收到关闭信号，退出")
				cancel()
				return
			default:
			}
			utils.Info("所有 worker 已停止，正在启动新实例...")
			continue
		}
	}
}
