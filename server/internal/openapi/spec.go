package openapi

// GenerateOpenAPISpec 生成 OpenAPI 3.x 规范文档
func GenerateOpenAPISpec() map[string]interface{} {
	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title": "Fuzhan (浮栈) Open API",
			"description": "浮栈对外 API，提供文件列表、搜索、下载、详情、上传、重复文件检测等功能。\n\n" +
				"## 认证\n\n" +
				"所有接口都需要有效凭证，没有匿名接口：\n\n" +
				"- **API Key（推荐）**：通过 `Authorization: Bearer <keyId>:<secret>` 请求头传递，需具备对应接口的 scope。\n" +
				"- **Web 会话**：浏览器已登录时自动携带 `fuzhan_token` Cookie。仅 `auth_mode=none` 时生效，通过后按 `open_api:writer` 兼容层级授权，可访问全部数据端点（与加固前的公开行为一致）；" +
				"`auth_mode=api_key` 下 Web 会话无效，必须使用 API Key。\n\n" +
				"## 响应格式\n\n" +
				"- 业务成功/失败使用统一信封：`{success, code, message, data}` / `{success, code, message, errors}`。\n" +
				"- 鉴权、scope、IP 白名单等中间件拒绝时返回简单结构：`{\"error\": \"...\"}`。\n" +
				"- `GET /files/download/{path}` 成功时直接返回文件二进制流，不使用信封。\n\n" +
				"## 通用拒绝响应\n\n" +
				"- `401`：缺少或无效凭证（简单结构）。\n" +
				"- `403`：scope 不足（简单结构），或上传关闭/目录不允许上传等业务拒绝（信封结构）。\n" +
				"- `404`：功能未开启（信封结构），或文件/记录/会话不存在（信封结构）。\n" +
				"- `429`：触发频率限制（信封结构，带 `Retry-After` 与 `X-RateLimit-Limit` 响应头）。",
			"version": "1.0.0",
		},
		"servers": []map[string]interface{}{
			{"url": "/api/open/v1", "description": "Open API v1"},
		},
		"tags": []map[string]interface{}{
			{"name": "文件操作", "description": "文件列表、搜索、下载与单请求上传"},
			{"name": "文件信息", "description": "文件详情、备注、依赖与重复文件查询"},
			{"name": "分片上传", "description": "大文件分片上传（断点续传）。需 `open_api:files:upload` 权限。\n\n" +
				"## 调用流程\n" +
				"1. `POST /files/upload/init`：提交文件名、文件总大小与目标目录，获取 `uploadId`、`chunkSize`、`totalChunks`\n" +
				"2. `POST /files/upload/chunk`：按 `chunkIndex`（从 0 开始）逐片上传分片；可分片乱序、可重传覆盖单片、可随时补传缺失分片\n" +
				"3. `POST /files/upload/finalize`：全部分片上传完成后调用，服务端按序合并、落盘、同步索引并写入上传记录\n" +
				"4. （可选）`GET /files/upload/{id}`：查询 `uploadedIndexes` 与过期时间，实现断点续传\n\n" +
				"注意事项：\n" +
				"- 上传会话 24 小时过期，过期后需重新 init\n" +
				"- 目标文件已存在时，仅上传者同一 IP 可覆盖（与 Web 公共目录上传一致）\n" +
				"- 小文件可直接使用 `POST /files/upload` 单请求上传，无需分片"},
			{"name": "系统", "description": "OpenAPI 规范与文档页面。同样需要有效凭证，但不要求 API Key scope"},
		},
		"security": sessionOrAPIKeySecurity(),
		"paths": map[string]interface{}{
			"/files/list": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":          "文件列表",
					"description":      "基于文件索引表，以分页方式获取指定目录的文件列表。\n\n所需 scope：`open_api:reader`",
					"tags":             []string{"文件操作"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:reader",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "description": "目录路径：`/` 列出所有根目录；`/rootName` 列出根目录；`/rootName/subdir` 列出子目录", "schema": map[string]interface{}{"type": "string", "default": "/", "example": "/docs"}},
						{"name": "page", "in": "query", "description": "页码，从 1 开始", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "default": 1}},
						{"name": "pageSize", "in": "query", "description": "每页数量，非法值回退为 50", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100, "default": 50}},
						{"name": "sort", "in": "query", "description": "排序字段", "schema": map[string]interface{}{"type": "string", "enum": []string{"name", "time", "size"}, "default": "name"}},
						{"name": "order", "in": "query", "description": "排序方向", "schema": map[string]interface{}{"type": "string", "enum": []string{"asc", "desc"}, "default": "asc"}},
					},
					"responses": map[string]interface{}{
						"200": successResponse("目录文件列表", objectSchema(map[string]interface{}{
							"path":       map[string]interface{}{"type": "string", "description": "请求时的目录路径（已规范化为 /）", "example": "/docs"},
							"files":      arraySchema(schemaRef("PublicFileItem")),
							"pagination": schemaRef("Pagination"),
						}, "path", "files", "pagination")),
						"400": errorResponse("请求参数格式错误"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("API Key 缺少 open_api:reader scope"),
						"404": errorResponse("Open API 功能未开启"),
						"429": errorResponse("请求过于频繁"),
					},
				},
			},
			"/files/search": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":          "文件搜索",
					"description":      "根据关键词搜索文件（匹配文件名和备注）。\n\n所需 scope：`open_api:reader`",
					"tags":             []string{"文件操作"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:reader",
					"parameters": []map[string]interface{}{
						{"name": "query", "in": "query", "required": true, "description": "搜索关键词", "schema": map[string]interface{}{"type": "string", "example": "report"}},
						{"name": "root", "in": "query", "description": "限定搜索的根目录名称（不是完整路径）", "schema": map[string]interface{}{"type": "string", "example": "docs"}},
						{"name": "page", "in": "query", "description": "页码，从 1 开始", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "default": 1}},
						{"name": "pageSize", "in": "query", "description": "每页数量，非法值回退为 50", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100, "default": 50}},
					},
					"responses": map[string]interface{}{
						"200": successResponse("搜索结果", objectSchema(map[string]interface{}{
							"results":    arraySchema(schemaRef("SearchFileItem")),
							"pagination": schemaRef("Pagination"),
						}, "results", "pagination")),
						"400": errorResponse("缺少搜索关键词 query"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("API Key 缺少 open_api:reader scope"),
						"404": errorResponse("Open API 功能未开启"),
						"429": errorResponse("请求过于频繁"),
					},
				},
			},
			"/files/download/{path}": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":          "文件下载",
					"description":      "通过路径下载文件。路径不带前导 `/`，格式为 `rootName/path/to/file`（等价于完整路径 `/rootName/path/to/file`）。危险类型（HTML/SVG/XML/JS 等）即使请求 inline 也会强制按附件下载。\n\n所需 scope：`open_api:reader`",
					"tags":             []string{"文件操作"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:reader",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "path", "required": true, "allowReserved": true, "description": "文件路径，必须包含根目录名，不带前导 /", "schema": map[string]interface{}{"type": "string", "example": "docs/report.pdf"}},
						{"name": "disposition", "in": "query", "description": "下载方式；危险类型会被强制改为 attachment", "schema": map[string]interface{}{"type": "string", "enum": []string{"attachment", "inline"}, "default": "attachment"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "文件二进制流",
							"headers": map[string]interface{}{
								"Content-Disposition": map[string]interface{}{"description": "下载方式与文件名（含 RFC 5987 filename*）", "schema": map[string]interface{}{"type": "string"}},
								"Content-Type":        map[string]interface{}{"description": "配置的下载 MIME，未配置时按扩展名推断", "schema": map[string]interface{}{"type": "string"}},
							},
							"content": map[string]interface{}{
								"application/octet-stream": map[string]interface{}{"schema": map[string]interface{}{"type": "string", "format": "binary"}},
							},
						},
						"400": errorResponse("缺少文件路径、路径格式错误、不能下载目录或 disposition 非法"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("API Key 缺少 open_api:reader scope"),
						"404": errorResponse("文件不存在或根目录配置不存在"),
						"429": errorResponse("请求过于频繁"),
					},
				},
			},
			"/files/info": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":          "文件详情",
					"description":      "根据文件记录 ID 或完整路径 `/rootName/path/to/file` 获取单个文件/目录的详细信息。\n\n所需 scope：`open_api:files:info`（兼容 `open_api:reader` / `open_api:writer`）",
					"tags":             []string{"文件信息"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:info",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "query", "description": "文件记录 ID（与 path 二选一）", "schema": map[string]interface{}{"type": "integer", "format": "int64", "minimum": 1, "example": 1}},
						{"name": "path", "in": "query", "description": "完整路径（与 id 二选一），如 `/docs/report.pdf`", "schema": map[string]interface{}{"type": "string", "example": "/docs/report.pdf"}},
					},
					"responses": map[string]interface{}{
						"200": successResponse("文件详情", schemaRef("FileDetail")),
						"400": errorResponse("缺少查询条件、ID 格式错误或路径格式错误"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("API Key 缺少 open_api:files:info scope"),
						"404": errorResponse("文件记录不存在或 Open API 功能未开启"),
						"429": errorResponse("请求过于频繁"),
					},
				},
			},
			"/files/upload": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":          "文件上传",
					"description":      "以 multipart/form-data 上传单个文件到指定公共目录（适合小文件）；大文件请使用「分片上传」分组的接口。目标已存在时仅上传者同一 IP 且传 overwrite=true 才会覆盖。\n\n所需 scope：`open_api:files:upload`（兼容 `open_api:writer`）",
					"tags":             []string{"文件操作"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:upload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"multipart/form-data": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"file"},
									"properties": map[string]interface{}{
										"file":      map[string]interface{}{"type": "string", "format": "binary", "description": "上传的文件"},
										"path":      map[string]interface{}{"type": "string", "description": "目标目录，格式 /rootName[/subdir]，缺省为 /", "example": "/docs"},
										"overwrite": map[string]interface{}{"type": "boolean", "description": "目标已存在时是否覆盖（仅上传者同一 IP 有效）", "default": false},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": successResponse("上传成功", schemaRef("UploadResult")),
						"400": errorResponse("参数 / 文件名 / 类型 / 路径错误"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("上传功能关闭、目录不允许上传或无覆盖权限"),
						"404": errorResponse("根目录不存在"),
						"409": errorResponse("目标文件已存在且未允许覆盖"),
						"413": errorResponse("文件大小超出限制"),
						"429": errorResponse("请求过于频繁"),
					},
				},
			},
			"/files/upload/init": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":          "创建分片上传会话",
					"description":      "大文件分片上传第 1 步：提交文件名、总大小与目标目录，返回 uploadId、chunkSize、totalChunks；会话 24 小时过期。\n\n所需 scope：`open_api:files:upload`",
					"tags":             []string{"分片上传"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:upload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"filename", "fileSize"},
									"properties": map[string]interface{}{
										"filename": map[string]interface{}{"type": "string", "maxLength": 255, "description": "文件名", "example": "report.pdf"},
										"fileSize": map[string]interface{}{"type": "integer", "format": "int64", "minimum": 1, "description": "文件总大小（字节）", "example": 104857600},
										"path":     map[string]interface{}{"type": "string", "description": "目标目录，格式 /rootName[/subdir]，缺省为 /", "example": "/docs"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"201": successResponse("会话创建成功", schemaRef("UploadInitResult")),
						"400": errorResponse("参数错误或超出大小限制"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("上传功能关闭或目录不允许上传"),
						"404": errorResponse("根目录不存在"),
						"429": errorResponse("请求过于频繁"),
					},
				},
			},
			"/files/upload/chunk": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":          "上传分片",
					"description":      "大文件分片上传第 2 步：按 uploadId 上传指定索引的分片；分片可乱序上传，重复上传同一索引会覆盖该片。\n\n所需 scope：`open_api:files:upload`",
					"tags":             []string{"分片上传"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:upload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"multipart/form-data": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"uploadId", "chunkIndex", "chunk"},
									"properties": map[string]interface{}{
										"uploadId":   map[string]interface{}{"type": "integer", "format": "int64", "description": "上传会话 ID", "example": 1},
										"chunkIndex": map[string]interface{}{"type": "integer", "minimum": 0, "description": "分片索引，从 0 开始", "example": 0},
										"chunk":      map[string]interface{}{"type": "string", "format": "binary", "description": "分片数据"},
										"checksum":   map[string]interface{}{"type": "string", "description": "分片 xxh3 十六进制校验值（可选，传则校验）", "example": "0123456789abcdef"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": successResponse("分片上传成功", schemaRef("UploadChunkResult")),
						"400": errorResponse("会话 ID / 分片索引 / 分片文件无效"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("无权操作该上传会话"),
						"404": errorResponse("上传会话不存在"),
						"413": errorResponse("分片大小超出限制"),
						"429": errorResponse("请求过于频繁"),
						"500": errorResponse("分片写入或校验失败"),
					},
				},
			},
			"/files/upload/finalize": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":          "完成分片上传",
					"description":      "大文件分片上传第 3 步：全部分片上传完成后调用，服务端按序合并落盘、同步索引并写入上传记录；目标已存在时仅上传者同一 IP 可覆盖（与 Web 公共目录上传一致）。\n\n所需 scope：`open_api:files:upload`",
					"tags":             []string{"分片上传"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:upload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"uploadId"},
									"properties": map[string]interface{}{
										"uploadId": map[string]interface{}{"type": "integer", "format": "int64", "description": "上传会话 ID", "example": 1},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": successResponse("上传完成，返回文件记录 ID 与完整路径", schemaRef("UploadFinalizeResult")),
						"400": errorResponse("请求数据格式错误或会话 ID 无效"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("无权操作该上传会话"),
						"404": errorResponse("上传会话不存在"),
						"409": errorResponse("目标文件已存在且未允许覆盖"),
						"429": errorResponse("请求过于频繁"),
						"500": errorResponse("分片不完整或落盘失败"),
					},
				},
			},
			"/files/upload/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":          "查询分片上传会话",
					"description":      "可选步骤：查询会话状态、expiredAt 与 uploadedIndexes，用于断点续传（跳过已上传的分片）。\n\n所需 scope：`open_api:files:upload`",
					"tags":             []string{"分片上传"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:upload",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "上传会话 ID", "schema": map[string]interface{}{"type": "integer", "format": "int64", "minimum": 1, "example": 1}},
					},
					"responses": map[string]interface{}{
						"200": successResponse("会话状态", schemaRef("UploadStatusResult")),
						"400": errorResponse("无效的会话 ID"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("无权操作该上传会话"),
						"404": errorResponse("上传会话不存在"),
						"429": errorResponse("请求过于频繁"),
					},
				},
			},
			"/files/duplicates": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":          "重复文件查询",
					"description":      "基于 xxh3 哈希值检测重复文件，按分组返回。`pagination.total` 为按 minSize 过滤后的重复分组总数，与 groups 一致；每组 `fileCount`/`totalSize`/`files` 也仅统计大小 ≥ minSize 的文件。\n\n所需 scope：`open_api:files:duplicates`（兼容 `open_api:reader` / `open_api:writer`）",
					"tags":             []string{"文件信息"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:duplicates",
					"parameters": []map[string]interface{}{
						{"name": "minSize", "in": "query", "description": "最小文件大小（字节），小于 0 时按 0 处理", "schema": map[string]interface{}{"type": "integer", "format": "int64", "minimum": 0, "default": 0}},
						{"name": "page", "in": "query", "description": "页码，从 1 开始", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "default": 1}},
						{"name": "pageSize", "in": "query", "description": "每页分组数量，非法值回退为 50", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100, "default": 50}},
					},
					"responses": map[string]interface{}{
						"200": successResponse("重复文件分组", objectSchema(map[string]interface{}{
							"groups":     arraySchema(schemaRef("DuplicateGroup")),
							"pagination": schemaRef("Pagination"),
						}, "groups", "pagination")),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("API Key 缺少 open_api:files:duplicates scope"),
						"429": errorResponse("请求过于频繁"),
						"500": errorResponse("查询重复文件失败"),
					},
				},
			},
			"/files/notes": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":          "文件备注查询",
					"description":      "获取指定文件的备注信息。\n\n所需 scope：`open_api:files:notes`（兼容 `open_api:reader` / `open_api:writer`）",
					"tags":             []string{"文件信息"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:notes",
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "description": "文件完整路径 `/rootName/path/to/file`", "schema": map[string]interface{}{"type": "string", "example": "/docs/report.pdf"}},
					},
					"responses": map[string]interface{}{
						"200": successResponse("文件备注", schemaRef("NotesResult")),
						"400": errorResponse("缺少文件路径或路径格式错误"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("API Key 缺少 open_api:files:notes scope"),
						"404": errorResponse("文件不存在"),
						"429": errorResponse("请求过于频繁"),
					},
				},
			},
			"/files/{id}/depends": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":          "文件依赖树查询",
					"description":      "基于文件记录 ID 查询文件的依赖树：上游递归（最多 10 层），下游仅一层。\n\n所需 scope：`open_api:files:depends`（兼容 `open_api:reader` / `open_api:writer`）",
					"tags":             []string{"文件信息"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:depends",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "文件记录 ID", "schema": map[string]interface{}{"type": "integer", "format": "int64", "minimum": 1, "example": 1}},
					},
					"responses": map[string]interface{}{
						"200": successResponse("依赖树", objectSchema(map[string]interface{}{
							"root": schemaRef("DependencyTreeNode"),
						}, "root")),
						"400": errorResponse("无效的记录 ID 或查询依赖树失败（如循环依赖、层级过深）"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("API Key 缺少 open_api:files:depends scope"),
						"404": errorResponse("文件记录不存在"),
						"429": errorResponse("请求过于频繁"),
					},
				},
			},
			"/openapi.json": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "OpenAPI 规范文档",
					"description": "返回 OpenAPI 3.x 规范 JSON。响应未被信封包裹。需要有效凭证（Web 会话 Cookie 或 API Key）但不需要 scope。",
					"tags":        []string{"系统"},
					"security":    sessionOrAPIKeySecurity(),
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "OpenAPI 规范",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{"type": "object", "additionalProperties": true, "description": "OpenAPI 3.0.3 文档对象"},
								},
							},
						},
						"401": guardResponse("缺少或无效凭证"),
						"404": errorResponse("Open API 功能未开启"),
						"429": errorResponse("请求过于频繁"),
						"503": guardResponse("Open API 鉴权服务不可用"),
					},
				},
			},
			"/docs": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "API 文档页面",
					"description": "301 重定向到 `/scalar.html`（Scalar 交互式 API 文档页面）。需要有效凭证，Scalar 静态页本身匿名可加载。",
					"tags":        []string{"系统"},
					"security":    sessionOrAPIKeySecurity(),
					"responses": map[string]interface{}{
						"301": map[string]interface{}{
							"description": "重定向到 /scalar.html",
							"headers": map[string]interface{}{
								"Location": map[string]interface{}{"description": "重定向目标", "schema": map[string]interface{}{"type": "string", "example": "/scalar.html"}},
							},
						},
						"401": guardResponse("缺少或无效凭证"),
						"404": errorResponse("Open API 功能未开启"),
						"429": errorResponse("请求过于频繁"),
						"503": guardResponse("Open API 鉴权服务不可用"),
					},
				},
			},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"ApiKeyAuth": map[string]interface{}{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "keyId:secret",
					"description":  "系统签发的 API Key，格式 `keyId:secret`，通过 `Authorization: Bearer <keyId>:<secret>` 传递。凭证需具备接口要求的 scope。",
				},
				"WebSessionCookie": map[string]interface{}{
					"type":        "apiKey",
					"in":          "cookie",
					"name":        "fuzhan_token",
					"description": "Web 登录会话 JWT，Cookie 名 `fuzhan_token`。仅 `auth_mode=none` 时被接受（含强制登录场景），通过后按 `open_api:writer` 兼容层级授权；`auth_mode=api_key` 下无效。",
				},
			},
			"schemas": componentSchemas(),
		},
	}
}

