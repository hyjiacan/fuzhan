package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/models"
	"fuzhan/internal/security"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"
	"fuzhan/pkg/jwt"
)

// workerShutdownTimeout 关闭/热重启时等待 worker 退出的上限，超时后打印堆栈快照并强制继续
const workerShutdownTimeout = 30 * time.Second

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

	// stopAllWorkers 取消当前迭代的所有 worker。
	// 不在此处删除 cancels 条目：worker 退出时会自行删除，因此该 map 始终反映"仍在运行"的 worker，
	// 超时诊断时可直接据此列出未退出的 worker。
	stopAllWorkers := func(wm *workerManager) {
		wm.mu.Lock()
		for name, cancel := range wm.cancels {
			cancel()
			utils.Info("已停止 worker", utils.String("name", name))
		}
		wm.mu.Unlock()
	}

	// waitWorkersWithTimeout 等待当前迭代的所有 worker 退出；
	// 超时则打印仍未退出的 worker 名单与全量 goroutine 堆栈快照，便于定位阻塞点，然后强制继续
	waitWorkersWithTimeout := func(wm *workerManager) {
		done := make(chan struct{})
		go func() {
			wm.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			utils.Info("所有 worker 已退出")
		case <-time.After(workerShutdownTimeout):
			wm.mu.Lock()
			names := make([]string, 0, len(wm.cancels))
			for name := range wm.cancels {
				names = append(names, name)
			}
			wm.mu.Unlock()
			utils.Warn("等待 workers 超时，强制继续",
				utils.Int("timeout_seconds", int(workerShutdownTimeout/time.Second)),
				utils.String("workers", strings.Join(names, ",")))
			buf := make([]byte, 1<<20)
			n := runtime.Stack(buf, true)
			utils.Warn("未退出 worker 堆栈快照\n" + string(buf[:n]))
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
				if err := srv.Shutdown(shutdownCtx); err != nil {
					// 存在未在超时内排空的活跃长连接（如 SSE），强制关闭，避免关闭流程被拖住
					utils.Warn("HTTP 服务器优雅关闭超时，强制关闭", utils.Err(err))
					_ = srv.Close()
				}
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
		if rt.ldapService != nil {
			workerStart(wm, "ldap-sync", func(workerCtx context.Context) { rt.ldapService.StartSyncWorker(workerCtx) })
		}

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
			// 文件监听器由索引服务内部管理，不随 worker 退出，需在此显式停止
			rt.indexService.StopWatcher()
			if rt.idxSearch != nil {
				if cerr := rt.idxSearch.CloseWriter(); cerr != nil {
					utils.Warn("关闭文件名检索索引失败", utils.Err(cerr))
				}
			}
			// 停止有界记录器，排空并写完全部排队操作记录
			services.StopRecordWriter()
			// 停止 DDoS 高频检测器（先于写入器，避免排空期间仍有新告警入队）
			security.StopDDoSDetector()
			// 停止安全事件写入器，排空并写完全部排队安全事件
			security.StopWriter()
			// 停止下载计数批量记录器，排空未落库的计数
			services.StopDownloadCountRecorder()
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
