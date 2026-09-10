package index

import (
	"time"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/utils"
	"github.com/robfig/cron/v3"
)

// getDefaultCronExpr 获取默认 cron 表达式
func getDefaultCronExpr() string {
	return "0 1 * * *" // 每天凌晨 1:00
}

// StartScanTimer 启动定时扫描器，根据 cron 表达式周期执行全量扫描
// 在 main.go 初始化后调用，配置变更时会自动重启
func (s *Service) StartScanTimer() {
	s.StopScanTimer() // 确保先停掉旧的

	cronExpr := appconfig.GlobalConfig.Index.ScanCronExpression
	if cronExpr == "" {
		cronExpr = getDefaultCronExpr()
	}

	// 验证 cron 表达式
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		utils.Warn("定时扫描 cron 表达式无效，使用默认值",
			utils.String("cron", cronExpr),
			utils.Err(err))
		cronExpr = getDefaultCronExpr()
		schedule, _ = parser.Parse(cronExpr)
	}

	s.timerStopCh = make(chan struct{})
	s.timerRunning = true

	go s.runScanTimer(schedule, cronExpr)

	utils.Info("定时扫描器已启动",
		utils.String("cron", cronExpr),
		utils.String("next_scan", schedule.Next(time.Now()).Format("2006-01-02 15:04:05")))
}

// StopScanTimer 停止定时扫描器
func (s *Service) StopScanTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timerStopCh != nil {
		close(s.timerStopCh)
		s.timerStopCh = nil
	}
	s.timerRunning = false
}

// RestartScanTimer 重启定时扫描器（用于配置热生效）
func (s *Service) RestartScanTimer() {
	utils.Info("配置变更，重启定时扫描器")
	s.StopScanTimer()
	s.StartScanTimer()
}

// runScanTimer 定时扫描核心循环，基于 cron 表达式调度
func (s *Service) runScanTimer(schedule cron.Schedule, cronExpr string) {
	// 捕获 stopCh 本地副本，防止 StopScanTimer 将 s.timerStopCh 置 nil
	// 后导致 nil channel 永久阻塞（goroutine 泄漏）
	s.mu.Lock()
	stopCh := s.timerStopCh
	s.mu.Unlock()

	// 计算下次执行时间
	nextTime := schedule.Next(time.Now())
	utils.Info("定时扫描下次执行时间",
		utils.String("next", nextTime.Format("2006-01-02 15:04:05")))

	for {
		now := utils.Now()
		if !now.Before(nextTime) {
			// 执行定时扫描
			utils.Info("定时扫描触发",
				utils.String("cron", cronExpr),
				utils.String("last_scan", func() string {
					s.mu.Lock()
					last := s.lastScanEnd
					s.mu.Unlock()
					if last.IsZero() {
						return "从未扫描"
					}
					return last.Format("2006-01-02 15:04:05")
				}()))
			s.runScheduledScan()

			// 计算下次执行时间
			nextTime = schedule.Next(time.Now())
			utils.Info("定时扫描下次执行时间",
				utils.String("next", nextTime.Format("2006-01-02 15:04:05")))

			// 检查配置变更（支持热生效）
			newCronExpr := appconfig.GlobalConfig.Index.ScanCronExpression
			if newCronExpr == "" {
				newCronExpr = getDefaultCronExpr()
			}
			if newCronExpr != cronExpr {
				parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
				if newSchedule, err := parser.Parse(newCronExpr); err == nil {
					cronExpr = newCronExpr
					schedule = newSchedule
					nextTime = schedule.Next(time.Now())
					utils.Info("定时扫描 cron 表达式已变更",
						utils.String("cron", cronExpr),
						utils.String("next", nextTime.Format("2006-01-02 15:04:05")))
				}
			}
		}

		// 等待到下次执行时间，同时响应停止信号
		waitDuration := nextTime.Sub(time.Now())
		if waitDuration < 0 {
			waitDuration = 0
		}

		// 每 30 秒检查一次停止信号，避免长时间阻塞
		checkTicker := time.NewTicker(30 * time.Second)
		select {
		case <-stopCh:
			checkTicker.Stop()
			utils.Info("定时扫描器已停止")
			return
		case <-time.After(waitDuration):
			checkTicker.Stop()
			// 时间到，进入下一轮循环执行扫描
		case <-checkTicker.C:
			checkTicker.Stop()
			// 继续循环重新检查时间
		}
	}
}

