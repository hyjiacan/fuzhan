package appconfig

import (
	"fmt"
	"net"
	"strings"
)

// GetConfig 获取当前配置的副本（线程安全）
func GetConfig() Config {
	configMu.RLock()
	defer configMu.RUnlock()
	return GlobalConfig
}

// IsPortInUse 检查端口是否被占用
func IsPortInUse(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return true
	}
	defer listener.Close()
	return false
}

// ParseQuotaString 解析配额字符串为字节数
func ParseQuotaString(quotaStr string) (int64, error) {
	if quotaStr == "" {
		return 0, nil
	}

	// 转换为大写以便处理
	quotaStr = strings.ToUpper(strings.TrimSpace(quotaStr))

	// 定义单位映射
	units := map[string]int64{
		"B":  1,
		"K":  1024,
		"KB": 1024,
		"M":  1024 * 1024,
		"MB": 1024 * 1024,
		"G":  1024 * 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
		"T":  1024 * 1024 * 1024 * 1024,
		"TB": 1024 * 1024 * 1024 * 1024,
	}

	// 查找单位
	var numberStr string
	var unitStr string

	// 从字符串末尾开始查找单位
	for i := len(quotaStr) - 1; i >= 0; i-- {
		char := quotaStr[i]
		if char >= '0' && char <= '9' || char == '.' {
			numberStr = quotaStr[:i+1]
			unitStr = quotaStr[i+1:]
			break
		}
	}

	// 如果没有找到数字部分，说明整个字符串都是数字
	if numberStr == "" {
		numberStr = quotaStr
	}

	// 解析数字部分
	number, err := parseFloat(numberStr)
	if err != nil {
		return 0, fmt.Errorf("invalid quota format: %s", quotaStr)
	}

	// 获取单位乘数
	multiplier := int64(1)
	if unitStr != "" {
		var exists bool
		multiplier, exists = units[unitStr]
		if !exists {
			return 0, fmt.Errorf("unsupported quota unit: %s", unitStr)
		}
	}

	// 计算最终字节数
	result := int64(number * float64(multiplier))
	return result, nil
}

// parseFloat 解析浮点数字符串
func parseFloat(s string) (float64, error) {
	var result float64
	_, err := fmt.Sscanf(s, "%f", &result)
	return result, err
}

var onSaveHooks []func()

// AddOnSaveHook 注册配置保存后的钩子
func AddOnSaveHook(hook func()) {
	onSaveHooks = append(onSaveHooks, hook)
}

// RunOnSaveHooks 触发热更新钩子
func RunOnSaveHooks() {
	for _, hook := range onSaveHooks {
		hook()
	}
}
