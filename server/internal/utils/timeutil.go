package utils

import "time"

// Now 返回统一的当前时间（UTC）。
// 项目内所有写入数据库/持久化的时间字段，均应通过本函数获取时间戳，
// 保证全链路采用同一时区（UTC）存储，避免本地时区与 UTC 混用。
func Now() time.Time {
	return time.Now().UTC()
}
