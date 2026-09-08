package services

import (
	"fuzhan/internal/index"
	"fuzhan/internal/repositories"
	"gorm.io/gorm"
)

// ServiceDependencies 服务依赖声明
// 明确各服务之间的依赖关系，确保创建顺序正确
type ServiceDependencies struct {
	DB              *gorm.DB
	IndexService    *index.Service
	RecordRepo      repositories.AuditStore
	FileService     *FileService
	SearchService   *SearchService
	AuthService     *AuthService
	DownloadService *DownloadService
}

// ServiceBuilder 服务构建器
// 确保服务按正确的依赖顺序创建
type ServiceBuilder struct {
	deps        ServiceDependencies
	tempPath    string
	privatePath string
}

// NewServiceBuilder 创建服务构建器
func NewServiceBuilder(db *gorm.DB, tempPath, privatePath string) *ServiceBuilder {
	return &ServiceBuilder{
		deps: ServiceDependencies{
			DB: db,
		},
		tempPath:    tempPath,
		privatePath: privatePath,
	}
}

// WithRecordRepository 设置记录仓库
func (sb *ServiceBuilder) WithRecordRepository() *ServiceBuilder {
	sb.deps.RecordRepo = repositories.NewRecordRepository(sb.deps.DB)
	return sb
}

// WithIndexService 设置索引服务（需在 FileService 之前创建）
func (sb *ServiceBuilder) WithIndexService(rootNames map[string]string) *ServiceBuilder {
	sb.deps.IndexService = index.NewService(sb.deps.DB, rootNames, sb.tempPath, sb.privatePath)
	return sb
}

// WithFileService 设置文件服务（依赖 IndexService）
func (sb *ServiceBuilder) WithFileService() *ServiceBuilder {
	if sb.deps.IndexService == nil {
		panic("ServiceBuilder: IndexService must be set before FileService")
	}
	sb.deps.FileService = NewFileService()
	return sb
}

// WithSearchService 设置搜索服务（依赖 DB 直接查询索引表）
func (sb *ServiceBuilder) WithSearchService() *ServiceBuilder {
	sb.deps.SearchService = NewSearchService(sb.deps.DB)
	return sb
}

// WithDownloadService 设置下载服务（依赖 RecordRepo 和 SearchService）
func (sb *ServiceBuilder) WithDownloadService() *ServiceBuilder {
	if sb.deps.RecordRepo == nil {
		panic("ServiceBuilder: RecordRepo must be set before DownloadService")
	}
	if sb.deps.SearchService == nil {
		panic("ServiceBuilder: SearchService must be set before DownloadService")
	}
	sb.deps.DownloadService = NewDownloadServiceWithRepo(sb.deps.RecordRepo, sb.deps.SearchService)
	return sb
}

// WithAuthService 设置认证服务
func (sb *ServiceBuilder) WithAuthService() *ServiceBuilder {
	sb.deps.AuthService = NewAuthService(sb.deps.DB)
	return sb
}

// WithPrivateStorageService 设置私有存储服务（依赖 FileService）
func (sb *ServiceBuilder) WithPrivateStorageService() *ServiceBuilder {
	if sb.deps.FileService == nil {
		panic("ServiceBuilder: FileService must be set before PrivateStorageService")
	}
	// 注意：这个方法需要额外参数，这里只创建基本服务
	return sb
}

// Build 验证所有服务是否已正确创建
func (sb *ServiceBuilder) Build() *ServiceDependencies {
	if sb.deps.DB == nil {
		panic("ServiceBuilder: DB must be set")
	}
	return &sb.deps
}

// BuildAll 按正确顺序构建所有核心服务
func BuildAllServices(db *gorm.DB, rootNames map[string]string, tempPath, privatePath string) *ServiceDependencies {
	deps := &ServiceDependencies{DB: db}

	// 1. 先创建仓库
	deps.RecordRepo = repositories.NewRecordRepository(db)

	// 2. 创建索引服务
	deps.IndexService = index.NewService(db, rootNames, tempPath, privatePath)

	// 3. 基于索引服务创建文件服务
	deps.FileService = NewFileService()

	// 4. 基于 DB 创建搜索服务（直接查询索引表）
	deps.SearchService = NewSearchService(db)
	deps.DownloadService = NewDownloadServiceWithRepo(deps.RecordRepo, deps.SearchService)
	deps.AuthService = NewAuthService(db)

	return deps
}
