package middleware

// webActionFallback 未登记路由的兜底操作名。
// 新增 Web 路由时建议在此表补充具体业务操作名，便于在 api.log 中直接看出做了什么。
const webActionFallback = "Web界面操作"

// webRouteActions 将「方法 + 路由模板」映射为具体业务操作名。
// 键取自 gin 的 c.FullPath()（如 /api/v1/web/admin/users/:uuid/reset-password），
// 与前端实际调用路径一致；未登记的路由回退到 webActionFallback。
var webRouteActions = map[string]string{
	// 健康检查与系统选项
	"GET /api/v1/web/health":            "健康检查",
	"GET /api/v1/web/ready":             "就绪检查",
	"GET /api/v1/web/options":           "读取系统选项",
	"GET /api/v1/web/config":            "读取系统配置",
	"POST /api/v1/web/config":           "保存系统配置",
	"GET /api/v1/web/config/partitions": "获取磁盘分区",

	// 监控统计
	"GET /api/v1/web/monitor/storage":       "查看存储统计",
	"GET /api/v1/web/monitor/access":        "查看访问统计",
	"GET /api/v1/web/monitor/keywords":      "查看搜索关键词统计",
	"GET /api/v1/web/monitor/recent":        "查看最近搜索词",
	"GET /api/v1/web/monitor/rankings":      "查看排行榜",
	"GET /api/v1/web/monitor/hot-downloads": "查看热门下载",

	// 认证
	"GET /api/v1/web/auth/captcha":          "获取图形验证码",
	"POST /api/v1/web/auth/register":        "用户注册",
	"POST /api/v1/web/auth/login":           "用户登录",
	"POST /api/v1/web/auth/logout":          "用户登出",
	"POST /api/v1/web/auth/refresh":         "刷新登录凭证",
	"GET /api/v1/web/auth/user":             "获取当前登录用户",
	"PUT /api/v1/web/auth/password":         "修改登录密码",
	"GET /api/v1/web/auth/oauth2/status":    "查询统一认证状态",
	"GET /api/v1/web/auth/oauth2/authorize": "发起统一认证",
	"GET /api/v1/web/auth/oauth2/callback":  "统一认证回调",

	// 个人中心
	"GET /api/v1/web/user/login-logs": "查看登录记录",
	"GET /api/v1/web/user/records":    "查看个人操作记录",
	"GET /api/v1/web/user/stats":      "查看个人存储统计",

	// 文件浏览 / 下载 / 预览
	"GET /api/v1/web/files/list":                "浏览目录",
	"GET /api/v1/web/files/recent":              "查看最近上传",
	"GET /api/v1/web/files/recent/carousel":     "查看最近上传轮播",
	"GET /api/v1/web/files/record":              "查询文件记录",
	"GET /api/v1/web/files/scan/status":         "查看扫描状态",
	"GET /api/v1/web/files/search-records":      "搜索文件记录",
	"GET /api/v1/web/files/depends/:id":         "查看文件依赖树",
	"GET /api/v1/web/files/records/:id/notes":   "读取文件备注",
	"PUT /api/v1/web/files/records/:id/notes":   "更新文件备注",
	"POST /api/v1/web/files/dependencies":       "创建文件依赖",
	"DELETE /api/v1/web/files/dependencies/:id": "删除文件依赖",
	"GET /api/v1/web/files/preview/*path":       "预览文件",
	"GET /api/v1/web/files/preview-chunk/*path": "读取预览分块",
	"POST /api/v1/web/files/checksum":           "计算文件校验值",
	"GET /api/v1/web/download/*path":            "下载文件",
	"HEAD /api/v1/web/download/*path":           "下载文件",
	"POST /api/v1/web/download/ticket":          "获取下载票据",
	"GET /api/v1/web/get_file_info":             "获取文件信息",
	"GET /api/v1/web/search/*query":             "搜索文件",
	"GET /api/v1/web/search-suggest":            "搜索联想",
	"GET /api/v1/web/search-spellcheck":         "搜索拼写纠错",
	"GET /api/v1/web/search-related":            "搜索相关词推荐",
	"GET /api/v1/web/admin/search/*query":       "管理端搜索文件",
	"GET /api/v1/web/admin/download/*path":      "管理端下载文件",
	"HEAD /api/v1/web/admin/download/*path":     "管理端下载文件",

	// 上传会话
	"POST /api/v1/web/uploads/session":                    "创建上传会话",
	"GET /api/v1/web/uploads/session/:id":                 "查询上传会话",
	"POST /api/v1/web/uploads/session/:id/resume":         "续传上传会话",
	"DELETE /api/v1/web/uploads/session/:id":              "取消上传会话",
	"POST /api/v1/web/uploads/chunk":                      "上传文件分片",
	"POST /api/v1/web/uploads/finalize":                   "完成文件上传",
	"GET /api/v1/web/uploads/sessions":                    "查看上传会话列表",
	"POST /api/v1/web/uploads/url":                        "从 URL 上传文件",
	"POST /api/v1/web/uploads/url-probe":                  "探测 URL 上传",
	"POST /api/v1/web/uploads/url_info":                   "解析 URL 文件信息",
	"GET /api/v1/web/uploads/url-task/:taskId":            "查询 URL 上传任务",
	"GET /api/v1/web/uploads/url-tasks":                   "查看 URL 上传任务列表",
	"GET /api/v1/web/uploads/url-tasks/stream":            "订阅 URL 上传进度",
	"POST /api/v1/web/uploads/url-tasks/:taskId/cancel":   "取消 URL 上传任务",
	"POST /api/v1/web/uploads/url-tasks/:taskId/retry":    "重试 URL 上传任务",
	"POST /api/v1/web/uploads/url-tasks/:taskId/checksum": "计算 URL 上传文件校验值",
	"DELETE /api/v1/web/uploads/url-tasks/:taskId":        "删除 URL 上传任务",

	// 临时文件
	"GET /api/v1/web/temp/list":                             "查看临时文件列表",
	"GET /api/v1/web/temp/quota":                            "查看临时文件配额",
	"GET /api/v1/web/temp/client-ip":                        "获取客户端 IP",
	"GET /api/v1/web/temp/:code":                            "查看临时文件信息",
	"GET /api/v1/web/temp/:code/download":                   "下载临时文件",
	"HEAD /api/v1/web/temp/:code/download":                  "下载临时文件",
	"DELETE /api/v1/web/temp/:code":                         "删除临时文件",
	"POST /api/v1/web/temp/upload/session":                  "创建临时上传会话",
	"GET /api/v1/web/temp/upload/session/:uploadId":         "查询临时上传会话",
	"POST /api/v1/web/temp/upload/session/:uploadId/resume": "续传临时上传会话",
	"DELETE /api/v1/web/temp/upload/session/:uploadId":      "取消临时上传会话",
	"POST /api/v1/web/temp/upload/chunk":                    "上传临时文件分片",
	"POST /api/v1/web/temp/upload/finalize":                 "完成临时文件上传",

	// 私有存储
	"GET /api/v1/web/private/files":                       "浏览私有文件",
	"GET /api/v1/web/private/quota":                       "查看私有存储配额",
	"DELETE /api/v1/web/private/files/:code":              "删除私有文件",
	"POST /api/v1/web/private/uploads/session":            "创建私有上传会话",
	"GET /api/v1/web/private/uploads/session/:id":         "查询私有上传会话",
	"POST /api/v1/web/private/uploads/session/:id/resume": "续传私有上传会话",
	"DELETE /api/v1/web/private/uploads/session/:id":      "取消私有上传会话",
	"POST /api/v1/web/private/uploads/chunk":              "上传私有文件分片",
	"POST /api/v1/web/private/uploads/finalize":           "完成私有文件上传",

	// 通知与初始化
	"GET /api/v1/web/notifications":            "获取通知列表",
	"POST /api/v1/web/notifications/read":      "标记通知已读",
	"POST /api/v1/web/notifications/merge":     "合并匿名通知",
	"GET /api/v1/web/setup/status":             "查询初始化状态",
	"POST /api/v1/web/setup/save":              "保存初始化配置",
	"POST /api/v1/web/setup/validate-dir":      "校验存储目录",
	"GET /api/v1/web/setup/network/interfaces": "获取网卡列表",
	"GET /api/v1/web/setup/default-config":     "获取默认配置",

	// 配置测试
	"POST /api/v1/web/config/ldap/test":     "测试 LDAP 连接",
	"POST /api/v1/web/config/oauth2/test":   "测试统一认证连接",
	"POST /api/v1/web/config/webhook/test":  "测试 Webhook 回调",
	"GET /api/v1/web/config/webhook/events": "读取 Webhook 事件目录",

	// 数据绑定示例
	"POST /api/v1/web/examples/upload":            "示例：上传文件",
	"POST /api/v1/web/examples/rename":            "示例：重命名文件",
	"POST /api/v1/web/examples/validation/upload": "示例：校验上传参数",

	// 管理端：用户与会话
	"GET /api/v1/web/admin/users":                      "查看用户列表",
	"POST /api/v1/web/admin/users":                     "创建用户",
	"PUT /api/v1/web/admin/users/:uuid/reset-password": "重置用户密码",
	"PUT /api/v1/web/admin/users/:uuid/disabled":       "启用或禁用用户",
	"DELETE /api/v1/web/admin/users/:uuid":             "删除用户",
	"GET /api/v1/web/admin/sessions":                   "查看上传会话",
	"POST /api/v1/web/admin/sessions/cleanup":          "清理上传会话",
	"POST /api/v1/web/admin/sessions/cleanup-all":      "清理僵尸上传会话",
	"GET /api/v1/web/admin/online-ips":                 "查看在线 IP",
	"POST /api/v1/web/admin/records/clear":             "清空操作记录",
	"POST /api/v1/web/admin/records/delete":            "删除操作记录",

	// 管理端：安全与告警
	"GET /api/v1/web/admin/security/events":                "查看安全事件",
	"GET /api/v1/web/admin/security/events/unread-summary": "查看未读安全事件",
	"POST /api/v1/web/admin/security/events/read":          "标记安全事件已读",
	"GET /api/v1/web/admin/security/events/stats":          "查看安全事件统计",
	"GET /api/v1/web/admin/alert-messages":                 "查看告警消息",
	"GET /api/v1/web/admin/alert-messages/unread-count":    "查看未读告警数",
	"POST /api/v1/web/admin/alert-messages/read":           "标记告警消息已读",

	// 管理端：行为分析
	"GET /api/v1/web/admin/behavior-analytics/summary":   "查看下载行为概览",
	"GET /api/v1/web/admin/behavior-analytics/trend":     "查看下载趋势",
	"GET /api/v1/web/admin/behavior-analytics/top-files": "查看热门文件",
	"GET /api/v1/web/admin/behavior-analytics/sources":   "查看下载来源",
	"GET /api/v1/web/admin/behavior-analytics/failures":  "查看下载失败分析",
	"GET /api/v1/web/admin/behavior-analytics/file":      "查看单文件行为分析",
	"GET /api/v1/web/admin/behavior-analytics/heatmap":   "查看下载热力图",
	"GET /api/v1/web/admin/behavior-analytics/aggregate": "查看行为聚合数据",
	"GET /api/v1/web/admin/behavior-analytics/lifecycle": "查看文件生命周期分析",

	// 管理端：证书与网络
	"POST /api/v1/web/admin/upload-cert":       "上传 TLS 证书",
	"POST /api/v1/web/admin/upload-key":        "上传 TLS 私钥",
	"GET /api/v1/web/admin/network-interfaces": "获取网卡列表",

	// 管理端：文件管理
	"GET /api/v1/web/admin/files/list":  "管理端浏览目录",
	"POST /api/v1/web/admin/files/move": "移动文件",
	"DELETE /api/v1/web/admin/files":    "删除文件",

	// 管理端：URL 下载任务
	"GET /api/v1/web/admin/url-tasks":            "查看 URL 下载任务",
	"POST /api/v1/web/admin/url-tasks/:id/retry": "重试 URL 下载任务",
	"DELETE /api/v1/web/admin/url-tasks/:id":     "删除 URL 下载任务",

	// 管理端：系统作业
	"GET /api/v1/web/admin/tasks":             "查看系统作业",
	"GET /api/v1/web/admin/tasks/history":     "查看作业执行历史",
	"GET /api/v1/web/admin/tasks/:id":         "查看作业详情",
	"POST /api/v1/web/admin/tasks/:id/cancel": "取消作业",

	// 管理端：API Key
	"GET /api/v1/web/admin/api-keys":            "查看 API Key 列表",
	"POST /api/v1/web/admin/api-keys":           "创建 API Key",
	"GET /api/v1/web/admin/api-keys/:id":        "查看 API Key 详情",
	"PUT /api/v1/web/admin/api-keys/:id/status": "更新 API Key 状态",
	"DELETE /api/v1/web/admin/api-keys/:id":     "删除 API Key",

	// 管理端：文件索引
	"POST /api/v1/web/admin/index/scan":                    "触发文件扫描",
	"POST /api/v1/web/admin/index/scan/trigger":            "触发全量扫描",
	"GET /api/v1/web/admin/index/scan/progress":            "查看扫描进度",
	"GET /api/v1/web/admin/index/scan/status":              "查看扫描状态",
	"GET /api/v1/web/admin/index/records":                  "查看索引记录",
	"GET /api/v1/web/admin/index/stats":                    "查看索引统计",
	"POST /api/v1/web/admin/index/check":                   "触发索引一致性检查",
	"DELETE /api/v1/web/admin/index/records/:id":           "删除索引记录",
	"GET /api/v1/web/admin/index/duplicates":               "查看重复文件",
	"POST /api/v1/web/admin/index/duplicates/:id/keep":     "保留指定重复文件",
	"PUT /api/v1/web/admin/index/records/:id/notes":        "更新索引记录备注",
	"GET /api/v1/web/admin/index/dependencies":             "查看文件依赖列表",
	"POST /api/v1/web/admin/index/dependencies":            "创建文件依赖",
	"GET /api/v1/web/admin/index/records/:id/dependencies": "查看文件依赖",
	"DELETE /api/v1/web/admin/index/dependencies/:id":      "删除文件依赖",

	// 管理端：OpenAPI 统计
	"GET /api/v1/web/admin/open-api/stats":  "查看开放 API 调用统计",
	"GET /api/v1/web/admin/open-api/scopes": "查看开放 API 权限目录",

	// 管理端：日志与资源监控
	"GET /api/v1/web/admin/logs/files":        "查看日志文件列表",
	"GET /api/v1/web/admin/logs/tail":         "读取日志内容",
	"GET /api/v1/web/admin/logs/stream":       "订阅日志流",
	"GET /api/v1/web/admin/resource/snapshot": "查看服务器资源快照",
	"GET /api/v1/web/admin/resource/history":  "查看服务器资源历史",
	"GET /api/v1/web/admin/resource/stream":   "订阅服务器资源流",
}

// webActionLabel 返回 Web 渠道请求的业务操作名，用于访问日志的 message。
// 未登记的路由回退到通用名称，路由详情仍由 action 字段记录。
func webActionLabel(method, route string) string {
	if label, ok := webRouteActions[method+" "+route]; ok {
		return label
	}
	return webActionFallback
}
