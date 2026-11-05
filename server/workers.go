package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/index"
	"fuzhan/internal/utils"

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

// runCronJob 按 5 段 cron 表达式周期执行 fn，直到 ctx 结束。
// expr 为空或解析失败时回退 defaultExpr；每 30 秒唤醒一次以便及时响应停止信号。
func (rt *runCtx) runCronJob(ctx context.Context, name, expr, defaultExpr string, fn func()) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if strings.TrimSpace(expr) == "" {
		expr = defaultExpr
	}
	schedule, err := parser.Parse(expr)
	if err != nil {
		utils.Warn(name+" cron 表达式无效，回退默认值",
			utils.String("cron", expr), utils.String("default", defaultExpr), utils.Err(err))
		schedule, err = parser.Parse(defaultExpr)
		if err != nil {
			utils.Error(name+"默认 cron 表达式无效，作业不启动", utils.Err(err))
			return
		}
	}
	next := schedule.Next(time.Now())
	utils.Info(name+"下次执行时间",
		utils.String("cron", expr), utils.String("next", next.Format("2006-01-02 15:04:05")))

	for {
		wait := time.Until(next)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(min(wait, 30*time.Second))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if time.Now().Before(next) {
			continue
		}
		fn()
		next = schedule.Next(time.Now())
		utils.Info(name+"下次执行时间",
			utils.String("cron", expr), utils.String("next", next.Format("2006-01-02 15:04:05")))
	}
}

// runCleanupWorker 清理过期上传会话、临时上传分片会话与过期临时文件（启动时 + 按 cron 周期）
func (rt *runCtx) runCleanupWorker(ctx context.Context) {
	taskService := rt.taskService
	uploadSessionHandler := rt.uploadSessionHandler
	privateUploadHandler := rt.privateUploadHandler
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
			cleaned = append(cleaned, fmt.Sprintf("清理公开上传会话 %d 个", n))
		}
		cleaned = append(cleaned, fmt.Sprintf("清理私有上传会话 %d 个", privateUploadHandler.CleanupExpiredSessions()))
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

	// 启动时立即执行一次清理，之后按 cron 表达式周期执行（默认每小时整点）
	runCleanupOnce("startup")
	rt.runCronJob(ctx, "临时存储清理",
		appconfig.GlobalConfig.Index.CleanupCronExpression, "0 * * * *",
		func() { runCleanupOnce("timer") })
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

// runConsistencyCheckWorker 按 cron 表达式定期执行索引与文件系统的一致性校验
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

	rt.runCronJob(ctx, "索引一致性检查",
		appconfig.GlobalConfig.Index.ConsistencyCheckCronExpression, "0 3 * * *",
		func() {
			// 全量扫描进行中时跳过本次校验，避免与扫描写盘竞争
			if rt.indexService.GetScanProgress().Status == index.ScanStatusRunning {
				utils.Info("一致性校验跳过: 全量扫描进行中")
				return
			}
			utils.Info("开始定期一致性校验")
			if _, err := rt.indexService.RunConsistencyCheck(ctx); err != nil {
				utils.Error("一致性校验失败", utils.Err(err))
			}
		})
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
