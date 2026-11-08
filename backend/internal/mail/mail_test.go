package mail

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildMessageEncodesUTF8(t *testing.T) {
	raw := string(BuildMessage("noreply@example.com", "alice@example.com", "【SFU会议】重置密码验证码", "验证码：123456，15 分钟内有效。"))

	headers, body, found := strings.Cut(raw, "\r\n\r\n")
	if !found {
		t.Fatal("message must separate headers and body with a blank line")
	}
	for _, want := range []string{
		"From: noreply@example.com\r\n",
		"To: alice@example.com\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"Content-Transfer-Encoding: base64\r\n",
		"Subject: =?UTF-8?b?",
	} {
		if !strings.Contains(headers, want) {
			t.Errorf("headers missing %q:\n%s", want, headers)
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(body), "\r\n", ""))
	if err != nil {
		t.Fatalf("body is not valid base64: %v", err)
	}
	if string(decoded) != "验证码：123456，15 分钟内有效。" {
		t.Fatalf("body round trip failed: %q", decoded)
	}
}

func TestWrapBase64LinesAre76Columns(t *testing.T) {
	long := strings.Repeat("A", 200)
	for _, line := range strings.Split(wrapBase64(long), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("line longer than 76: %d", len(line))
		}
	}
}

func TestMemoryMailerRecords(t *testing.T) {
	m := &MemoryMailer{}
	if err := m.Send(context.Background(), Message{To: "a@b.c", Subject: "s", Body: "code 000111"}); err != nil {
		t.Fatal(err)
	}
	last, ok := m.Last()
	if !ok || last.To != "a@b.c" || !strings.Contains(last.Body, "000111") {
		t.Fatalf("unexpected recorded mail: %+v %v", last, ok)
	}
}

func TestSMTPMailerRequiresConfig(t *testing.T) {
	m := &SMTPMailer{}
	if err := m.Send(context.Background(), Message{To: "a@b.c"}); err == nil {
		t.Fatal("unconfigured smtp mailer must fail fast")
	}
}
