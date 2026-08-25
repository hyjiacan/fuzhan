package appconfig

import (
    "fmt"
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
    MaxIdleConns    int `yaml:"max_idle_conns"`    // 最大空闲连接数，默认 5
    MaxOpenConns    int `yaml:"max_open_conns"`    // 最大打开连接数，默认 25
    ConnMaxLifetime int `yaml:"conn_max_lifetime"` // 连接最大生存时间（秒），默认 3600
}

// DefaultPoolConfig 返回默认连接池配置
func DefaultPoolConfig() PoolConfig {
    return PoolConfig{
        MaxIdleConns:    5,
        MaxOpenConns:    25,
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
            DSN:    "fuzhan.db",
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
        sqliteDSN := appendSQLitePragmas(cfg.DSN)
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
        pool.MaxIdleConns = 5
    }
    if pool.MaxOpenConns <= 0 {
        pool.MaxOpenConns = 25
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

// appendSQLitePragmas 在 SQLite DSN 上追加连接级 pragma。
// busy_timeout 接受写锁等待时间（毫秒），避免并发写时立即返回 SQLITE_BUSY；
// journal_mode=WAL 提升读写并发能力。
func appendSQLitePragmas(dsn string) string {
    const (
        busyTimeout    = "busy_timeout(15000)"
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
