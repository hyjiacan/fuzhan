package captcha

import (
	"strings"
	"testing"
)

// TestGenerate 校验生成结果：id 非空、PNG data URI、长度合理。
func TestGenerate(t *testing.T) {
	id, uri, err := Generate()
	if err != nil {
		t.Fatalf("Generate() 返回错误: %v", err)
	}
	if id == "" {
		t.Fatal("Generate() 返回空 id")
	}
	if !strings.HasPrefix(uri, pngDataURIPrefix) {
		t.Fatalf("Generate() data URI 不是 PNG 前缀: %.40s", uri)
	}
	if strings.HasPrefix(strings.ToLower(uri), "data:image/svg") {
		t.Fatal("Generate() 返回了 SVG，必须为 PNG")
	}
	if len(uri) <= 500 {
		t.Fatalf("Generate() data URI 长度异常: %d", len(uri))
	}
}

// TestVerifyWrongAnswerIsOneShot 错误答案返回 false，且同一 id 再次校验仍为 false（已销毁）。
func TestVerifyWrongAnswerIsOneShot(t *testing.T) {
	id, _, err := Generate()
	if err != nil {
		t.Fatalf("Generate() 返回错误: %v", err)
	}
	// 字符集不含 '0'，故 "0000" 必为错误答案
	if Verify(id, "0000") {
		t.Fatal("Verify() 对错误答案返回 true")
	}
	if Verify(id, "0000") {
		t.Fatal("Verify() 一次性语义失效：同一 id 第二次校验仍返回 true")
	}
}

// TestVerifyRejectsEmptyInput 空 id / 空答案（含全空白）直接返回 false。
func TestVerifyRejectsEmptyInput(t *testing.T) {
	id, _, err := Generate()
	if err != nil {
		t.Fatalf("Generate() 返回错误: %v", err)
	}
	if Verify("", "abcd") {
		t.Fatal("Verify() 对空 id 返回 true")
	}
	if Verify(id, "") {
		t.Fatal("Verify() 对空答案返回 true")
	}
	if Verify(id, "   ") {
		t.Fatal("Verify() 对全空白答案返回 true")
	}
}

// TestGenerateIdsAreUnique 连续生成两次 id 不同，避免 store key 冲突。
func TestGenerateIdsAreUnique(t *testing.T) {
	first, _, err := Generate()
	if err != nil {
		t.Fatalf("第一次 Generate() 返回错误: %v", err)
	}
	second, _, err := Generate()
	if err != nil {
		t.Fatalf("第二次 Generate() 返回错误: %v", err)
	}
	if first == second {
		t.Fatalf("Generate() 连续两次返回相同 id: %s", first)
	}
}

// TestNormalizeDataURI 覆盖裸 base64 补前缀、PNG 前缀透传、SVG 与其他 MIME 拒绝。
func TestNormalizeDataURI(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "裸 base64 补前缀", raw: "iVBORw0KGgo=", want: pngDataURIPrefix + "iVBORw0KGgo="},
		{name: "PNG 前缀透传", raw: pngDataURIPrefix + "iVBORw0KGgo=", want: pngDataURIPrefix + "iVBORw0KGgo="},
		{name: "SVG 拒绝", raw: "data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=", wantErr: true},
		{name: "其他 MIME 拒绝", raw: "data:image/jpeg;base64,/9j/4AAQ", wantErr: true},
		{name: "空数据", raw: "   ", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeDataURI(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeDataURI(%q) 未返回错误，得到 %q", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeDataURI(%q) 返回错误: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeDataURI(%q) = %q，期望 %q", tc.raw, got, tc.want)
			}
		})
	}
}