// runScheduledScan 执行一次完整的定时扫描（public → temp → private）
func (s *Service) runScheduledScan() {
	scopes := []ScanScope{ScanScopePublic, ScanScopeTemp, ScanScopePrivate}

	for _, scope := range scopes {
		// 按配置跳过未启用的范围
		switch scope {
		case ScanScopeTemp:
			if !appconfig.GetConfig().Storage.Temp.Enabled {
				utils.Info("定时扫描跳过临时文件范围（临时文件功能未启用）")
				continue
			}
		case ScanScopePrivate:
			if !appconfig.GetConfig().Storage.Private.Enabled {
				utils.Info("定时扫描跳过私有文件范围（私有存储功能未启用）")
				continue
			}
		}

		s.mu.Lock()
		s.currentScope = scope
		s.mu.Unlock()

		utils.Info("定时扫描进行中", utils.String("scope", string(scope)))
		err := s.StartScanByScope(scope, "timer")
		if err != nil {
			utils.Warn("定时扫描范围失败",
				utils.String("scope", string(scope)),
				utils.Err(err))
			// 继续下一个 scope，不中断
		}

		// 等待当前 scope 扫描完成，同时响应停止信号
		for {
			// 检查停止信号，避免扫描期间 StopScanTimer 被调后长时间无响应
			s.mu.Lock()
			running := s.timerRunning
			s.mu.Unlock()
			if !running {
				return
			}

			progress := s.scanner.GetScopeProgress(scope)
			if progress.Status == ScanStatusCompleted || progress.Status == ScanStatusFailed {
				if progress.Status == ScanStatusCompleted {
					utils.Info("定时扫描范围完成",
						utils.String("scope", string(scope)),
						utils.Int64("scanned_files", progress.ScannedFiles))
				} else {
					utils.Warn("定时扫描范围异常",
						utils.String("scope", string(scope)),
						utils.String("status", string(progress.Status)),
						utils.String("error", progress.ErrorMessage))
				}
				break
			}
			time.Sleep(2 * time.Second)
		}
	}

	s.mu.Lock()
	s.lastScanEnd = utils.Now()
	s.currentScope = ""
	s.mu.Unlock()

	utils.Info("定时扫描全部完成",
		utils.String("finish_time", s.lastScanEnd.Format("2006-01-02 15:04:05")))
}

// GetScanStatus 获取当前扫描状态（供 Footer API 使用）
func (s *Service) GetScanStatus() ScanStatusResponse {
	s.mu.Lock()
	lastScan := s.lastScanEnd
	s.mu.Unlock()

	cronExpr := appconfig.GlobalConfig.Index.ScanCronExpression
	if cronExpr == "" {
		cronExpr = getDefaultCronExpr()
	}

	var nextScan *time.Time
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if schedule, err := parser.Parse(cronExpr); err == nil {
		ns := schedule.Next(time.Now())
		nextScan = &ns
	}

	// 检查当前是否有扫描在进行（取任一 scope 的状态）
	isScanning := false
	scope := ""
	var processed, total int64
	for _, sc := range []ScanScope{ScanScopePublic, ScanScopeTemp, ScanScopePrivate} {
		p := s.scanner.GetScopeProgress(sc)
		if p.Status == ScanStatusRunning {
			isScanning = true
			scope = string(sc)
			processed = p.ScannedFiles
			total = p.TotalFiles
			break
		}
	}

	return ScanStatusResponse{
		LastScanTime:       copyTimePtr(&lastScan),
		NextScanTime:       nextScan,
		IsScanning:         isScanning,
		ScanScope:          scope,
		ScanProgress:       processed,
		ScanTotal:          total,
		ScanCronExpression: cronExpr,
	}
}

// copyTimePtr 返回 time.Time 指针的安全拷贝
func copyTimePtr(t *time.Time) *time.Time {
	if t == nil || t.IsZero() {
		return nil
	}
	cp := *t
	return &cp
}