// dataEndpointSecurity 数据端点的安全要求：none 模式接受 Web 会话或 API Key，
// api_key 模式只接受 API Key（模式差异在 info.description 中说明）。
func dataEndpointSecurity() []map[string]interface{} {
	return []map[string]interface{}{
		{"ApiKeyAuth": []string{}},
		{"WebSessionCookie": []string{}},
	}
}

// sessionOrAPIKeySecurity 文档端点的安全要求：Web 会话或 API Key。
func sessionOrAPIKeySecurity() []map[string]interface{} {
	return []map[string]interface{}{
		{"ApiKeyAuth": []string{}},
		{"WebSessionCookie": []string{}},
	}
}

// schemaRef 引用 components.schemas 中的定义。
func schemaRef(name string) map[string]interface{} {
	return map[string]interface{}{"$ref": "#/components/schemas/" + name}
}

// objectSchema 构造对象 schema，required 为空时不写 required。
func objectSchema(properties map[string]interface{}, required ...string) map[string]interface{} {
	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// arraySchema 构造数组 schema。
func arraySchema(items map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{"type": "array", "items": items}
}

// jsonContent 把 schema 包成 application/json content。
func jsonContent(schema map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"application/json": map[string]interface{}{"schema": schema},
	}
}

// successResponse 构造统一信封的成功响应。
func successResponse(description string, dataSchema map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"description": description,
		"content": jsonContent(objectSchema(map[string]interface{}{
			"success": map[string]interface{}{"type": "boolean", "example": true},
			"code":    map[string]interface{}{"type": "integer", "description": "业务码，0 表示成功", "example": 0},
			"message": map[string]interface{}{"type": "string", "example": ""},
			"data":    dataSchema,
		}, "success", "code", "message", "data")),
	}
}

