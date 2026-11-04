// Package mailer 提供告警邮件发送能力，仅依赖标准库 net/smtp。
package mailer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Config SMTP 连接配置。
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLSMode  string // none / starttls / ssl；为空时按端口推断
}

// Message 待发送的邮件内容。
type Message struct {
	To      []string
	Subject string
	Body    string
}

// Send 通过 SMTP 发送一封纯文本邮件。
func Send(cfg Config, msg Message) error {
	cfg.Host = strings.TrimSpace(cfg.Host)
	if cfg.Host == "" {
		return errors.New("SMTP 服务器地址未配置")
	}
	if cfg.Port <= 0 {
		cfg.Port = 587
	}
	to := normalizeRecipients(msg.To)
	if len(to) == 0 {
		return errors.New("邮件接收人未配置")
	}
	from := strings.TrimSpace(cfg.From)
	if from == "" {
		from = cfg.Username
	}
	if from == "" {
		return errors.New("发件人未配置")
	}

	mode := strings.ToLower(strings.TrimSpace(cfg.TLSMode))
	if mode == "" {
		if cfg.Port == 465 {
			mode = "ssl"
		} else {
			mode = "starttls"
		}
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprint(cfg.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	tlsConfig := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}

	var conn net.Conn
	var err error
	if mode == "ssl" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("初始化 SMTP 连接失败: %w", err)
	}
	defer client.Close()

	if mode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP 服务器不支持 STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS 握手失败: %w", err)
		}
	}
	if cfg.Username != "" {
		// net/smtp 的 PlainAuth 会拒绝在非 TLS 连接上传输凭据（localhost 除外），
		// 这里提前给出明确错误，避免出现难以理解的 "unencrypted connection"。
		if mode == "none" {
			return errors.New("不加密连接无法进行 SMTP 认证，请改用 STARTTLS/SSL 或清空用户名")
		}
		if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("SMTP 认证失败: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("设置发件人失败: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("设置收件人 %s 失败: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("准备邮件内容失败: %w", err)
	}
	payload := buildPayload(from, to, msg.Subject, msg.Body)
	if _, err := w.Write([]byte(payload)); err != nil {
		_ = w.Close()
		return fmt.Errorf("写入邮件内容失败: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("提交邮件内容失败: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("结束 SMTP 会话失败: %w", err)
	}
	return nil
}

func buildPayload(from string, to []string, subject, body string) string {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	b.WriteString("\r\n")
	return b.String()
}

func normalizeRecipients(list []string) []string {
	seen := make(map[string]bool, len(list))
	out := make([]string, 0, len(list))
	for _, raw := range list {
		addr := strings.TrimSpace(raw)
		if addr == "" || seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	return out
}
