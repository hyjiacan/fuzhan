package services

import (
    "encoding/json"
    "os"
    "path/filepath"
    "strings"
    "time"

    "fuzhan/internal/appconfig"
    "fuzhan/internal/utils"
)

// PrivateFileMeta 私有文件元数据
type PrivateFileMeta struct {
    Filename   string    `json:"filename"`
    UploadTime time.Time `json:"uploadTime"`
    FileSize   int64     `json:"fileSize"`
    Owner      string    `json:"owner"`
    Code       string    `json:"code"`
    ExpireTime time.Time `json:"expireTime"`
}

// PrivateStorageService 私有存储服务
type PrivateStorageService struct {
    fileService interface{}
}

// NewPrivateStorageService 创建私有存储服务实例
func NewPrivateStorageService(fileService interface{}) *PrivateStorageService {
    return &PrivateStorageService{fileService: fileService}
}

// CleanUpExpiredFiles 清理过期的私有文件
func (ts *PrivateStorageService) CleanUpExpiredFiles(tempConfig appconfig.PrivateStorageConfig) {
    if !tempConfig.Enabled {
        return
    }

    cleanUpExpiredPrivateFiles()
    utils.Info("私有存储清理任务执行完成")
}

// loadPrivateFileMetadata 加载文件元数据（内联实现以避免循环导入）
func loadPrivateFileMetadata(fileDir string) (*PrivateFileMeta, error) {
    metaPath := filepath.Join(fileDir, "meta.json")
    file, err := os.Open(metaPath)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    var meta PrivateFileMeta
    if err := json.NewDecoder(file).Decode(&meta); err != nil {
        return nil, err
    }
    return &meta, nil
}

// cleanUpExpiredPrivateFiles 清理过期文件（内联实现以避免循环导入）
func cleanUpExpiredPrivateFiles() {
    if !appconfig.GlobalConfig.Storage.Private.Enabled {
        return
    }

    usersDir := filepath.Join(appconfig.GlobalConfig.Storage.Private.Path, "users")
    if _, err := os.Stat(usersDir); os.IsNotExist(err) {
        return
    }

    entries, err := os.ReadDir(usersDir)
    if err != nil {
        utils.Error("读取用户目录失败", utils.Err(err))
        return
    }

    var deleted int
    for _, userEntry := range entries {
        if !userEntry.IsDir() {
            continue
        }

        userDir := filepath.Join(usersDir, userEntry.Name())
        filepath.Walk(userDir, func(path string, info os.FileInfo, err error) error {
            if err != nil {
                return nil
            }
            if !info.IsDir() && strings.EqualFold(info.Name(), "meta.json") {
                fileDir := filepath.Dir(path)
                meta, loadErr := loadPrivateFileMetadata(fileDir)
                if loadErr != nil {
                    return nil
                }
                if !meta.ExpireTime.IsZero() && time.Now().After(meta.ExpireTime) {
                    os.RemoveAll(fileDir)
                    deleted++
                    utils.Info("已删除过期文件", utils.String("filename", meta.Filename))
                }
            }
            return nil
        })
    }

    if deleted > 0 {
        utils.Info("清理过期私有文件", utils.Int("count", deleted))
    }
}