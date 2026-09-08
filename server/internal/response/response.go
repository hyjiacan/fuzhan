package response

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"fuzhan/internal/appconfig"
)

// EncodeResponse 编码并发送JSON响应（用于标准 http.ResponseWriter）
func EncodeResponse(w http.ResponseWriter, data interface{}, err string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": err == "",
		"message": err,
		"data":    data,
	})
}

// UploadRecord 上传记录信息
type UploadRecord struct {
	IPAddress  string    `json:"ipAddress"`
	UploadTime time.Time `json:"uploadTime"`
	Filename   string    `json:"filename"`
	FilePath   string    `json:"path"`
}

// 上传记录存储
var uploadRecords = []UploadRecord{}
var recordsMutex sync.Mutex

// AddUploadRecord 添加上传记录
func AddUploadRecord(ipAddress string, filename string, filePath string) {
	recordsMutex.Lock()
	defer recordsMutex.Unlock()

	record := UploadRecord{
		IPAddress:  ipAddress,
		UploadTime: time.Now(),
		Filename:   filename,
	}

	// 将新记录添加到开头
	uploadRecords = append([]UploadRecord{record}, uploadRecords...)

	// 只保留最近的100条记录
	if len(uploadRecords) > 100 {
		uploadRecords = uploadRecords[:100]
	}
}

// CheckDirectoryQuota 检查共享目录配额
// 从 file_records_public 查询已用空间，索引未就绪则配额检查通过（仅检查乐观值）
func CheckDirectoryQuota(rootPath string, quota int64, additionalSize int64) error {
	if quota == 0 {
		return nil
	}

	db := appconfig.GetDB()
	if db == nil {
		return nil
	}

	var rootName string
	for name, path := range appconfig.RootNames {
		if path == rootPath {
			rootName = name
			break
		}
	}
	if rootName == "" {
		return nil
	}

	var currentSize int64
	if err := db.Table("file_records_public").
		Where("root_name = ? AND status = ? AND is_dir = ?",
			rootName, "active", false).
		Select("COALESCE(SUM(file_size), 0)").
		Scan(&currentSize).Error; err != nil {
		return nil // 索引未就绪时跳过配额检查
	}

	if currentSize+additionalSize > quota {
		return fmt.Errorf("quota exceeded: %d bytes used of %d bytes limit", currentSize, quota)
	}

	return nil
}
