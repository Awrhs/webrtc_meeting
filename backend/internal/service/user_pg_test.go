package service_test

// userService 在真实 Postgres + Redis 上的集成测试：登录会话、登出、改密码作废旧 token、
// 删除设备踢下线、找回密码、邮箱验证。邮件用 MemoryMailer 截获验证码。

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"conference-sfu/backend/internal/auth"
	"conference-sfu/backend/internal/mail"
	"conference-sfu/backend/internal/models"
	"conference-sfu/backend/internal/repository"
	"conference-sfu/backend/internal/service"
	"conference-sfu/backend/internal/testutil"

	"go.uber.org/zap"
)

type userEnv struct {
	svc       service.UserService
	mailer    *mail.MemoryMailer
	validator *auth.Validator
	repo      repository.UserRepository
}

func newUserEnv(t *testing.T, requireVerification bool) *userEnv {
	t.Helper()
	gdb := testutil.OpenTestDB(t, "service")
	rc := testutil.OpenTestRedis(t)
	repo := repository.NewUserRepository(gdb)
	mailer := &mail.MemoryMailer{}
	svc := service.NewUserService(repo, rc, service.UserServiceConfig{
		JWTSecret:                "test-jwt-secret",
		JWTExpiry:                time.Hour,
		Mailer:                   mailer,
		RequireEmailVerification: requireVerification,
		AppName:                  "TestApp",
	}, zap.NewNop())
	return &userEnv{
		svc:       svc,
		mailer:    mailer,
		validator: &auth.Validator{Secret: "test-jwt-secret", Sessions: rc, Versions: svc},
		repo:      repo,
	}
}

var codeRE = regexp.MustCompile(`\b(\d{6})\b`)

func (e *userEnv) lastCode(t *testing.T) string {
	t.Helper()
	msg, ok := e.mailer.Last()
	if !ok {
		t.Fatal("no mail was sent")
	}
	m := codeRE.FindStringSubmatch(msg.Body)
	if m == nil {
		t.Fatalf("no 6-digit code in mail body: %q", msg.Body)
	}
	return m[1]
}

func devA() service.DeviceInfo {
	return service.DeviceInfo{ID: "dev-a", UserAgent: "ua-a", IP: "10.0.0.1"}
}

func devB() service.DeviceInfo {
	return service.DeviceInfo{ID: "dev-b", UserAgent: "ua-b", IP: "10.0.0.2"}
}

func TestLoginLogoutSession(t *testing.T) {
	e := newUserEnv(t, false)
	user, verify, err := e.svc.Register("alice", "alice@example.com", "13900000001", "Password123")
	if err != nil || verify {
		t.Fatalf("Register: err=%v verificationRequired=%v", err, verify)
	}

	token, _, err := e.svc.Login("alice", "Password123", devA())
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	claims, err := e.validator.Validate(token)
	if err != nil {
		t.Fatalf("fresh token rejected: %v", err)
	}
	if claims.UserID != user.ID || claims.DeviceID != "dev-a" || claims.JTI == "" {
		t.Fatalf("unexpected claims: %+v", claims)
	}

	// 邮箱/手机号登录同样可用
	if _, _, err := e.svc.Login("alice@example.com", "Password123", devA()); err != nil {
		t.Fatalf("login by email: %v", err)
	}
	if _, _, err := e.svc.Login("13900000001", "Password123", devA()); err != nil {
		t.Fatalf("login by phone: %v", err)
	}

	if err := e.svc.Logout(user.ID, claims.JTI); err != nil {
		t.Fatal(err)
	}
	if _, err := e.validator.Validate(token); !errors.Is(err, auth.ErrSessionRevoked) {
		t.Fatalf("token after logout: %v", err)
	}
}

func TestPasswordChangeInvalidatesOtherSessions(t *testing.T) {
	e := newUserEnv(t, false)
	user, _, _ := e.svc.Register("bob", "bob@example.com", "13900000002", "Password123")
	tokenA, _, _ := e.svc.Login("bob", "Password123", devA())
	tokenB, _, _ := e.svc.Login("bob", "Password123", devB())

	if _, err := e.svc.UpdatePassword(user.ID, "wrong", "NewPassword456", devA()); err == nil {
		t.Fatal("wrong old password must be rejected")
	}
	newToken, err := e.svc.UpdatePassword(user.ID, "Password123", "NewPassword456", devA())
	if err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}

	for name, tok := range map[string]string{"A": tokenA, "B": tokenB} {
		if _, err := e.validator.Validate(tok); err == nil {
			t.Fatalf("old token %s must be invalid after password change", name)
		}
	}
	if _, err := e.validator.Validate(newToken); err != nil {
		t.Fatalf("new token for current device must be valid: %v", err)
	}
	if _, _, err := e.svc.Login("bob", "Password123", devA()); err == nil {
		t.Fatal("old password must no longer work")
	}
	if _, _, err := e.svc.Login("bob", "NewPassword456", devB()); err != nil {
		t.Fatalf("new password login: %v", err)
	}
}

