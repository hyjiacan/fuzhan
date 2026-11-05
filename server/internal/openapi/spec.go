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
				"- 响应数据字段统一使用 `snake_case`（如 `file_size`、`page_size`、`upload_id`）。\n" +
				"- 鉴权、scope、IP 白名单等中间件拒绝时返回简单结构：`{\"error\": \"...\"}`。\n" +
				"- `GET /files/download/{path}` 成功时直接返回文件二进制流，不使用信封。\n\n" +
				"## 参数校验\n\n" +
				"- 参数缺省时使用声明的默认值；一旦提供非法值（类型、范围、枚举或格式不匹配）直接返回 `400`，不做静默回退。\n" +
				"- 路径参数会规范化为 `/rootName[/sub]`，包含 `.` 或 `..` 片段时返回 `400`。\n\n" +
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
			{"name": "文件操作", "description": "文件列表、搜索、下载、上传、重命名/移动与删除（删除文件与删除目录为独立权限）"},
			{"name": "文件信息", "description": "文件详情（含备注与依赖树）、存在性查询与重复文件查询"},
			{"name": "分片上传", "description": "大文件分片上传（断点续传）。需 `open_api:files:upload` 权限。\n\n" +
				"## 调用流程\n" +
				"1. `POST /files/upload/init`：提交文件名、文件总大小与目标目录，获取 `upload_id`、`chunk_size`、`total_chunks`\n" +
				"2. `POST /files/upload/chunk`：按 `chunkIndex`（从 0 开始）逐片上传分片；可分片乱序、可重传覆盖单片、可随时补传缺失分片\n" +
				"3. `POST /files/upload/finalize`：全部分片上传完成后调用，服务端按序合并、落盘、同步索引并写入上传记录\n" +
				"4. （可选）`GET /files/upload/{id}`：查询 `uploaded_indexes` 与过期时间，实现断点续传\n\n" +
				"注意事项：\n" +
				"- 上传会话 24 小时过期，过期后需重新 init\n" +
				"- 目标文件已存在时，需传 `overwrite=true` 且具备 `open_api:files:overwrite` 权限\n" +
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
						{"name": "page", "in": "query", "description": "页码，从 1 开始；提供非法值返回 400", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "default": 1}},
						{"name": "pageSize", "in": "query", "description": "每页数量，1-100；提供非法值返回 400（不回退）", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100, "default": 50}},
						{"name": "sort", "in": "query", "description": "排序字段；提供非法值返回 400", "schema": map[string]interface{}{"type": "string", "enum": []string{"name", "time", "size"}, "default": "name"}},
						{"name": "order", "in": "query", "description": "排序方向；提供非法值返回 400", "schema": map[string]interface{}{"type": "string", "enum": []string{"asc", "desc"}, "default": "asc"}},
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
					"description":      "根据关键词模糊搜索文件（匹配文件名和备注）；空格分隔的多个关键词需全部命中。支持按扩展名过滤，并返回与 Web 端一致的搜索建议词。\n\n所需 scope：`open_api:reader`",
					"tags":             []string{"文件操作"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:reader",
					"parameters": []map[string]interface{}{
						{"name": "query", "in": "query", "required": true, "description": "搜索关键词，支持空格分隔多个关键词（全部命中）", "schema": map[string]interface{}{"type": "string", "example": "annual report"}},
						{"name": "root", "in": "query", "description": "限定搜索的根目录名称（不是完整路径）", "schema": map[string]interface{}{"type": "string", "example": "docs"}},
						{"name": "ext", "in": "query", "description": "扩展名过滤，支持逗号分隔多个（最多 10 个）；可带或不带前导点，非法扩展名返回 400", "schema": map[string]interface{}{"type": "string", "example": "pdf,docx"}},
						{"name": "page", "in": "query", "description": "页码，从 1 开始；提供非法值返回 400", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "default": 1}},
						{"name": "pageSize", "in": "query", "description": "每页数量，1-100；提供非法值返回 400（不回退）", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100, "default": 50}},
					},
					"responses": map[string]interface{}{
						"200": successResponse("搜索结果", objectSchema(map[string]interface{}{
							"results":     arraySchema(schemaRef("SearchFileItem")),
							"suggestions": arraySchema(map[string]interface{}{"type": "string", "description": "搜索建议关键词（前缀补全 / 相关词 / 拼写纠错），逻辑与 Web 端一致"}),
							"pagination":  schemaRef("Pagination"),
						}, "results", "pagination")),
						"400": errorResponse("缺少搜索关键词 query，或分页/扩展名参数非法"),
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
						"400": errorResponse("缺少文件路径、URL 转义/路径格式非法、不能下载目录或 disposition 非法"),
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
					"description":      "根据文件记录 ID 或完整路径 `/rootName/path/to/file` 获取单个文件/目录的详细信息，响应同时包含 `notes` 备注与 `depends` 依赖树（原备注/依赖独立接口已合并到本接口）。\n\n所需 scope：`open_api:files:info`（兼容 `open_api:reader` / `open_api:writer`）",
					"tags":             []string{"文件信息"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:info",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "query", "description": "文件记录 ID（与 path 二选一）", "schema": map[string]interface{}{"type": "integer", "format": "int64", "minimum": 1, "example": 1}},
						{"name": "path", "in": "query", "description": "完整路径（与 id 二选一），如 `/docs/report.pdf`", "schema": map[string]interface{}{"type": "string", "example": "/docs/report.pdf"}},
					},
					"responses": map[string]interface{}{
						"200": successResponse("文件详情", schemaRef("FileDetail")),
						"400": errorResponse("缺少查询条件、同时提供 id 与 path、ID 格式错误或路径格式错误"),
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
					"description":      "以 multipart/form-data 上传单个文件到指定公共目录（适合小文件）；大文件请使用「分片上传」分组的接口。目标已存在时需传 overwrite=true 且具备 `open_api:files:overwrite` 权限才会覆盖。\n\n所需 scope：`open_api:files:upload`（兼容 `open_api:writer`）",
					"tags":             []string{"文件操作"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:upload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"multipart/form-data": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"file", "path"},
									"properties": map[string]interface{}{
										"file":      map[string]interface{}{"type": "string", "format": "binary", "description": "上传的文件"},
										"path":      map[string]interface{}{"type": "string", "description": "目标目录（必填），格式 /rootName[/subdir]", "example": "/docs"},
										"overwrite": map[string]interface{}{"type": "boolean", "description": "目标已存在时是否覆盖，需具备 open_api:files:overwrite 权限；非法值返回 400", "default": false},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": successResponse("上传成功", schemaRef("UploadResult")),
						"400": errorResponse("参数 / 文件名 / 类型 / 路径错误，或 overwrite 非法"),
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
					"description":      "大文件分片上传第 1 步：提交文件名、总大小与目标目录，返回 `upload_id`、`chunk_size`、`total_chunks`；会话 24 小时过期。\n\n所需 scope：`open_api:files:upload`",
					"tags":             []string{"分片上传"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:upload",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"filename", "fileSize", "path"},
									"properties": map[string]interface{}{
										"filename": map[string]interface{}{"type": "string", "maxLength": 255, "description": "文件名", "example": "report.pdf"},
										"fileSize": map[string]interface{}{"type": "integer", "format": "int64", "minimum": 1, "description": "文件总大小（字节），超出配置上限返回 413", "example": 104857600},
										"path":     map[string]interface{}{"type": "string", "description": "目标目录（必填），格式 /rootName[/subdir]", "example": "/docs"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"201": successResponse("会话创建成功", schemaRef("UploadInitResult")),
						"400": errorResponse("参数格式/范围非法、缺少 path 或存储空间不足"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("上传功能关闭或目录不允许上传"),
						"404": errorResponse("根目录不存在"),
						"413": errorResponse("文件大小超出限制"),
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
										"checksum":   map[string]interface{}{"type": "string", "description": "分片 xxh3 十六进制校验值（可选，传则校验；非法格式返回 400）", "example": "0123456789abcdef"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": successResponse("分片上传成功", schemaRef("UploadChunkResult")),
						"400": errorResponse("会话 ID / 分片索引 / 校验值 / 分片内容无效"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("无权操作该上传会话"),
						"404": errorResponse("上传会话不存在"),
						"409": errorResponse("上传会话已过期"),
						"413": errorResponse("分片大小超出限制"),
						"429": errorResponse("请求过于频繁"),
						"500": errorResponse("分片写入失败"),
					},
				},
			},
			"/files/upload/finalize": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":          "完成分片上传",
					"description":      "大文件分片上传第 3 步：全部分片上传完成后调用，服务端按序合并落盘、同步索引并写入上传记录；目标已存在时需传 overwrite=true 且具备 `open_api:files:overwrite` 权限。\n\n所需 scope：`open_api:files:upload`",
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
										"uploadId":  map[string]interface{}{"type": "integer", "format": "int64", "description": "上传会话 ID", "example": 1},
										"overwrite": map[string]interface{}{"type": "boolean", "description": "目标已存在时是否覆盖，需具备 open_api:files:overwrite 权限", "default": false},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": successResponse("上传完成，返回文件记录 ID 与完整路径", schemaRef("UploadFinalizeResult")),
						"400": errorResponse("请求数据格式错误或会话 ID 无效"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("无权操作该上传会话，或缺少覆盖权限"),
						"404": errorResponse("上传会话不存在"),
						"409": errorResponse("目标文件已存在且未允许覆盖，或分片不完整/会话已过期"),
						"429": errorResponse("请求过于频繁"),
						"500": errorResponse("落盘失败"),
					},
				},
			},
			"/files/upload/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":          "查询分片上传会话",
					"description":      "可选步骤：查询会话状态、`expired_at` 与 `uploaded_indexes`，用于断点续传（跳过已上传的分片）。\n\n所需 scope：`open_api:files:upload`",
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
					"description":      "基于 xxh3 哈希值检测重复文件，按分组返回。`pagination.total` 为按 minSize 过滤后的重复分组总数，与 groups 一致；每组 `file_count`/`total_size`/`files` 也仅统计大小 ≥ minSize 的文件。\n\n所需 scope：`open_api:files:duplicates`（兼容 `open_api:reader` / `open_api:writer`）",
					"tags":             []string{"文件信息"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:duplicates",
					"parameters": []map[string]interface{}{
						{"name": "minSize", "in": "query", "description": "最小文件大小（字节），必须 >= 0；提供非法值返回 400", "schema": map[string]interface{}{"type": "integer", "format": "int64", "minimum": 0, "default": 0}},
						{"name": "page", "in": "query", "description": "页码，从 1 开始；提供非法值返回 400", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "default": 1}},
						{"name": "pageSize", "in": "query", "description": "每页分组数量，1-100；提供非法值返回 400（不回退）", "schema": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100, "default": 50}},
					},
					"responses": map[string]interface{}{
						"200": successResponse("重复文件分组", objectSchema(map[string]interface{}{
							"groups":     arraySchema(schemaRef("DuplicateGroup")),
							"pagination": schemaRef("Pagination"),
						}, "groups", "pagination")),
						"400": errorResponse("minSize / page / pageSize 参数非法"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("API Key 缺少 open_api:files:duplicates scope"),
						"429": errorResponse("请求过于频繁"),
						"500": errorResponse("查询重复文件失败"),
					},
				},
			},
			"/files/exists": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":          "批量查询文件是否存在",
					"description":      "按一组 xxh3 哈希批量查询文件是否存在，返回每个哈希的存在情况与命中路径（多个副本时返回全部路径，首个路径同时作为 `path`）。单次最多 100 个哈希。\n\n所需 scope：`open_api:files:exists`（兼容 `open_api:reader` / `open_api:writer`）",
					"tags":             []string{"文件信息"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:exists",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"xxh3"},
									"properties": map[string]interface{}{
										"xxh3": map[string]interface{}{
											"type":        "array",
											"description": "待查询的 xxh3 哈希列表（不区分大小写）",
											"minItems":    1,
											"maxItems":    100,
											"items":       map[string]interface{}{"type": "string", "example": "5d41402abc4b2a76"},
										},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": successResponse("存在性查询结果", objectSchema(map[string]interface{}{
							"results": arraySchema(schemaRef("FileExistsItem")),
						}, "results")),
						"400": errorResponse("请求数据格式错误，或 xxh3 为空/格式非法（需 16 位十六进制）"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("API Key 缺少 open_api:files:exists scope"),
						"429": errorResponse("请求过于频繁"),
					},
				},
			},
			"/files/rename": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":          "重命名或移动文件",
					"description":      "支持仅修改文件名（newName，同目录）或修改完整路径（newPath，可跨目录/跨根）；两者至少提供一个。\n\n所需 scope：`open_api:files:rename`（兼容 `open_api:writer`）",
					"tags":             []string{"文件操作"},
					"security":         dataEndpointSecurity(),
					"x-required-scope": "open_api:files:rename",
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"path"},
									"properties": map[string]interface{}{
										"path":    map[string]interface{}{"type": "string", "description": "文件当前完整路径", "example": "/docs/report.pdf"},
										"newName": map[string]interface{}{"type": "string", "description": "新文件名（仅同目录改名时使用）", "example": "report-2026.pdf"},
										"newPath": map[string]interface{}{"type": "string", "description": "新完整路径（移动或跨目录改名时使用）", "example": "/archive/report-2026.pdf"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": successResponse("重命名/移动成功", objectSchema(map[string]interface{}{
							"old_path": map[string]interface{}{"type": "string", "description": "原完整路径"},
							"path":     map[string]interface{}{"type": "string", "description": "新完整路径"},
							"name":     map[string]interface{}{"type": "string", "description": "新文件名"},
							"root":     map[string]interface{}{"type": "string", "description": "新根目录名称"},
						}, "old_path", "path", "name", "root")),
						"400": errorResponse("参数错误或路径格式错误"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("API Key 缺少 open_api:files:rename scope"),
						"404": errorResponse("源文件或根目录不存在"),
						"409": errorResponse("目标路径已存在"),
						"429": errorResponse("请求过于频繁"),
					},
				},
			},
			"/files": map[string]interface{}{
				"delete": map[string]interface{}{
					"summary":     "删除文件或目录",
					"description": "删除指定文件或目录（目录会递归删除），并同步清理索引记录。删除权限按目标类型区分：目标为文件时需 `open_api:files:delete`，目标为目录时需 `open_api:files:delete_dir`（兼容 `open_api:writer`）。目标不存在时返回 404，不执行任何删除。\n\n所需 scope：`open_api:files:delete`（文件）/ `open_api:files:delete_dir`（目录）",
					"tags":        []string{"文件操作"},
					"security":    dataEndpointSecurity(),
					// 权限按目标类型动态判定，故同时声明两种 scope。
					"x-required-scopes": []string{"open_api:files:delete", "open_api:files:delete_dir"},
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "description": "文件/目录完整路径 `/rootName/path`，不能是根目录本身", "schema": map[string]interface{}{"type": "string", "example": "/docs/report.pdf"}},
					},
					"responses": map[string]interface{}{
						"200": successResponse("删除成功", objectSchema(map[string]interface{}{
							"path":      map[string]interface{}{"type": "string", "description": "被删除的完整路径"},
							"deleted":   map[string]interface{}{"type": "boolean", "description": "是否删除成功"},
							"is_dir":    map[string]interface{}{"type": "boolean", "description": "是否为目录删除"},
							"file_size": map[string]interface{}{"type": "integer", "format": "int64", "description": "被删除文件大小（目录为 0）"},
						}, "path", "deleted")),
						"400": errorResponse("缺少 path、路径格式非法或试图删除根目录"),
						"401": guardResponse("缺少或无效凭证"),
						"403": mixedErrorResponse("API Key 缺少对应目标类型的 scope（文件 delete / 目录 delete_dir）"),
						"404": errorResponse("文件/目录或根目录不存在"),
						"429": errorResponse("请求过于频繁"),
						"500": errorResponse("检查目标或删除失败"),
					},
				},
			},
			"/openapi.json": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "OpenAPI 规范文档",
					"description": "返回 OpenAPI 3.x 规范 JSON。响应未被信封包裹。未开启强制登录时匿名可读；开启强制登录后需要有效凭证（Web 会话 Cookie 或 API Key），但不需要 scope。",
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
						"401": guardResponse("开启强制登录后缺少或无效凭证"),
						"404": errorResponse("Open API 功能未开启"),
						"429": errorResponse("请求过于频繁"),
						"503": guardResponse("Open API 鉴权服务不可用"),
					},
				},
			},
			"/docs": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "API 文档页面",
					"description": "301 重定向到 `/scalar.html`（Scalar 交互式 API 文档页面）。未开启强制登录时匿名可访问；开启强制登录后需要有效凭证。",
					"tags":        []string{"系统"},
					"security":    sessionOrAPIKeySecurity(),
					"responses": map[string]interface{}{
						"301": map[string]interface{}{
							"description": "重定向到 /scalar.html",
							"headers": map[string]interface{}{
								"Location": map[string]interface{}{"description": "重定向目标", "schema": map[string]interface{}{"type": "string", "example": "/scalar.html"}},
							},
						},
						"401": guardResponse("开启强制登录后缺少或无效凭证"),
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
			"depends":        map[string]interface{}{"nullable": true, "description": "文件依赖树（上游递归最多 10 层、下游 1 层）；目录或查询失败时为 null", "allOf": []map[string]interface{}{schemaRef("DependencyTreeNode")}},
			"download_count": map[string]interface{}{"type": "integer", "format": "int64", "description": "下载次数", "example": 3},
			"status":         map[string]interface{}{"type": "string", "enum": []string{"active", "deleted"}, "description": "文件记录状态", "example": "active"},
			"created_at":     map[string]interface{}{"type": "string", "format": "date-time", "description": "记录创建时间", "example": "2026-11-03T10:00:00+08:00"},
			"updated_at":     map[string]interface{}{"type": "string", "format": "date-time", "description": "记录更新时间", "example": "2026-11-03T10:00:00+08:00"},
		}, "id", "name", "path", "full_path", "root", "file_path", "size", "is_dir", "mod_time", "xxh3_hash", "hash_status", "notes", "depends", "download_count", "status", "created_at", "updated_at"),

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
			"upload_id":    map[string]interface{}{"type": "integer", "format": "int64", "description": "上传会话 ID", "example": 1},
			"chunk_size":   map[string]interface{}{"type": "integer", "format": "int64", "description": "分片大小（字节）", "example": 5242880},
			"total_chunks": map[string]interface{}{"type": "integer", "description": "分片总数", "example": 20},
			"expired_at":   map[string]interface{}{"type": "string", "format": "date-time", "description": "会话过期时间", "example": "2026-11-04T10:00:00+08:00"},
			"root":         map[string]interface{}{"type": "string", "description": "目标根目录名称", "example": "docs"},
			"path":         map[string]interface{}{"type": "string", "description": "目标目录路径", "example": "/docs"},
		}, "upload_id", "chunk_size", "total_chunks", "expired_at", "root", "path"),

		"UploadChunkResult": objectSchema(map[string]interface{}{
			"chunk_index": map[string]interface{}{"type": "integer", "description": "已接收的分片索引", "example": 0},
		}, "chunk_index"),

		"UploadFinalizeResult": objectSchema(map[string]interface{}{
			"id":          map[string]interface{}{"type": "integer", "format": "int64", "description": "文件记录 ID；索引同步前可能为 0", "example": 1},
			"name":        map[string]interface{}{"type": "string", "description": "文件名", "example": "report.pdf"},
			"path":        map[string]interface{}{"type": "string", "description": "完整路径", "example": "/docs/report.pdf"},
			"full_path":   map[string]interface{}{"type": "string", "description": "完整路径，与 path 相同", "example": "/docs/report.pdf"},
			"root":        map[string]interface{}{"type": "string", "description": "根目录名称", "example": "docs"},
			"size":        map[string]interface{}{"type": "integer", "format": "int64", "description": "文件大小（字节）", "example": 104857600},
			"overwritten": map[string]interface{}{"type": "boolean", "description": "是否覆盖了已存在的文件", "example": false},
		}, "id", "name", "path", "full_path", "root", "size", "overwritten"),

		"UploadStatusResult": objectSchema(map[string]interface{}{
			"upload_id":        map[string]interface{}{"type": "integer", "format": "int64", "description": "上传会话 ID", "example": 1},
			"file_name":        map[string]interface{}{"type": "string", "description": "文件名", "example": "report.pdf"},
			"file_size":        map[string]interface{}{"type": "integer", "format": "int64", "description": "文件总大小（字节）", "example": 104857600},
			"chunk_size":       map[string]interface{}{"type": "integer", "format": "int64", "description": "分片大小（字节）", "example": 5242880},
			"total_chunks":     map[string]interface{}{"type": "integer", "description": "分片总数", "example": 20},
			"uploaded_indexes": arraySchema(map[string]interface{}{"type": "integer", "description": "已上传的分片索引", "example": 0}),
			"status":           map[string]interface{}{"type": "string", "enum": []string{"pending", "in_progress", "expired", "completed", "failed", "cancelled"}, "description": "会话状态", "example": "in_progress"},
			"expired":          map[string]interface{}{"type": "boolean", "description": "会话是否已过期", "example": false},
			"expired_at":       map[string]interface{}{"type": "string", "format": "date-time", "description": "会话过期时间", "example": "2026-11-04T10:00:00+08:00"},
		}, "upload_id", "file_name", "file_size", "chunk_size", "total_chunks", "uploaded_indexes", "status", "expired", "expired_at"),

		"FileExistsFileItem": objectSchema(map[string]interface{}{
			"id":        map[string]interface{}{"type": "integer", "format": "int64", "description": "文件记录 ID", "example": 1},
			"name":      map[string]interface{}{"type": "string", "description": "文件名", "example": "report.pdf"},
			"path":      map[string]interface{}{"type": "string", "description": "完整路径", "example": "/docs/report.pdf"},
			"root":      map[string]interface{}{"type": "string", "description": "根目录名称", "example": "docs"},
			"file_path": map[string]interface{}{"type": "string", "description": "根目录内相对路径", "example": "/report.pdf"},
			"size":      map[string]interface{}{"type": "integer", "format": "int64", "description": "文件大小（字节）", "example": 10240},
			"mod_time":  map[string]interface{}{"type": "string", "format": "date-time", "description": "文件修改时间"},
			"xxh3_hash": map[string]interface{}{"type": "string", "description": "xxh3 哈希值", "example": "0123456789abcdef"},
		}, "id", "name", "path", "root", "file_path", "size", "mod_time", "xxh3_hash"),

		"FileExistsItem": objectSchema(map[string]interface{}{
			"xxh3":   map[string]interface{}{"type": "string", "description": "查询的 xxh3 哈希（小写）", "example": "0123456789abcdef"},
			"exists": map[string]interface{}{"type": "boolean", "description": "是否存在", "example": true},
			"path":   map[string]interface{}{"type": "string", "description": "首个命中文件的完整路径；不存在时为空", "example": "/docs/report.pdf"},
			"paths":  arraySchema(map[string]interface{}{"type": "string", "description": "全部命中文件的完整路径"}),
			"id":     map[string]interface{}{"type": "integer", "format": "int64", "description": "首个命中文件的记录 ID"},
			"size":   map[string]interface{}{"type": "integer", "format": "int64", "description": "首个命中文件的大小"},
			"files":  arraySchema(schemaRef("FileExistsFileItem")),
		}, "xxh3", "exists", "paths", "files"),

		"DuplicateFileItem": objectSchema(map[string]interface{}{
			"id":        map[string]interface{}{"type": "integer", "format": "int64", "description": "文件记录 ID", "example": 1},
			"file_name": map[string]interface{}{"type": "string", "description": "文件名", "example": "report.pdf"},
			"file_path": map[string]interface{}{"type": "string", "description": "根目录内的相对路径", "example": "/report.pdf"},
			"full_path": map[string]interface{}{"type": "string", "description": "完整路径", "example": "/docs/report.pdf"},
			"root_name": map[string]interface{}{"type": "string", "description": "根目录名称", "example": "docs"},
			"file_size": map[string]interface{}{"type": "integer", "format": "int64", "description": "文件大小（字节）", "example": 10240},
			"mod_time":  map[string]interface{}{"type": "string", "format": "date-time", "description": "文件修改时间", "example": "2026-11-03T10:00:00+08:00"},
		}, "id", "file_name", "file_path", "full_path", "root_name", "file_size", "mod_time"),

		"DuplicateGroup": objectSchema(map[string]interface{}{
			"xxh3_hash":  map[string]interface{}{"type": "string", "description": "分组哈希值", "example": "0123456789abcdef"},
			"file_count": map[string]interface{}{"type": "integer", "format": "int64", "description": "组内文件数量", "example": 2},
			"total_size": map[string]interface{}{"type": "integer", "format": "int64", "description": "组内文件总大小（字节）", "example": 20480},
			"files":      arraySchema(schemaRef("DuplicateFileItem")),
		}, "xxh3_hash", "file_count", "total_size", "files"),

		"DependencyTreeNode": objectSchema(map[string]interface{}{
			"id":            map[string]interface{}{"type": "integer", "format": "int64", "description": "文件记录 ID", "example": 1},
			"dependency_id": map[string]interface{}{"type": "integer", "format": "int64", "description": "依赖关系 ID（用于删除），根节点为 0", "example": 1},
			"file_name":     map[string]interface{}{"type": "string", "description": "文件名", "example": "main.pdf"},
			"file_path":     map[string]interface{}{"type": "string", "description": "根目录内的相对路径", "example": "/main.pdf"},
			"full_path":     map[string]interface{}{"type": "string", "description": "完整路径", "example": "/docs/main.pdf"},
			"root_name":     map[string]interface{}{"type": "string", "description": "根目录名称", "example": "docs"},
			"file_size":     map[string]interface{}{"type": "integer", "format": "int64", "description": "文件大小（字节）", "example": 10240},
			"is_dir":        map[string]interface{}{"type": "boolean", "description": "是否为目录", "example": false},
			"relation":      map[string]interface{}{"type": "string", "enum": []string{"", "requires", "referenced_by", "related"}, "description": "依赖关系类型；根节点为空字符串", "example": "requires"},
			"download_url":  map[string]interface{}{"type": "string", "description": "Web 下载 URL；根节点为空字符串", "example": "/download/docs/main.pdf"},
			"children":      arraySchema(schemaRef("DependencyTreeNode")),
			"downstream":    arraySchema(schemaRef("DependencyTreeNode")),
		}, "id", "dependency_id", "file_name", "file_path", "full_path", "root_name", "file_size", "is_dir", "relation", "download_url", "children", "downstream"),

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
