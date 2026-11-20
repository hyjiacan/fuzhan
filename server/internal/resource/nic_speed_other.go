//go:build !linux && !windows

package resource

import "context"

// nicLinkSpeeds 其它平台不做链路速率探测（无统一的无外部进程 API），返回空结果。
func nicLinkSpeeds(_ context.Context) map[string]int {
	return map[string]int{}
}
