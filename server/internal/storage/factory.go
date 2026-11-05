package storage

import (
	"fmt"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/studio-b12/gowebdav"
)

// ConnConfig 后端存储通用连接配置。
// 对应配置文件 storage.s3 / storage.oss / storage.webdav 三节；不同后端只使用其中相关字段。
// 为避免 storage 包反向依赖 appconfig 形成循环，此处定义最小连接配置，由上层负责转换。
type ConnConfig struct {
	Endpoint  string // S3/OSS 端点，如 https://s3.amazonaws.com 或 oss-cn-hangzhou.aliyuncs.com
	AccessKey string // S3/OSS 访问密钥
	SecretKey string // S3/OSS 访问密钥
	Bucket    string // S3/OSS 桶名
	Region    string // S3 区域（MinIO 可留空）
	UseSSL    bool   // S3/MinIO 是否启用 HTTPS

	WebDAVURL      string // WebDAV 服务地址
	WebDAVUsername string // WebDAV 用户名
	WebDAVPassword string // WebDAV 密码
}

// NewStorageFromConfig 根据后端类型创建对应存储实例。
//
// backend 取值：local / s3 / minio / oss / webdav。
// rootPath 为本逻辑存储实例的根：本地后端为磁盘根目录；远端后端为虚拟前缀。
func NewStorageFromConfig(backend, rootPath string, cfg ConnConfig) (Storage, error) {
	switch backend {
	case "local":
		return NewLocalStorage(rootPath), nil

	case "s3", "minio":
		client, err := minio.New(cfg.Endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
			Secure: cfg.UseSSL,
			Region: cfg.Region,
		})
		if err != nil {
			return nil, fmt.Errorf("创建 S3/MinIO 客户端失败: %w", err)
		}
		return NewS3Storage(client, cfg.Bucket, rootPath), nil

	case "oss":
		client, err := oss.New(cfg.Endpoint, cfg.AccessKey, cfg.SecretKey)
		if err != nil {
			return nil, fmt.Errorf("创建 OSS 客户端失败: %w", err)
		}
		return NewOSSStorage(client, cfg.Bucket, rootPath)

	case "webdav":
		client := gowebdav.NewClient(cfg.WebDAVURL, cfg.WebDAVUsername, cfg.WebDAVPassword)
		return NewWebDAVStorage(client, rootPath), nil

	default:
		return nil, fmt.Errorf("不支持的存储后端类型: %s", backend)
	}
}
