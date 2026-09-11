package cli

import (
	"net/url"
	"testing"
)

// TestEscapeSegMatchesEncodeURIComponent 验证 escapeSeg 与前端 JS encodeURIComponent 行为一致，
// 且能经 url.QueryUnescape（download 等 handler 使用的解码）无损还原。
func TestEscapeSegMatchesEncodeURIComponent(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"a b+c.txt", "a%20b%2Bc.txt"},
		{"中文.png", "%E4%B8%AD%E6%96%87.png"},
		{"100%.txt", "100%25.txt"},
		{"a#b?c&d=e/f", "a%23b%3Fc%26d%3De%2Ff"},
		{"plain.txt", "plain.txt"},
		{"a-b_c.d~e!f*g'h(i)", "a-b_c.d~e!f*g'h(i)"},
	}
	for _, c := range cases {
		got := escapeSeg(c.in)
		if got != c.want {
			t.Errorf("escapeSeg(%q) = %q, 期望 %q", c.in, got, c.want)
		}
		// 经 QueryUnescape 还原应得到原值（+ 以 %2B 传输避免被当作空格）
		dec, err := url.QueryUnescape(got)
		if err != nil || dec != c.in {
			t.Errorf("QueryUnescape(escapeSeg(%q)) = %q, err=%v, 期望还原为 %q", c.in, dec, err, c.in)
		}
	}
}

// TestEncodeSegments 验证多段拼接。
func TestEncodeSegments(t *testing.T) {
	got := encodeSegments([]string{"root", "子 dir", "a+b.txt"})
	want := "root/%E5%AD%90%20dir/a%2Bb.txt"
	if got != want {
		t.Errorf("encodeSegments = %q, 期望 %q", got, want)
	}
}