// errorResponse 构造统一信封的失败响应。
func errorResponse(description string) map[string]interface{} {
	return map[string]interface{}{
		"description": description,
		"content":     jsonContent(schemaRef("ErrorResponse")),
	}
}

// guardResponse 构造中间件拒绝的简单错误响应。
func guardResponse(description string) map[string]interface{} {
	return map[string]interface{}{
		"description": description,
		"content":     jsonContent(schemaRef("GuardError")),
	}
}

// mixedErrorResponse 构造可能为信封或简单结构的 403 响应。
func mixedErrorResponse(description string) map[string]interface{} {
	return map[string]interface{}{
		"description": description,
		"content": jsonContent(map[string]interface{}{
			"oneOf": []map[string]interface{}{
				schemaRef("ErrorResponse"),
				schemaRef("GuardError"),
			},
		}),
	}
}

// componentSchemas 定义全部响应数据模型。
func componentSchemas() map[string]interface{} {
	return map[string]interface{}{
		"Pagination": objectSchema(map[string]interface{}{
			"page":      map[string]interface{}{"type": "integer", "description": "当前页码", "example": 1},
			"page_size": map[string]interface{}{"type": "integer", "description": "每页数量", "example": 50},
			"total":     map[string]interface{}{"type": "integer", "format": "int64", "description": "符合条件的总记录数", "example": 120},
		}, "page", "page_size", "total"),

		"PublicFileItem": objectSchema(map[string]interface{}{
			"name":      map[string]interface{}{"type": "string", "description": "文件名", "example": "report.pdf"},
			"path":      map[string]interface{}{"type": "string", "description": "完整路径 /rootName/...", "example": "/docs/report.pdf"},
			"full_path": map[string]interface{}{"type": "string", "description": "完整路径，与 path 相同", "example": "/docs/report.pdf"},
			"root":      map[string]interface{}{"type": "string", "description": "根目录名称", "example": "docs"},
			"size":      map[string]interface{}{"type": "integer", "format": "int64", "description": "文件大小（字节），目录为 0", "example": 10240},
			"is_dir":    map[string]interface{}{"type": "boolean", "description": "是否为目录", "example": false},
			"mod_time":  map[string]interface{}{"type": "string", "format": "date-time", "description": "文件修改时间", "example": "2026-11-03T10:00:00+08:00"},
			"xxh3_hash": map[string]interface{}{"type": "string", "description": "xxh3 哈希值，未计算时为空", "example": "0123456789abcdef"},
		}, "name", "path", "full_path", "root", "size", "is_dir", "mod_time", "xxh3_hash"),

		"SearchFileItem": objectSchema(map[string]interface{}{
			"id":        map[string]interface{}{"type": "integer", "format": "int64", "description": "文件记录 ID", "example": 1},
			"name":      map[string]interface{}{"type": "string", "description": "文件名", "example": "report.pdf"},
			"path":      map[string]interface{}{"type": "string", "description": "完整路径 /rootName/...", "example": "/docs/report.pdf"},
			"full_path": map[string]interface{}{"type": "string", "description": "完整路径，与 path 相同", "example": "/docs/report.pdf"},
			"root":      map[string]interface{}{"type": "string", "description": "根目录名称", "example": "docs"},
			"size":      map[string]interface{}{"type": "integer", "format": "int64", "description": "文件大小（字节），目录为 0", "example": 10240},
			"is_dir":    map[string]interface{}{"type": "boolean", "description": "是否为目录", "example": false},
			"mod_time":  map[string]interface{}{"type": "string", "format": "date-time", "description": "文件修改时间", "example": "2026-11-03T10:00:00+08:00"},
			"xxh3_hash": map[string]interface{}{"type": "string", "description": "xxh3 哈希值，未计算时为空", "example": "0123456789abcdef"},
		}, "id", "name", "path", "full_path", "root", "size", "is_dir", "mod_time", "xxh3_hash"),

		"FileDetail": objectSchema(map[string]interface{}{
			"id":             map[string]interface{}{"type": "integer", "format": "int64", "description": "文件记录 ID", "example": 1},
			"name":           map[string]interface{}{"type": "string", "description": "文件名", "example": "report.pdf"},
			"path":           map[string]interface{}{"type": "string", "description": "完整路径 /rootName/...", "example": "/docs/report.pdf"},
			"full_path":      map[string]interface{}{"type": "string", "description": "完整路径，与 path 相同", "example": "/docs/report.pdf"},
			"root":           map[string]interface{}{"type": "string", "description": "根目录名称", "example": "docs"},
			"file_path":      map[string]interface{}{"type": "string", "description": "根目录内的相对路径", "example": "/report.pdf"},
			"size":           map[string]interface{}{"type": "integer", "format": "int64", "description": "文件大小（字节）", "example": 10240},
			"is_dir":         map[string]interface{}{"type": "boolean", "description": "是否为目录", "example": false},
			"mod_time":       map[string]interface{}{"type": "string", "format": "date-time", "description": "文件修改时间", "example": "2026-11-03T10:00:00+08:00"},
			"xxh3_hash":      map[string]interface{}{"type": "string", "description": "xxh3 哈希值，未计算时为空", "example": "0123456789abcdef"},
			"hash_status":    map[string]interface{}{"type": "string", "enum": []string{"pending", "done", "failed"}, "description": "哈希计算状态", "example": "done"},
			"notes":          map[string]interface{}{"type": "string", "description": "文件备注", "example": ""},
			"download_count": map[string]interface{}{"type": "integer", "format": "int64", "description": "下载次数", "example": 3},
			"status":         map[string]interface{}{"type": "string", "enum": []string{"active", "deleted"}, "description": "文件记录状态", "example": "active"},
			"created_at":     map[string]interface{}{"type": "string", "format": "date-time", "description": "记录创建时间", "example": "2026-11-03T10:00:00+08:00"},
			"updated_at":     map[string]interface{}{"type": "string", "format": "date-time", "description": "记录更新时间", "example": "2026-11-03T10:00:00+08:00"},
		}, "id", "name", "path", "full_path", "root", "file_path", "size", "is_dir", "mod_time", "xxh3_hash", "hash_status", "notes", "download_count", "status", "created_at", "updated_at"),

		"UploadResult": objectSchema(map[string]interface{}{
			"id":          map[string]interface{}{"type": "integer", "format": "int64", "description": "上传操作记录 ID（索引同步失败时可能为 0）", "example": 1},
			"name":        map[string]interface{}{"type": "string", "description": "文件名", "example": "report.pdf"},
			"path":        map[string]interface{}{"type": "string", "description": "完整路径", "example": "/docs/report.pdf"},
			"root":        map[string]interface{}{"type": "string", "description": "根目录名称", "example": "docs"},
			"size":        map[string]interface{}{"type": "integer", "format": "int64", "description": "文件大小（字节）", "example": 10240},
			"full_path":   map[string]interface{}{"type": "string", "description": "完整路径，与 path 相同", "example": "/docs/report.pdf"},
			"overwritten": map[string]interface{}{"type": "boolean", "description": "是否覆盖了已存在的文件", "example": false},
		}, "id", "name", "path", "root", "size", "full_path", "overwritten"),

		"UploadInitResult": objectSchema(map[string]interface{}{
			"uploadId":    map[string]interface{}{"type": "integer", "format": "int64", "description": "上传会话 ID", "example": 1},
			"chunkSize":   map[string]interface{}{"type": "integer", "format": "int64", "description": "分片大小（字节）", "example": 5242880},
			"totalChunks": map[string]interface{}{"type": "integer", "description": "分片总数", "example": 20},
			"expiredAt":   map[string]interface{}{"type": "string", "format": "date-time", "description": "会话过期时间", "example": "2026-11-04T10:00:00+08:00"},
			"root":        map[string]interface{}{"type": "string", "description": "目标根目录名称", "example": "docs"},
			"path":        map[string]interface{}{"type": "string", "description": "目标目录路径", "example": "/docs"},
		}, "uploadId", "chunkSize", "totalChunks", "expiredAt", "root", "path"),

		"UploadChunkResult": objectSchema(map[string]interface{}{
			"chunkIndex": map[string]interface{}{"type": "integer", "description": "已接收的分片索引", "example": 0},
		}, "chunkIndex"),

		"UploadFinalizeResult": objectSchema(map[string]interface{}{
			"id":        map[string]interface{}{"type": "integer", "format": "int64", "description": "文件记录 ID；索引同步后可能为 0", "example": 1},
			"name":      map[string]interface{}{"type": "string", "description": "文件名", "example": "report.pdf"},
			"path":      map[string]interface{}{"type": "string", "description": "完整路径", "example": "/docs/report.pdf"},
			"full_path": map[string]interface{}{"type": "string", "description": "完整路径，与 path 相同", "example": "/docs/report.pdf"},
			"root":      map[string]interface{}{"type": "string", "description": "根目录名称", "example": "docs"},
			"size":      map[string]interface{}{"type": "integer", "format": "int64", "description": "文件大小（字节）", "example": 104857600},
		}, "id", "name", "path", "full_path", "root", "size"),

		"UploadStatusResult": objectSchema(map[string]interface{}{
			"uploadId":        map[string]interface{}{"type": "integer", "format": "int64", "description": "上传会话 ID", "example": 1},
			"fileName":        map[string]interface{}{"type": "string", "description": "文件名", "example": "report.pdf"},
			"fileSize":        map[string]interface{}{"type": "integer", "format": "int64", "description": "文件总大小（字节）", "example": 104857600},
			"chunkSize":       map[string]interface{}{"type": "integer", "format": "int64", "description": "分片大小（字节）", "example": 5242880},
			"totalChunks":     map[string]interface{}{"type": "integer", "description": "分片总数", "example": 20},
			"uploadedIndexes": arraySchema(map[string]interface{}{"type": "integer", "description": "已上传的分片索引", "example": 0}),
			"status":          map[string]interface{}{"type": "string", "enum": []string{"pending", "in_progress", "expired", "completed", "failed", "cancelled"}, "description": "会话状态", "example": "in_progress"},
			"expired":         map[string]interface{}{"type": "boolean", "description": "会话是否已过期", "example": false},
			"expiredAt":       map[string]interface{}{"type": "string", "format": "date-time", "description": "会话过期时间", "example": "2026-11-04T10:00:00+08:00"},
		}, "uploadId", "fileName", "fileSize", "chunkSize", "totalChunks", "uploadedIndexes", "status", "expired", "expiredAt"),

		"NotesResult": objectSchema(map[string]interface{}{
			"path":  map[string]interface{}{"type": "string", "description": "规范化后的文件完整路径", "example": "/docs/report.pdf"},
			"notes": map[string]interface{}{"type": "string", "description": "文件备注，无备注时为空字符串", "example": "季度报告"},
		}, "path", "notes"),

		"DuplicateFileItem": objectSchema(map[string]interface{}{
			"id":       map[string]interface{}{"type": "integer", "format": "int64", "description": "文件记录 ID", "example": 1},
			"fileName": map[string]interface{}{"type": "string", "description": "文件名", "example": "report.pdf"},
			"filePath": map[string]interface{}{"type": "string", "description": "根目录内的相对路径", "example": "/report.pdf"},
			"fullPath": map[string]interface{}{"type": "string", "description": "完整路径", "example": "/docs/report.pdf"},
			"rootName": map[string]interface{}{"type": "string", "description": "根目录名称", "example": "docs"},
			"fileSize": map[string]interface{}{"type": "integer", "format": "int64", "description": "文件大小（字节）", "example": 10240},
			"modTime":  map[string]interface{}{"type": "string", "format": "date-time", "description": "文件修改时间", "example": "2026-11-03T10:00:00+08:00"},
		}, "id", "fileName", "filePath", "fullPath", "rootName", "fileSize", "modTime"),

		"DuplicateGroup": objectSchema(map[string]interface{}{
			"xxh3Hash":  map[string]interface{}{"type": "string", "description": "分组哈希值", "example": "0123456789abcdef"},
			"fileCount": map[string]interface{}{"type": "integer", "format": "int64", "description": "组内文件数量", "example": 2},
			"totalSize": map[string]interface{}{"type": "integer", "format": "int64", "description": "组内文件总大小（字节）", "example": 20480},
			"files":     arraySchema(schemaRef("DuplicateFileItem")),
		}, "xxh3Hash", "fileCount", "totalSize", "files"),

		"DependencyTreeNode": objectSchema(map[string]interface{}{
			"id":           map[string]interface{}{"type": "integer", "format": "int64", "description": "文件记录 ID", "example": 1},
			"dependencyId": map[string]interface{}{"type": "integer", "format": "int64", "description": "依赖关系 ID（用于删除），根节点为 0", "example": 1},
			"fileName":     map[string]interface{}{"type": "string", "description": "文件名", "example": "main.pdf"},
			"filePath":     map[string]interface{}{"type": "string", "description": "根目录内的相对路径", "example": "/main.pdf"},
			"fullPath":     map[string]interface{}{"type": "string", "description": "完整路径", "example": "/docs/main.pdf"},
			"rootName":     map[string]interface{}{"type": "string", "description": "根目录名称", "example": "docs"},
			"fileSize":     map[string]interface{}{"type": "integer", "format": "int64", "description": "文件大小（字节）", "example": 10240},
			"isDir":        map[string]interface{}{"type": "boolean", "description": "是否为目录", "example": false},
			"relation":     map[string]interface{}{"type": "string", "enum": []string{"", "requires", "referenced_by", "related"}, "description": "依赖关系类型；根节点为空字符串", "example": "requires"},
			"downloadURL":  map[string]interface{}{"type": "string", "description": "Web 下载 URL；根节点为空字符串", "example": "/download/docs/main.pdf"},
			"children":     arraySchema(schemaRef("DependencyTreeNode")),
			"downstream":   arraySchema(schemaRef("DependencyTreeNode")),
		}, "id", "dependencyId", "fileName", "filePath", "fullPath", "rootName", "fileSize", "isDir", "relation", "downloadURL", "children", "downstream"),

		"ErrorResponse": objectSchema(map[string]interface{}{
			"success": map[string]interface{}{"type": "boolean", "example": false},
			"code":    map[string]interface{}{"type": "integer", "description": "业务错误码（1004 参数错误 / 2004 禁止 / 3001 不存在 / 5001 内部错误等）", "example": 1004},
			"message": map[string]interface{}{"type": "string", "description": "错误描述", "example": "请求参数格式错误"},
			"errors":  map[string]interface{}{"description": "可选的详细错误信息，类型不固定"},
			"data":    map[string]interface{}{"description": "可选的附加数据，类型不固定"},
		}, "success", "code", "message"),

		"GuardError": objectSchema(map[string]interface{}{
			"error": map[string]interface{}{"type": "string", "description": "鉴权/权限/IP/限流等中间件拒绝原因", "example": "需要有效凭证（Web 会话 JWT 或 API Key）"},
		}, "error"),
	}
}
