package services

import (
	"errors"
	"testing"
	"time"
)

// TestIdentityConfirmTicketLifecycle 票据一次性使用：消费后不可再次读取。
func TestIdentityConfirmTicketLifecycle(t *testing.T) {
	svc := NewIdentityConfirmService()
	token, err := svc.Issue(IdentityConfirmTicket{Username: "alice", AuthType: "oauth2"})
	if err != nil {
		t.Fatalf("签发票据失败: %v", err)
	}
	if len(token) != 64 {
		t.Errorf("票据应为 32 字节 hex（64 字符），实际长度 %d", len(token))
	}

	ticket, ok := svc.Lookup(token)
	if !ok || ticket.Username != "alice" {
		t.Fatalf("票据读取失败: ok=%v ticket=%+v", ok, ticket)
	}

	if _, ok := svc.Consume(token); !ok {
		t.Fatal("首次消费票据应成功")
	}
	if _, ok := svc.Consume(token); ok {
		t.Fatal("票据只能使用一次")
	}
	if _, ok := svc.Lookup(token); ok {
		t.Fatal("消费后不应再读取到票据")
	}
}

// TestIdentityConfirmTicketDistinctTokens 不同票据 token 必须唯一。
func TestIdentityConfirmTicketDistinctTokens(t *testing.T) {
	svc := NewIdentityConfirmService()
	first, err := svc.Issue(IdentityConfirmTicket{Username: "alice"})
	if err != nil {
		t.Fatalf("签发票据失败: %v", err)
	}
	second, err := svc.Issue(IdentityConfirmTicket{Username: "alice"})
	if err != nil {
		t.Fatalf("签发票据失败: %v", err)
	}
	if first == second {
		t.Fatal("两次签发的票据 token 不应相同")
	}
}

// TestIdentityConfirmTicketExpired 过期票据不可读取/消费。
func TestIdentityConfirmTicketExpired(t *testing.T) {
	svc := NewIdentityConfirmService()
	token, err := svc.Issue(IdentityConfirmTicket{Username: "alice"})
	if err != nil {
		t.Fatalf("签发票据失败: %v", err)
	}
	// 直接改写过期时间，模拟有效期结束
	svc.mu.Lock()
	svc.tickets[token].ExpiresAt = time.Now().UTC().Add(-time.Second)
	svc.mu.Unlock()

	if _, ok := svc.Lookup(token); ok {
		t.Fatal("过期票据不应可读")
	}
	if _, ok := svc.Consume(token); ok {
		t.Fatal("过期票据不应可消费")
	}
}

// TestIdentityConfirmTicketFailureLimit 本地密码错误达到上限后票据失效。
func TestIdentityConfirmTicketFailureLimit(t *testing.T) {
	svc := NewIdentityConfirmService()
	token, err := svc.Issue(IdentityConfirmTicket{Username: "alice"})
	if err != nil {
		t.Fatalf("签发票据失败: %v", err)
	}

	for i := 1; i < IdentityConfirmMaxPasswordFailure; i++ {
		remaining, valid := svc.RecordFailure(token)
		if !valid {
			t.Fatalf("第 %d 次错误不应使票据失效", i)
		}
		if want := IdentityConfirmMaxPasswordFailure - i; remaining != want {
			t.Errorf("第 %d 次错误后剩余次数 = %d, want %d", i, remaining, want)
		}
	}
	if _, valid := svc.RecordFailure(token); valid {
		t.Fatal("第 5 次错误后票据应失效")
	}
	if _, ok := svc.Lookup(token); ok {
		t.Fatal("票据失效后不应可读")
	}
}

// TestIdentityConfirmTicketBeginExclusive Begin 原子占用：并发提交只有一个请求能拿到票据。
func TestIdentityConfirmTicketBeginExclusive(t *testing.T) {
	svc := NewIdentityConfirmService()
	token, err := svc.Issue(IdentityConfirmTicket{Username: "alice"})
	if err != nil {
		t.Fatalf("签发票据失败: %v", err)
	}

	if _, ok := svc.Begin(token); !ok {
		t.Fatal("首次 Begin 应成功")
	}
	if _, ok := svc.Begin(token); ok {
		t.Fatal("占用期间重复 Begin 应失败")
	}

	// End 释放后允许重试（本地密码错误等可重试场景）
	svc.End(token)
	if _, ok := svc.Begin(token); !ok {
		t.Fatal("End 后应可重新 Begin")
	}

	// Consume 后票据消失，Begin 失败
	svc.Consume(token)
	if _, ok := svc.Begin(token); ok {
		t.Fatal("消费后不应再能 Begin")
	}
}

// TestIdentityConflictErrorIsAccountUnavailable 冲突错误仍需满足既有 errors.Is 语义。
func TestIdentityConflictErrorIsAccountUnavailable(t *testing.T) {
	err := error(&IdentityConflictError{Username: "alice"})
	if !errors.Is(err, ErrAccountUnavailable) {
		t.Fatal("IdentityConflictError 应按 ErrAccountUnavailable 处理")
	}
	if err.Error() != ErrAccountUnavailable.Error() {
		t.Errorf("对外文案应保持通用，实际 %q", err.Error())
	}
}
