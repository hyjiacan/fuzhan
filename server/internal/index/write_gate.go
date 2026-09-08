package index

import (
	"sync"
	"time"

	"fuzhan/internal/utils"
)

// sqliteWriteMu 串行化高吞吐的批量后台写。
// SQLite 是单写者数据库；Scanner（全量扫描批量写入）与 HashWorker（逐条更新
// 哈希）都可能在后台并行操作 file_records_* 表。此锁保证同一时刻只有一路
// 后台批量写在执行，并在每批之间释放，让前台小写（上传/登录/索引同步）能插入，
// 从而避免两路后台写互相抢占，把 busy_timeout 造成的"等锁占用连接"放大成
// 连接池耗尽。
//
// 注意：锁只包住"单条/单批数据写"语句，绝不能包住磁盘文件哈希等慢操作。
var sqliteWriteMu sync.Mutex

// writeLockWarnAfter 等锁超过该阈值时记录警告日志，用于观测锁竞争窗口。
const writeLockWarnAfter = 200 * time.Millisecond

// lockWrite 申请后台批量写锁，返回释放函数。等待超过阈值会记录日志。
func lockWrite() (release func()) {
	start := time.Now()
	sqliteWriteMu.Lock()
	if waited := time.Since(start); waited > writeLockWarnAfter {
		utils.Warn("后台批量写等锁耗时较长",
			utils.String("wait_ms", waited.Round(time.Millisecond).String()))
	}
	return sqliteWriteMu.Unlock
}
