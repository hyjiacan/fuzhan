package appconfig

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"fuzhan/internal/utils"
)

// DatabaseConfig 数据库配置结构体
type DatabaseConfig struct {
	Driver string     `yaml:"driver"` // sqlite, mysql, postgres
	DSN    string     `yaml:"dsn"`    // 连接字符串
	Pool   PoolConfig `yaml:"pool"`   // 连接池配置
}

// PoolConfig 数据库连接池配置
type PoolConfig struct {
	MaxIdleConns    int `yaml:"max_idle_conns"`    // 最大空闲连接数，默认 2
	MaxOpenConns    int `yaml:"max_open_conns"`    // 最大打开连接数，默认 8
	ConnMaxLifetime int `yaml:"conn_max_lifetime"` // 连接最大生存时间（秒），默认 3600
}

// DefaultPoolConfig 返回默认连接池配置
// 注意：SQLite 是单写者数据库，过大的连接池并不能提升写并发，
// 反而会放大写锁争抢、并在等锁时占满连接池。
// 因此对 SQLite 应使用小型连接池。
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxIdleConns:    2,
		MaxOpenConns:    8,
		ConnMaxLifetime: 3600, // 1小时
	}
}

// dbAtomic 原子化的数据库实例，支持热替换
var dbAtomic atomic.Value

// GetDB 获取当前数据库实例
func GetDB() *gorm.DB {
	if db, ok := dbAtomic.Load().(*gorm.DB); ok {
		return db
	}
	return nil
}

// SetDB 设置数据库实例（用于热重启时替换）
func SetDB(db *gorm.DB) {
	dbAtomic.Store(db)
}

// gormConfig GORM 配置
var gormConfig = &gorm.Config{
	Logger: logger.Default.LogMode(logger.Warn),
	NowFunc: func() time.Time {
		return time.Now().UTC()
	},
}

