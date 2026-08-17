package openapi

// GenerateOpenAPISpec 生成 OpenAPI 3.x 规范文档
func GenerateOpenAPISpec() map[string]interface{} {
    return map[string]interface{}{
        "openapi": "3.0.3",
        "info": map[string]interface{}{
            "title":       "Fuzhan (浮栈) Open API",
            "description": "浮栈对外公开 API，提供文件列表、搜索、下载、重复文件检测等功能。\n\n## 认证\n\n部分 API 需要认证。支持以下方式：\n- **API Key**: 通过 `Authorization: Bearer <keyId>:<secret>` 请求头传递\n- **Query 参数**: 通过 `api_key=<keyId>:<secret>` 查询参数传递",
            "version": "1.0.0",
        },
        "servers": []map[string]interface{}{
            {"url": "/api/open/v1", "description": "Open API v1"},
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
                                                            "page":     map[string]interface{}{"type": "integer"},
                                                            "page_size": map[string]interface{}{"type": "integer"},
                                                            "total":    map[string]interface{}{"type": "integer"},
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
                    "type":         "http",
                    "scheme":       "bearer",
                    "description": "使用 API Key 进行认证，格式: keyId:secret",
                },
            },
        },
    }
}
