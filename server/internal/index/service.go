package index

import (
	"sync"
	"time"

	"fuzhan/internal/models"
	"gorm.io/gorm"
)

// Service 文件索引服务
// 提供文件索引的 CRUD、全量扫描、增量同步、一致性校验、哈希计算等能力。
// 作为组合 facade, 内部协调 Scanner/Syncer/ConsistencyChecker/HashWorker。
type Service struct {
	db         *gorm.DB
	rootNames  map[string]string
	scanner    *Scanner
	syncer     *Syncer
	checker    *ConsistencyChecker
	hashWorker *HashWorker

	// 任务记录服务（可选，用于记录扫描/哈希等任务到 task_records 表）
	taskService TaskRecorder

	// 定时扫描
	mu           sync.Mutex
	timerStopCh  chan struct{}
	timerRunning bool
	lastScanEnd  time.Time // 上次扫描完成时间
	currentScope ScanScope // 当前正在扫描的 scope

	// 文件监听
	watcher *Watcher

	// 最近同步的文件记录（key: rootName:relPath，用于防止 watcher 重复处理上传文件）
	recentlySynced map[string]time.Time
	recentlyMu     sync.Mutex
	recentlyStopCh chan struct{}
}

// TaskRecorder 任务记录接口（解耦 index 包对 services 包的依赖）
type TaskRecorder interface {
	CreateTask(taskType, taskName, trigger string) (*models.TaskRecord, error)
	StartTask(id uint) error
	UpdateTaskProgress(id uint, progress int, doneItems, totalItems int64) error
	UpdateTaskDetails(id uint, details string) error
	CompleteTask(id uint) error
	FailTask(id uint, errMsg string) error
}

// SetTaskService 注入任务记录服务
func (s *Service) SetTaskService(ts TaskRecorder) {
	s.taskService = ts
	s.scanner.taskService = ts
	s.hashWorker.taskService = ts
}

// SetFileIndexNotifier 注入文件名检索索引变更通知器（可选）。
// 文件增/删/移动时增量同步检索索引，保证搜索联想与拼写纠错与索引表一致。
func (s *Service) SetFileIndexNotifier(n FileIndexNotifier) {
	s.syncer.SetFileIndexNotifier(n)
}

// NewService 创建索引服务
func NewService(db *gorm.DB, rootNames map[string]string, tempFilePath, privatePath string) *Service {
	s := &Service{
		db:             db,
		rootNames:      rootNames,
		scanner:        NewScanner(db, rootNames, tempFilePath, privatePath),
		syncer:         NewSyncer(db, rootNames),
		checker:        NewConsistencyChecker(db, rootNames),
		hashWorker:     NewHashWorker(db, rootNames),
		recentlySynced: make(map[string]time.Time),
		recentlyStopCh: make(chan struct{}),
	}
	// 启动最近同步记录清理协程
	go s.cleanupRecentlySynced()
	return s
}
