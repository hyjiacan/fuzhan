package migration

import (
    "encoding/json"
)

// defaultJSONMarshal JSON 序列化 (兼容 nil 值)
func defaultJSONMarshal(v interface{}) ([]byte, error) {
    return json.Marshal(v)
}

// replaceTypeRegex 替换类型正则匹配
func replaceTypeRegex(s, pattern, replacement string) string {
    upper := s
    upperPattern := pattern
    upperReplacement := replacement

    if len(upper) > 0 && len(upperPattern) > 0 {
        // 简单实现
        for i := 0; i <= len(upper)-len(upperPattern); i++ {
            match := true
            for j := 0; j < len(upperPattern); j++ {
                c1 := upper[i+j]
                c2 := upperPattern[j]
                // 简单大小写不敏感比较
                if c1 != c2 && (c1-'a'+26)%26 != (c2-'a'+26)%26 && (c1-'A'+26)%26 != (c2-'A'+26)%26 {
                    match = false
                    break
                }
            }
            if match {
                upper = upper[:i] + upperReplacement + upper[i+len(upperPattern):]
                // 继续检查后续
                i += len(upperReplacement) - 1
            }
        }
    }
    return upper
}