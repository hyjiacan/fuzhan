package file

import (
    "fuzhan/internal/appconfig"
)

// BaseHandler 基础处理器
type BaseHandler struct {
    Config *appconfig.Config
}

// NewBaseHandler 创建基础处理器实例
func NewBaseHandler(cfg *appconfig.Config) *BaseHandler {
    return &BaseHandler{
        Config: cfg,
    }
}