// InitDB 初始化数据库连接
func InitDB(cfg *DatabaseConfig) (*gorm.DB, error) {
	if cfg == nil || cfg.Driver == "" {
		cfg = &DatabaseConfig{
			Driver: "sqlite",
			DSN:    defaultSQLiteDSN(),
		}
	}

	if cfg.DSN == "" {
		return nil, fmt.Errorf("database DSN cannot be empty")
	}

	var db *gorm.DB
	var err error

	switch cfg.Driver {
	case "sqlite":
		// 使用纯 Go 驱动 (glebarez/sqlite 基于 modernc.org/sqlite)。
		// 追加 pragma 缓解并发写锁：busy_timeout 让写锁等待而非立即失败，WAL 提升读写并发
		sqliteDSN := appendSQLitePragmas(normalizeSQLiteDSN(cfg.DSN))
		db, err = gorm.Open(sqlite.Open(sqliteDSN), gormConfig)
		if err != nil {
			return nil, fmt.Errorf("SQLite 连接失败: %w", err)
		}
	case "mysql":
		db, err = gorm.Open(mysql.Open(cfg.DSN), gormConfig)
		if err != nil {
			return nil, fmt.Errorf("MySQL 连接失败: %w", err)
		}
	case "postgres":
		db, err = gorm.Open(postgres.Open(cfg.DSN), gormConfig)
		if err != nil {
			return nil, fmt.Errorf("PostgreSQL 连接失败: %w", err)
		}
	default:
		return nil, fmt.Errorf("不支持的数据库类型: %s", cfg.Driver)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 应用连接池配置，使用默认值（0值会使用默认值）
	pool := cfg.Pool
	if pool.MaxIdleConns <= 0 {
		pool.MaxIdleConns = 2
	}
	if pool.MaxOpenConns <= 0 {
		pool.MaxOpenConns = 8
	}
	if pool.ConnMaxLifetime <= 0 {
		pool.ConnMaxLifetime = 3600
	}
	sqlDB.SetMaxIdleConns(pool.MaxIdleConns)
	sqlDB.SetMaxOpenConns(pool.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Duration(pool.ConnMaxLifetime) * time.Second)

	utils.Info("数据库连接成功",
		utils.String("driver", cfg.Driver),
		utils.Int("max_idle_conns", pool.MaxIdleConns),
		utils.Int("max_open_conns", pool.MaxOpenConns),
		utils.Int("conn_max_lifetime_sec", pool.ConnMaxLifetime))
	return db, nil
}

// defaultSQLiteDSN 返回默认 SQLite 数据文件路径（位于工作目录 data 子目录下）
func defaultSQLiteDSN() string {
	return filepath.Join(GetDataDir(), "fuzhan.db")
}

// normalizeSQLiteDSN 将历史遗留/缺省的 SQLite DSN 归一到工作目录 data 子目录。
// 兼容旧默认值 "fuzhan.db"（位于进程当前目录），迁移到标准路径方便统一管理。
func normalizeSQLiteDSN(dsn string) string {
	if dsn == "" || dsn == "fuzhan.db" || dsn == "./fuzhan.db" {
		resolved := defaultSQLiteDSN()
		if dsn != "" {
			utils.Warn("SQLite 数据文件从旧路径迁移到标准数据目录",
				utils.String("from", dsn), utils.String("to", resolved))
			migrateLegacySQLiteFile(dsn, resolved)
		}
		return resolved
	}
	return dsn
}

// migrateLegacySQLiteFile 将旧位置的 SQLite 数据文件无损复制到新位置。
// 仅当旧文件存在且目标文件不存在时执行，避免覆盖与重复迁移。
func migrateLegacySQLiteFile(from, to string) {
	src, err := filepath.Abs(from)
	if err != nil {
		utils.Warn("解析旧 SQLite 路径失败，跳过迁移", utils.Err(err))
		return
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return // 旧文件本就不存在，无需迁移
	}
	if _, err := os.Stat(to); err == nil {
		return // 目标已存在，跳过
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		utils.Warn("创建数据目录失败，跳过 SQLite 迁移", utils.Err(err))
		return
	}
	in, err := os.Open(src)
	if err != nil {
		utils.Warn("打开旧 SQLite 文件失败，跳过迁移", utils.Err(err))
		return
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		utils.Warn("创建新 SQLite 文件失败，跳过迁移", utils.Err(err))
		return
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(to)
		utils.Warn("复制 SQLite 数据失败，跳过迁移", utils.Err(err))
		return
	}
	if err := out.Close(); err != nil {
		utils.Warn("关闭新 SQLite 文件失败", utils.Err(err))
		return
	}
	utils.Info("SQLite 数据文件迁移完成",
		utils.String("from", src), utils.String("to", to))
}

// appendSQLitePragmas 在 SQLite DSN 上追加连接级 pragma。
// busy_timeout 接受写锁等待时间（毫秒），避免并发写时立即返回 SQLITE_BUSY；
// 值不宜过大：等待期间会占用连接池连接和 goroutine，过大会把数据库锁竞争
// 放大成连接池耗尽。配合小型连接池 + 后台写串行化，2 秒已足够消化单条写锁碰撞。
// journal_mode=WAL 提升读写并发能力。
func appendSQLitePragmas(dsn string) string {
	const (
		busyTimeout    = "busy_timeout(2000)"
		journalModeWAL = "journal_mode(WAL)"
	)

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	// 避免对已存在的 pragma 重复追加
	if strings.Contains(dsn, "busy_timeout") {
		return dsn + sep + "_pragma=" + journalModeWAL
	}
	return dsn + sep + "_pragma=" + busyTimeout + "&_pragma=" + journalModeWAL
}

// InitDBWithAutoMigrate 初始化数据库并执行自动迁移
func InitDBWithAutoMigrate(cfg *DatabaseConfig, models ...interface{}) (*gorm.DB, error) {
	db, err := InitDB(cfg)
	if err != nil {
		return nil, err
	}

	if len(models) > 0 {
		if err := db.AutoMigrate(models...); err != nil {
			return nil, fmt.Errorf("数据库迁移失败: %w", err)
		}
		utils.Info("数据库迁移完成", utils.Int("models", len(models)))
	}

	dbAtomic.Store(db)
	return db, nil
}

// CloseDB 关闭数据库连接
func CloseDB() error {
	db := GetDB()
	if db != nil {
		sqlDB, err := db.DB()
		if err != nil {
			return err
		}
		err = sqlDB.Close()
		dbAtomic.Store((*gorm.DB)(nil))
		return err
	}
	return nil
}
