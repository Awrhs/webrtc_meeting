// Package mail 发送事务性邮件（找回密码、邮箱验证的验证码）。
//
// 三种实现：
//   - LogMailer：把邮件内容写进服务端日志。开发与局域网试用的默认选择，验证码从日志里读。
//   - SMTPMailer：标准库 net/smtp，支持 STARTTLS（587）、隐式 TLS（465）与明文（仅本机调试）。
//   - MemoryMailer：测试用，把发出的邮件存在内存里供断言。
//
// 只依赖标准库。
package mail

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Message 一封待发送的纯文本邮件。
type Message struct {
	To      string
	Subject string
	Body    string
}

// Mailer 邮件发送器。
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// LogMailer 把邮件打到日志（Info 级别）。
type LogMailer struct {
	Logger *zap.Logger
}

func (m *LogMailer) Send(_ context.Context, msg Message) error {
	logger := m.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	logger.Info("mail (log provider, not actually sent)",
		zap.String("to", msg.To),
		zap.String("subject", msg.Subject),
		zap.String("body", msg.Body))
	return nil
}

// MemoryMailer 记录发出的邮件，供测试断言。
type MemoryMailer struct {
	mu   sync.Mutex
	Sent []Message
}

func (m *MemoryMailer) Send(_ context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Sent = append(m.Sent, msg)
	return nil
}

// Last 返回最近一封邮件。
func (m *MemoryMailer) Last() (Message, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Sent) == 0 {
		return Message{}, false
	}
	return m.Sent[len(m.Sent)-1], true
}

// SMTPMailer 通过 SMTP 发送。
type SMTPMailer struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// TLSMode：starttls（默认，587 端口）| implicit（465 端口，连接即 TLS）| none（明文，仅本机调试）
	TLSMode string
	Timeout time.Duration
}

func (m *SMTPMailer) Send(ctx context.Context, msg Message) error {
	if m.Host == "" || m.From == "" {
		return errors.New("smtp mailer not configured (host/from)")
	}
	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	addr := net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
	dialer := &net.Dialer{Timeout: timeout}

	var conn net.Conn
	var err error
	if m.TLSMode == "implicit" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: m.Host})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	client, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		return fmt.Errorf("smtp handshake: %w", err)
	}
	defer client.Close()

	if m.TLSMode != "implicit" && m.TLSMode != "none" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: m.Host}); err != nil {
				return fmt.Errorf("smtp starttls: %w", err)
			}
		} else {
			return errors.New("smtp server does not advertise STARTTLS (set SMTP_TLS_MODE=none only for local debugging)")
		}
	}

	if m.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(smtp.PlainAuth("", m.Username, m.Password, m.Host)); err != nil {
				return fmt.Errorf("smtp auth: %w", err)
			}
		}
	}

	if err := client.Mail(m.From); err != nil {
		return fmt.Errorf("smtp MAIL FROM: %w", err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp DATA: %w", err)
	}
	if _, err := w.Write(BuildMessage(m.From, msg.To, msg.Subject, msg.Body)); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return client.Quit()
}

// BuildMessage 组装 RFC 5322 邮件：UTF-8 主题（RFC 2047 B 编码）与 base64 编码的纯文本正文，
// 中文内容在任何 SMTP 服务器上都不会乱码。
func BuildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.BEncoding.Encode("UTF-8", subject) + "\r\n")
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	b.WriteString("\r\n")
	b.WriteString(wrapBase64(base64.StdEncoding.EncodeToString([]byte(body))))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// wrapBase64 按 76 列折行（RFC 2045）。
func wrapBase64(s string) string {
	const width = 76
	var b strings.Builder
	for len(s) > width {
		b.WriteString(s[:width])
		b.WriteString("\r\n")
		s = s[width:]
	}
	b.WriteString(s)
	return b.String()
}
