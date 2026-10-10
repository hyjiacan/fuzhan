package utils

import (
	"net/url"
	"regexp"
	"strings"
)

// sensitiveKeyParts 敏感字段名包含的片段（小写匹配），命中即脱敏。
// 采用「包含匹配」以覆盖 jwt_secret、bind_password、client_secret 等组合命名；
// 不使用裸 "key"，避免误伤排序键等正常字段。
var sensitiveKeyParts = []string{
	"password",
	"passwd",
	"pwd",
	"token",
	"secret",
	"jwt",
	"api_key",
	"apikey",
	"api-key",
	"access_key",
	"signing_key",
	"private_key",
	"privatekey",
	"credential",
	"authorization",
}

// IsSensitiveKey 判断字段名是否属于敏感字段（按片段包含匹配，忽略大小写）。
func IsSensitiveKey(k string) bool {
	lk := strings.ToLower(k)
	for _, p := range sensitiveKeyParts {
		if strings.Contains(lk, p) {
			return true
		}
	}
	return false
}

// SanitizeQuery 对查询串按字段名脱敏（如 ?token=xxx）。
// 解析失败时按文本兜底脱敏，绝不原样返回，避免令牌落盘。
func SanitizeQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return scrubQueryText(rawQuery)
	}
	for k := range values {
		if IsSensitiveKey(k) {
			values.Set(k, "***")
		}
	}
	return values.Encode()
}

// queryPairRe 匹配查询串中的 key=value 对。
var queryPairRe = regexp.MustCompile(`([^&=]+)=([^&]*)`)

// scrubQueryText 在查询串无法解析时，按字段名对 key=value 做兜底脱敏。
func scrubQueryText(rawQuery string) string {
	return queryPairRe.ReplaceAllStringFunc(rawQuery, func(m string) string {
		eq := strings.Index(m, "=")
		if eq < 0 {
			return m
		}
		key := m[:eq]
		if IsSensitiveKey(key) {
			return key + "=***"
		}
		return m
	})
}
