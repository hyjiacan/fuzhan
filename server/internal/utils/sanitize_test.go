package utils

import (
	"strings"
	"testing"
)

func TestSanitizeQuery(t *testing.T) {
	got := SanitizeQuery("token=abc&page=2")
	if strings.Contains(got, "abc") {
		t.Errorf("query 中的 token 未脱敏: %s", got)
	}
	if !strings.Contains(got, "page=2") {
		t.Errorf("非敏感参数被误改: %s", got)
	}
}

func TestSanitizeQueryNestedAndFallback(t *testing.T) {
	// 嵌套键名 filter[token] 也应命中
	if got := SanitizeQuery("filter[token]=abc&page=2"); strings.Contains(got, "abc") {
		t.Errorf("嵌套 token 未脱敏: %s", got)
	}
	// 非法转义导致解析失败时，按文本兜底脱敏
	if got := SanitizeQuery("token=%zz&page=2"); strings.Contains(got, "%zz") {
		t.Errorf("解析失败时未兜底脱敏: %s", got)
	}
}

func TestIsSensitiveKeyCombinedNames(t *testing.T) {
	for _, k := range []string{"jwt_secret", "bind_password", "client_secret", "access_token", "filter[token]"} {
		if !IsSensitiveKey(k) {
			t.Errorf("组合敏感字段 %q 未被识别", k)
		}
	}
	for _, k := range []string{"key", "sort_key", "page", "name"} {
		if IsSensitiveKey(k) {
			t.Errorf("正常字段 %q 被误判为敏感", k)
		}
	}
}
