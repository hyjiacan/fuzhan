package openapi

import (
	"strings"
	"testing"
)

// TestGenerateOpenAPISpec_AllOperationsDocumented 校验规范完整性：
// 每个操作都必须有 tags、security、responses，且所有 2xx 响应都必须带 schema。
func TestGenerateOpenAPISpec_AllOperationsDocumented(t *testing.T) {
	spec := GenerateOpenAPISpec()

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok || len(paths) == 0 {
		t.Fatal("paths 缺失或为空")
	}

	for path, rawItem := range paths {
		item, ok := rawItem.(map[string]interface{})
		if !ok {
			t.Fatalf("%s 的 path item 结构无效", path)
		}
		for method, rawOp := range item {
			op, ok := rawOp.(map[string]interface{})
			if !ok {
				t.Fatalf("%s %s 的 operation 结构无效", method, path)
			}
			if _, ok := op["tags"].([]string); !ok {
				t.Errorf("%s %s 缺少 tags", method, path)
			}
			if _, ok := op["security"]; !ok {
				t.Errorf("%s %s 缺少 security", method, path)
			}

			responses, ok := op["responses"].(map[string]interface{})
			if !ok || len(responses) == 0 {
				t.Errorf("%s %s 缺少 responses", method, path)
				continue
			}
			for code, rawResp := range responses {
				resp, ok := rawResp.(map[string]interface{})
				if !ok {
					t.Errorf("%s %s %s 的 response 结构无效", method, path, code)
					continue
				}
				if !strings.HasPrefix(code, "2") {
					continue
				}
				content, ok := resp["content"].(map[string]interface{})
				if !ok || len(content) == 0 {
					t.Errorf("%s %s %s 成功响应缺少 content/schema", method, path, code)
					continue
				}
				media, ok := content["application/json"].(map[string]interface{})
				if !ok {
					// 下载接口使用二进制媒体类型，单独要求存在 schema。
					media, ok = content["application/octet-stream"].(map[string]interface{})
				}
				if !ok {
					t.Errorf("%s %s %s 成功响应媒体类型不受支持", method, path, code)
					continue
				}
				if _, ok := media["schema"].(map[string]interface{}); !ok {
					t.Errorf("%s %s %s 成功响应缺少 schema", method, path, code)
				}
			}
		}
	}
}

// TestGenerateOpenAPISpec_AllRefsResolve 校验所有 $ref 都能在 components.schemas 中找到。
func TestGenerateOpenAPISpec_AllRefsResolve(t *testing.T) {
	spec := GenerateOpenAPISpec()
	components := spec["components"].(map[string]interface{})
	schemas := components["schemas"].(map[string]interface{})

	var walk func(v interface{})
	walk = func(v interface{}) {
		switch node := v.(type) {
		case map[string]interface{}:
			if ref, ok := node["$ref"].(string); ok {
				const prefix = "#/components/schemas/"
				if !strings.HasPrefix(ref, prefix) {
					t.Errorf("无法识别的 $ref: %s", ref)
					return
				}
				name := strings.TrimPrefix(ref, prefix)
				if _, ok := schemas[name]; !ok {
					t.Errorf("$ref 指向不存在的 schema: %s", ref)
				}
			}
			for _, child := range node {
				walk(child)
			}
		case []interface{}:
			for _, child := range node {
				walk(child)
			}
		case []map[string]interface{}:
			for _, child := range node {
				walk(child)
			}
		case []string:
			return
		}
	}
	walk(spec["paths"])
}