func TestRevokeDeviceKicksOnlyThatDevice(t *testing.T) {
	e := newUserEnv(t, false)
	user, _, _ := e.svc.Register("carol", "carol@example.com", "13900000003", "Password123")
	tokenA1, _, _ := e.svc.Login("carol", "Password123", devA())
	tokenA2, _, _ := e.svc.Login("carol", "Password123", devA())
	tokenB, _, _ := e.svc.Login("carol", "Password123", devB())

	for _, d := range []service.DeviceInfo{devA(), devB()} {
		if err := e.repo.UpsertDevice(&models.Device{UserID: user.ID, DeviceID: d.ID, UserAgent: d.UserAgent, IP: d.IP, LastActive: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	devices, _ := e.repo.GetDevicesByUserID(user.ID)
	var rowA uint
	for _, d := range devices {
		if d.DeviceID == "dev-a" {
			rowA = d.ID
		}
	}
	if rowA == 0 {
		t.Fatal("device row for dev-a not found")
	}

	if err := e.svc.RevokeDevice(user.ID, rowA); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}
	for _, tok := range []string{tokenA1, tokenA2} {
		if _, err := e.validator.Validate(tok); !errors.Is(err, auth.ErrSessionRevoked) {
			t.Fatalf("dev-a session must be revoked: %v", err)
		}
	}
	if _, err := e.validator.Validate(tokenB); err != nil {
		t.Fatalf("dev-b must stay logged in: %v", err)
	}
	if _, err := e.repo.GetDeviceByID(rowA, user.ID); err == nil {
		t.Fatal("device row must be deleted")
	}
	if err := e.svc.RevokeDevice(user.ID, rowA); err == nil {
		t.Fatal("revoking a missing device must fail")
	}
}

func TestPasswordResetFlow(t *testing.T) {
	e := newUserEnv(t, false)
	_, _, _ = e.svc.Register("dave", "dave@example.com", "13900000004", "Password123")
	oldToken, _, _ := e.svc.Login("dave", "Password123", devA())

	// 未知账号：同样成功返回，但不发邮件
	if err := e.svc.RequestPasswordReset("nobody@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(e.mailer.Sent) != 0 {
		t.Fatal("must not send mail for unknown account")
	}

	if err := e.svc.RequestPasswordReset("dave@example.com"); err != nil {
		t.Fatal(err)
	}
	code := e.lastCode(t)
	if last, _ := e.mailer.Last(); last.To != "dave@example.com" {
		t.Fatalf("reset mail sent to %s", last.To)
	}

	if err := e.svc.ResetPassword("dave", "000000", "NewPassword456"); !errors.Is(err, service.ErrInvalidCode) {
		t.Fatalf("wrong code: %v", err)
	}
	if err := e.svc.ResetPassword("dave", code, "NewPassword456"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	// 验证码一次性
	if err := e.svc.ResetPassword("dave", code, "AnotherPass789"); !errors.Is(err, service.ErrInvalidCode) {
		t.Fatalf("code reuse: %v", err)
	}
	if _, err := e.validator.Validate(oldToken); err == nil {
		t.Fatal("all sessions must be revoked after reset")
	}
	if _, _, err := e.svc.Login("dave", "NewPassword456", devA()); err != nil {
		t.Fatalf("login with reset password: %v", err)
	}
}

func TestPasswordResetTooManyAttempts(t *testing.T) {
	e := newUserEnv(t, false)
	_, _, _ = e.svc.Register("erin", "erin@example.com", "13900000005", "Password123")
	if err := e.svc.RequestPasswordReset("erin"); err != nil {
		t.Fatal(err)
	}
	code := e.lastCode(t)
	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}
	var lastErr error
	for i := 0; i < 6; i++ {
		lastErr = e.svc.ResetPassword("erin", wrong, "NewPassword456")
	}
	if !errors.Is(lastErr, service.ErrTooManyAttempts) {
		t.Fatalf("after 6 wrong attempts: %v", lastErr)
	}
	// 作废后正确的验证码也不再可用
	if err := e.svc.ResetPassword("erin", code, "NewPassword456"); !errors.Is(err, service.ErrInvalidCode) {
		t.Fatalf("code must be invalidated: %v", err)
	}
}

func TestEmailVerificationRequired(t *testing.T) {
	e := newUserEnv(t, true)
	_, verify, err := e.svc.Register("frank", "frank@example.com", "13900000006", "Password123")
	if err != nil || !verify {
		t.Fatalf("Register: err=%v verificationRequired=%v", err, verify)
	}
	code := e.lastCode(t)

	if _, _, err := e.svc.Login("frank", "Password123", devA()); !errors.Is(err, service.ErrEmailNotVerified) {
		t.Fatalf("unverified login: %v", err)
	}
	if err := e.svc.VerifyEmail("frank@example.com", "000000"); !errors.Is(err, service.ErrInvalidCode) {
		t.Fatalf("wrong verification code: %v", err)
	}
	if err := e.svc.VerifyEmail("frank@example.com", code); err != nil {
		t.Fatalf("VerifyEmail: %v", err)
	}
	if _, _, err := e.svc.Login("frank", "Password123", devA()); err != nil {
		t.Fatalf("login after verification: %v", err)
	}
	// 已验证的邮箱不再发送验证码
	before := len(e.mailer.Sent)
	if err := e.svc.SendEmailVerification("frank@example.com"); err != nil {
		t.Fatal(err)
	}
	if len(e.mailer.Sent) != before {
		t.Fatal("verified email must not receive another code")
	}
}
