package file

import (
    "fuzhan/internal/appconfig"
    "fuzhan/internal/index"
    "fuzhan/internal/repositories"
)

// BaseHandler 基础处理器
type BaseHandler struct {
    Config     *appconfig.Config
    IndexSvc   *index.Service
    RecordRepo repositories.AuditStore
}

// NewBaseHandler 创建基础处理器实例
func NewBaseHandler(cfg *appconfig.Config, indexSvc *index.Service, recordRepo repositories.AuditStore) *BaseHandler {
    return &BaseHandler{
        Config:     cfg,
        IndexSvc:   indexSvc,
        RecordRepo: recordRepo,
    }
}