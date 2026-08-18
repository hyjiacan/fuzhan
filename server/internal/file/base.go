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
    RecordRepo *repositories.RecordRepository
}

// NewBaseHandler 创建基础处理器实例
func NewBaseHandler(cfg *appconfig.Config, indexSvc *index.Service, recordRepo *repositories.RecordRepository) *BaseHandler {
    return &BaseHandler{
        Config:     cfg,
        IndexSvc:   indexSvc,
        RecordRepo: recordRepo,
    }
}