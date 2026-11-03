package openapi

// GenerateOpenAPISpec 生成 OpenAPI 3.x 规范文档
func GenerateOpenAPISpec() map[string]interface{} {
	return map[string]interface{}{
		"openapi": "3.0.3",
		"info": map[string]interface{}{
			"title":       "Fuzhan (浮栈) Open API",
			"description": "浮栈对外 API，提供文件列表、搜索、下载、详情、上传、重复文件检测等功能。\n\n## 认证\n\n所有接口都需要凭证：\n- **API Key**: 通过 `Authorization: Bearer <keyId>:<secret>` 请求头传递（需具备对应接口的 scope）\n- **Web 会话**: 凭证访问模式下可直接使用登录 Cookie",
			"version":     "1.0.0",
		},
		"servers": []map[string]interface{}{
			{"url": "/api/open/v1", "description": "Open API v1"},
		},
		"tags": []map[string]interface{}{
			{"name": "文件操作", "description": "文件列表、搜索、下载与单请求上传"},
			{"name": "文件信息", "description": "文件详情、备注、依赖与重复文件查询"},
			{"name": "分片上传", "description": "大文件分片上传（断点续传）。需 `open_api:files:upload` 权限。\n\n## 调用流程\n1. `POST /files/upload/init`：提交文件名、文件总大小与目标目录，获取 `uploadId`、`chunkSize`、`totalChunks`\n2. `POST /files/upload/chunk`：按 `chunkIndex`（从 0 开始）逐片上传分片；可分片乱序、可重传覆盖单片、可随时补传缺失分片\n3. `POST /files/upload/finalize`：全部分片上传完成后调用，服务端按序合并、落盘、同步索引并写入上传记录\n4. （可选）`GET /files/upload/{id}`：查询 `uploadedIndexes` 与过期时间，实现断点续传\n\n注意事项：\n- 上传会话 24 小时过期，过期后需重新 init\n- 目标文件已存在时，仅上传者同一 IP 可覆盖（与 Web 公共目录上传一致）\n- 小文件可直接使用 `POST /files/upload` 单请求上传，无需分片"},
			{"name": "文件管理", "description": "文件重命名、移动、删除"},
			{"name": "系统", "description": "OpenAPI 规范与文档页面"},
		},
		"paths": map[string]interface{}{
			"/files/list": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "文件列表",
					"description": "基于文件索引表，以分页方式获取指定目录的文件列表",
					"tags":        []string{"文件操作"},
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "description": "目录路径", "schema": map[string]interface{}{"type": "string", "default": "/"}},
						{"name": "page", "in": "query", "description": "页码", "schema": map[string]interface{}{"type": "integer", "default": 1}},
						{"name": "pageSize", "in": "query", "description": "每页数量", "schema": map[string]interface{}{"type": "integer", "default": 50, "maximum": 100}},
						{"name": "sort", "in": "query", "description": "排序字段", "schema": map[string]interface{}{"type": "string", "enum": []string{"name", "time", "size"}, "default": "name"}},
						{"name": "order", "in": "query", "description": "排序方向", "schema": map[string]interface{}{"type": "string", "enum": []string{"asc", "desc"}, "default": "asc"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "成功",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"success": map[string]interface{}{"type": "boolean"},
											"data": map[string]interface{}{
												"type": "object",
												"properties": map[string]interface{}{
													"path":  map[string]interface{}{"type": "string"},
													"files": map[string]interface{}{"type": "array"},
													"pagination": map[string]interface{}{
														"type": "object",
														"properties": map[string]interface{}{
															"page":      map[string]interface{}{"type": "integer"},
															"page_size": map[string]interface{}{"type": "integer"},
															"total":     map[string]interface{}{"type": "integer"},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			"/files/search": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "文件搜索",
					"description": "根据关键词搜索文件（匹配文件名和备注）",
					"tags":        []string{"文件操作"},
					"parameters": []map[string]interface{}{
						{"name": "query", "in": "query", "required": true, "description": "搜索关键词", "schema": map[string]interface{}{"type": "string"}},
						{"name": "root", "in": "query", "description": "限定搜索的根目录", "schema": map[string]interface{}{"type": "string"}},
						{"name": "page", "in": "query", "description": "页码", "schema": map[string]interface{}{"type": "integer", "default": 1}},
						{"name": "pageSize", "in": "query", "description": "每页数量", "schema": map[string]interface{}{"type": "integer", "default": 50, "maximum": 100}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "成功"},
						"400": map[string]interface{}{"description": "缺少搜索关键词"},
					},
				},
			},
			"/files/download/{path}": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "文件下载",
					"description": "通过文件路径下载文件",
					"tags":        []string{"文件操作"},
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "path", "required": true, "description": "文件路径 (如 /docs/report.pdf)", "schema": map[string]interface{}{"type": "string"}},
						{"name": "disposition", "in": "query", "description": "下载方式", "schema": map[string]interface{}{"type": "string", "enum": []string{"attachment", "inline"}, "default": "attachment"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "文件二进制流"},
						"404": map[string]interface{}{"description": "文件不存在"},
					},
				},
			},
			"/files/info": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "文件详情",
					"description": "根据文件记录 ID 或完整路径（/rootName/path/to/file）获取单个文件/目录的详细信息",
					"tags":        []string{"文件信息"},
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "query", "description": "文件记录 ID（与 path 二选一）", "schema": map[string]interface{}{"type": "integer"}},
						{"name": "path", "in": "query", "description": "完整路径（与 id 二选一），如 /docs/report.pdf", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "成功"},
						"400": map[string]interface{}{"description": "缺少查询条件或参数格式错误"},
						"404": map[string]interface{}{"description": "文件记录不存在"},
					},
				},
			},
			"/files/upload": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "文件上传",
					"description": "以 multipart/form-data 上传单个文件到指定公共目录（适合小文件）；大文件请使用「分片上传」分组的接口。目标已存在时仅上传者同一 IP 且传 overwrite=true 才会覆盖",
					"tags":        []string{"文件操作"},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"multipart/form-data": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"file"},
									"properties": map[string]interface{}{
										"file":      map[string]interface{}{"type": "string", "format": "binary", "description": "上传的文件"},
										"path":      map[string]interface{}{"type": "string", "description": "目标目录，格式 /rootName[/subdir]，缺省为 /"},
										"overwrite": map[string]interface{}{"type": "boolean", "description": "目标已存在时是否覆盖（仅上传者同一 IP 有效）", "default": false},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "上传成功"},
						"400": map[string]interface{}{"description": "参数 / 文件名 / 类型 / 路径错误"},
						"403": map[string]interface{}{"description": "上传功能关闭、目录不允许上传或无覆盖权限"},
						"404": map[string]interface{}{"description": "根目录不存在"},
						"409": map[string]interface{}{"description": "目标文件已存在且未允许覆盖"},
						"413": map[string]interface{}{"description": "文件大小超出限制"},
					},
				},
			},
			"/files/upload/init": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "创建分片上传会话",
					"description": "大文件分片上传第 1 步：提交文件名、总大小与目标目录，返回 uploadId、chunkSize、totalChunks；会话 24 小时过期",
					"tags":        []string{"分片上传"},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"filename", "fileSize"},
									"properties": map[string]interface{}{
										"filename": map[string]interface{}{"type": "string", "description": "文件名"},
										"fileSize": map[string]interface{}{"type": "integer", "format": "int64", "description": "文件总大小（字节）"},
										"path":     map[string]interface{}{"type": "string", "description": "目标目录，格式 /rootName[/subdir]，缺省为 /"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"201": map[string]interface{}{"description": "会话创建成功，返回 uploadId/chunkSize/totalChunks/expiredAt"},
						"400": map[string]interface{}{"description": "参数错误或超出大小限制"},
						"403": map[string]interface{}{"description": "上传功能关闭或目录不允许上传"},
						"404": map[string]interface{}{"description": "根目录不存在"},
					},
				},
			},
			"/files/upload/chunk": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "上传分片",
					"description": "大文件分片上传第 2 步：按 uploadId 上传指定索引的分片；分片可乱序上传，重复上传同一索引会覆盖该片",
					"tags":        []string{"分片上传"},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"multipart/form-data": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"uploadId", "chunkIndex", "chunk"},
									"properties": map[string]interface{}{
										"uploadId":   map[string]interface{}{"type": "integer", "description": "上传会话 ID"},
										"chunkIndex": map[string]interface{}{"type": "integer", "description": "分片索引，从 0 开始"},
										"chunk":      map[string]interface{}{"type": "string", "format": "binary", "description": "分片数据"},
										"checksum":   map[string]interface{}{"type": "string", "description": "分片校验和（可选）"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "分片上传成功"},
						"400": map[string]interface{}{"description": "会话ID / 分片索引 / 分片文件无效"},
						"403": map[string]interface{}{"description": "无权操作该上传会话"},
						"404": map[string]interface{}{"description": "上传会话不存在"},
						"413": map[string]interface{}{"description": "分片大小超出限制"},
					},
				},
			},
			"/files/upload/finalize": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "完成分片上传",
					"description": "大文件分片上传第 3 步：全部分片上传完成后调用，服务端按序合并落盘、同步索引并写入上传记录；目标已存在时仅上传者同一 IP 可覆盖（与 Web 公共目录上传一致）",
					"tags":        []string{"分片上传"},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"type":     "object",
									"required": []string{"uploadId"},
									"properties": map[string]interface{}{
										"uploadId": map[string]interface{}{"type": "integer", "description": "上传会话 ID"},
									},
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "上传完成，返回文件记录 ID 与完整路径"},
						"403": map[string]interface{}{"description": "无权操作该上传会话"},
						"409": map[string]interface{}{"description": "目标文件已存在且未允许覆盖"},
						"500": map[string]interface{}{"description": "分片不完整或落盘失败"},
					},
				},
			},
			"/files/upload/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "查询分片上传会话",
					"description": "可选步骤：查询会话状态、expiredAt 与 uploadedIndexes，用于断点续传（跳过已上传的分片）",
					"tags":        []string{"分片上传"},
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "上传会话 ID", "schema": map[string]interface{}{"type": "integer"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "会话状态"},
						"400": map[string]interface{}{"description": "无效的会话ID"},
						"403": map[string]interface{}{"description": "无权操作该上传会话"},
						"404": map[string]interface{}{"description": "上传会话不存在"},
					},
				},
			},
			"/files/duplicates": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "重复文件查询",
					"description": "基于 xxh3 哈希值检测重复文件",
					"tags":        []string{"文件管理"},
					"parameters": []map[string]interface{}{
						{"name": "minSize", "in": "query", "description": "最小文件大小", "schema": map[string]interface{}{"type": "integer", "default": 0}},
						{"name": "page", "in": "query", "description": "页码", "schema": map[string]interface{}{"type": "integer", "default": 1}},
						{"name": "pageSize", "in": "query", "description": "每页数量", "schema": map[string]interface{}{"type": "integer", "default": 50}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "成功"},
					},
				},
			},
			"/files/notes": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "文件备注查询",
					"description": "获取指定文件的备注信息",
					"tags":        []string{"文件管理"},
					"parameters": []map[string]interface{}{
						{"name": "path", "in": "query", "required": true, "description": "文件路径", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "成功"},
						"404": map[string]interface{}{"description": "文件不存在"},
					},
				},
			},
			"/files/{id}/depends": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "文件依赖树查询",
					"description": "基于文件记录 ID 查询文件的依赖树（上游递归、下游一层）",
					"tags":        []string{"文件管理"},
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "description": "文件记录 ID", "schema": map[string]interface{}{"type": "integer"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "成功"},
						"404": map[string]interface{}{"description": "文件记录不存在"},
					},
				},
			},
			"/openapi.json": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "OpenAPI 规范文档",
					"description": "返回 OpenAPI 3.x 规范 JSON",
					"tags":        []string{"系统"},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "成功"},
					},
				},
			},
			"/docs": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "API 文档页面",
					"description": "Scalar 交互式 API 文档页面",
					"tags":        []string{"系统"},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "HTML 页面"},
					},
				},
			},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"ApiKeyAuth": map[string]interface{}{
					"type":        "http",
					"scheme":      "bearer",
					"description": "使用 API Key 进行认证，格式: keyId:secret",
				},
			},
		},
	}
}
