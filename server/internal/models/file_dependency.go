package models

import "time"

// FileDependency 文件依赖关系
//
// 记录文件之间的依赖/引用关系，形成文件依赖网络。
// Relation 类型: requires / referenced_by / related
type FileDependency struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	FileRecordID uint      `gorm:"not null;index:idx_file_dep_source,priority:1" json:"file_record_id"`
	DependsOnID  uint      `gorm:"not null;index:idx_file_dep_target,priority:1" json:"depends_on_id"`
	Relation     string    `gorm:"size:32;not null;default:requires" json:"relation"`
	Description  string    `gorm:"size:512" json:"description"`
	CreatedAt    time.Time `json:"created_at"`

	// 关联（仅查询时填充）
	SourceFile *FileRecordPublic `gorm:"foreignKey:FileRecordID" json:"source_file,omitempty"`
	TargetFile *FileRecordPublic `gorm:"foreignKey:DependsOnID" json:"target_file,omitempty"`
}

// TableName 指定表名
func (FileDependency) TableName() string {
	return "file_dependencies"
}
