package config

import "testing"

func validProductionConfig() *Config {
	return &Config{
		GOEnv:                      "production",
		JWTSecret:                  "0123456789abcdef0123456789abcdef",
		SFUJWTSecret:               "abcdef0123456789abcdef0123456789",
		AllowedOrigins:             []string{"https://example.com"},
		SFUControlToken:            "control-token-that-is-not-default",
		SFUCallbackToken:           "callback-token-that-is-not-default",
		SFUAllowUnauthenticatedDev: false,
	}
}

func TestValidateProductionAcceptsStrongConfig(t *testing.T) {
	if err := validProductionConfig().Validate(); err != nil {
		t.Fatalf("expected valid production config, got %v", err)
	}
}

func TestValidateProductionRejectsWeakJWTSecret(t *testing.T) {
	cfg := validProductionConfig()
	cfg.JWTSecret = "short"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected weak JWT secret to be rejected")
	}
}

func TestValidateProductionRejectsWeakSFUJWTSecret(t *testing.T) {
	cfg := validProductionConfig()
	cfg.SFUJWTSecret = "short"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected weak SFU JWT secret to be rejected")
	}
}

func TestValidateProductionRejectsWildcardOrigin(t *testing.T) {
	cfg := validProductionConfig()
	cfg.AllowedOrigins = []string{"*"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected wildcard origin to be rejected")
	}
}

func TestValidateProductionRejectsUnauthenticatedSFUDevMode(t *testing.T) {
	cfg := validProductionConfig()
	cfg.SFUAllowUnauthenticatedDev = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected unauthenticated SFU dev mode to be rejected")
	}
}

func TestValidateDevelopmentAllowsDefaults(t *testing.T) {
	cfg := &Config{GOEnv: "development"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected development config to allow defaults, got %v", err)
	}
}
