package file

import (
	"fuzhan/internal/appconfig"
	"fuzhan/internal/index"
	"fuzhan/internal/repositories"
	"fuzhan/internal/storage"
)

// BaseHandler 基础处理器
type BaseHandler struct {
	Config     *appconfig.Config
	IndexSvc   *index.Service
	RecordRepo repositories.AuditStore
	// Storages 公开多根目录的存储后端映射（rootName → Storage）。
	// 本地后端为各磁盘根目录，远端后端为各虚拟前缀；供目录列举/预览等复用。
	Storages map[string]storage.Storage
}

// NewBaseHandler 创建基础处理器实例
func NewBaseHandler(cfg *appconfig.Config, indexSvc *index.Service, recordRepo repositories.AuditStore, storages map[string]storage.Storage) *BaseHandler {
	return &BaseHandler{
		Config:     cfg,
		IndexSvc:   indexSvc,
		RecordRepo: recordRepo,
		Storages:   storages,
	}
}

// storageFor 获取指定公开根目录对应的存储后端；未注册时回退物理根目录语义
func (h *BaseHandler) storageFor(rootName string) (storage.Storage, bool) {
	if h.Storages != nil {
		if st, ok := h.Storages[rootName]; ok {
			return st, true
		}
	}
	rootPath, exists := appconfig.RootNames[rootName]
	if !exists {
		return nil, false
	}
	return storage.NewLocalStorage(rootPath), true
}
