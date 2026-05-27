package config

import (
	"log/slog"
	"strings"
	"testing"
)

func TestFromOptionsDefaultsForNoAuth(t *testing.T) {
	cfg, err := FromOptions(Options{AuthType: string(AuthNone)})
	if err != nil {
		t.Fatalf("FromOptions() error = %v", err)
	}
	if cfg.Host != "0.0.0.0" {
		t.Fatalf("Host = %q", cfg.Host)
	}
	if cfg.Port != 8080 {
		t.Fatalf("Port = %d", cfg.Port)
	}
	if cfg.Path != "/data" {
		t.Fatalf("Path = %q", cfg.Path)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("LogLevel = %v", cfg.LogLevel)
	}
	if cfg.QuickAccessLimit != 4 {
		t.Fatalf("QuickAccessLimit = %d", cfg.QuickAccessLimit)
	}
}

func TestFromOptionsPasswordAuthRequiresCredentials(t *testing.T) {
	_, err := FromOptions(Options{AuthType: string(AuthPassword)})
	if err == nil || !strings.Contains(err.Error(), "FLATNOTES_USERNAME") {
		t.Fatalf("expected username error, got %v", err)
	}
}

func TestFromOptionsTOTPRequiresKey(t *testing.T) {
	_, err := FromOptions(Options{
		AuthType:  string(AuthTOTP),
		Username:  "user",
		Password:  "pass",
		SecretKey: "0123456789abcdef0123456789abcdef",
	})
	if err == nil || !strings.Contains(err.Error(), "FLATNOTES_TOTP_KEY") {
		t.Fatalf("expected TOTP key error, got %v", err)
	}
}

func TestFromOptionsValidatesEnumsAndTLS(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{name: "auth", opts: Options{AuthType: "bad"}, want: "FLATNOTES_AUTH_TYPE"},
		{name: "path prefix missing slash", opts: Options{AuthType: string(AuthNone), PathPrefix: "flatnotes"}, want: "FLATNOTES_PATH_PREFIX"},
		{name: "path prefix trailing slash", opts: Options{AuthType: string(AuthNone), PathPrefix: "/flatnotes/"}, want: "FLATNOTES_PATH_PREFIX"},
		{name: "quick sort", opts: Options{AuthType: string(AuthNone), QuickAccessSort: "bad"}, want: "FLATNOTES_QUICK_ACCESS_SORT"},
		{name: "tls pair", opts: Options{AuthType: string(AuthNone), TLSCertFile: "cert.pem"}, want: "TLS cert and key"},
		{name: "log level", opts: Options{AuthType: string(AuthNone), LogLevel: "trace"}, want: "invalid log level"},
		{name: "port low", opts: Options{AuthType: string(AuthNone), Port: -1}, want: "FLATNOTES_PORT"},
		{name: "port high", opts: Options{AuthType: string(AuthNone), Port: 70000}, want: "FLATNOTES_PORT"},
		{name: "session expiry", opts: Options{AuthType: string(AuthNone), SessionExpiryDays: -1}, want: "FLATNOTES_SESSION_EXPIRY_DAYS"},
		{name: "quick limit", opts: Options{AuthType: string(AuthNone), QuickAccessLimit: -1}, want: "FLATNOTES_QUICK_ACCESS_LIMIT"},
		{name: "short secret", opts: Options{AuthType: string(AuthPassword), Username: "user", Password: "pass", SecretKey: "short"}, want: "FLATNOTES_SECRET_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FromOptions(tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}

func TestFromOptionsPasswordAuthValid(t *testing.T) {
	cfg, err := FromOptions(Options{
		AuthType:    string(AuthPassword),
		Username:    "USER",
		Password:    "pass",
		SecretKey:   "0123456789abcdef0123456789abcdef",
		LogLevel:    "debug",
		PathPrefix:  "/flatnotes",
		TLSCertFile: "cert.pem",
		TLSKeyFile:  "key.pem",
	})
	if err != nil {
		t.Fatalf("FromOptions() error = %v", err)
	}
	if cfg.Username != "user" {
		t.Fatalf("Username = %q", cfg.Username)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("LogLevel = %v", cfg.LogLevel)
	}
}
