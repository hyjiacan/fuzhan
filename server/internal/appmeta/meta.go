// Package appmeta 提供启动期"一次性任务"的持久化执行记录（键值表）。
//
// 背景：bootstrap 中的索引创建、历史数据回填、检索索引全量重建等操作都是幂等的，
// 但只要没有执行记录，每次启动都会重新扫全表，造成启动耗时随数据量线性增长
// （表现为服务化启动时长时间无响应）。
//
// 本包用一张极小的 app_meta 表记录这些任务的完成标记与版本号：
//   - 任务逻辑或数据口径变化时递增 token，即可触发一次重新执行；
//   - token 未变化则整体跳过，避免重复的全表扫描。
package appmeta

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AppMeta 一次性任务的执行记录（键值表）
type AppMeta struct {
	Key       string    `gorm:"primaryKey;size:128" json:"key"` // 任务标识
	Value     string    `gorm:"size:512" json:"value"`          // 完成标记 / 版本 token
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (AppMeta) TableName() string {
	return "app_meta"
}

// Get 读取 key 对应的值；不存在或查询失败时返回空串。
func Get(db *gorm.DB, key string) string {
	if db == nil {
		return ""
	}
	var m AppMeta
	if err := db.Where("key = ?", key).Take(&m).Error; err != nil {
		return ""
	}
	return m.Value
}

// Set 写入（或覆盖）key 的值。
func Set(db *gorm.DB, key, value string) error {
	if db == nil {
		return nil
	}
	row := AppMeta{Key: key, Value: value, UpdatedAt: time.Now()}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error
}

// RunOnce 仅当 key 的当前值不等于 token 时执行 fn，执行成功后写入 token。
//
// 返回是否真正执行了 fn：
//   - db 为 nil（无法持久化）时总是执行，行为与旧逻辑一致（无门控）；
//   - fn 返回错误时不写入 token，下次启动会重试，保证任务最终完成。
func RunOnce(db *gorm.DB, key, token string, fn func() error) bool {
	if db == nil {
		_ = fn()
		return true
	}
	if Get(db, key) == token {
		return false
	}
	if err := fn(); err != nil {
		return true
	}
	_ = Set(db, key, token)
	return true
}
