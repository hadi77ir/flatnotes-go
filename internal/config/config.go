package config

import (
	"fmt"
	"log/slog"
	"strings"
)

type AuthType string

const (
	AuthNone     AuthType = "none"
	AuthReadOnly AuthType = "read_only"
	AuthPassword AuthType = "password"
	AuthTOTP     AuthType = "totp"

	MinSecretKeyLength = 32
)

type Config struct {
	Host              string
	Port              int
	TLSCertFile       string
	TLSKeyFile        string
	LogLevel          slog.Level
	Path              string
	PathPrefix        string
	AuthType          AuthType
	Username          string
	Password          string
	SecretKey         string
	SessionExpiryDays int
	TOTPKey           string
	QuickAccessHide   bool
	QuickAccessTitle  string
	QuickAccessTerm   string
	QuickAccessSort   string
	QuickAccessLimit  int
	ClientDistPath    string
}

type Options struct {
	Host              string
	Port              int
	TLSCertFile       string
	TLSKeyFile        string
	LogLevel          string
	Path              string
	PathPrefix        string
	AuthType          string
	Username          string
	Password          string
	SecretKey         string
	SessionExpiryDays int
	TOTPKey           string
	QuickAccessHide   bool
	QuickAccessTitle  string
	QuickAccessTerm   string
	QuickAccessSort   string
	QuickAccessLimit  int
	ClientDistPath    string
}

type Response struct {
	AuthType         AuthType `json:"authType"`
	QuickAccessHide  bool     `json:"quickAccessHide"`
	QuickAccessTitle string   `json:"quickAccessTitle"`
	QuickAccessTerm  string   `json:"quickAccessTerm"`
	QuickAccessSort  string   `json:"quickAccessSort"`
	QuickAccessLimit int      `json:"quickAccessLimit"`
}

func FromOptions(opts Options) (Config, error) {
	cfg := Config{
		Host:              opts.Host,
		Port:              opts.Port,
		TLSCertFile:       opts.TLSCertFile,
		TLSKeyFile:        opts.TLSKeyFile,
		Path:              opts.Path,
		PathPrefix:        opts.PathPrefix,
		AuthType:          AuthType(strings.ToLower(opts.AuthType)),
		Username:          strings.ToLower(opts.Username),
		Password:          opts.Password,
		SecretKey:         opts.SecretKey,
		SessionExpiryDays: opts.SessionExpiryDays,
		TOTPKey:           opts.TOTPKey,
		QuickAccessHide:   opts.QuickAccessHide,
		QuickAccessTitle:  opts.QuickAccessTitle,
		QuickAccessTerm:   opts.QuickAccessTerm,
		QuickAccessSort:   opts.QuickAccessSort,
		QuickAccessLimit:  opts.QuickAccessLimit,
		ClientDistPath:    opts.ClientDistPath,
	}
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Path == "" {
		cfg.Path = "/data"
	}
	if cfg.AuthType == "" {
		cfg.AuthType = AuthPassword
	}
	if cfg.SessionExpiryDays == 0 {
		cfg.SessionExpiryDays = 30
	}
	if cfg.QuickAccessTitle == "" {
		cfg.QuickAccessTitle = "RECENTLY MODIFIED"
	}
	if cfg.QuickAccessTerm == "" {
		cfg.QuickAccessTerm = "*"
	}
	if cfg.QuickAccessSort == "" {
		cfg.QuickAccessSort = "lastModified"
	}
	if cfg.QuickAccessLimit == 0 {
		cfg.QuickAccessLimit = 4
	}
	level, err := ParseLogLevel(opts.LogLevel)
	if err != nil {
		return cfg, err
	}
	cfg.LogLevel = level
	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) Response() Response {
	return Response{
		AuthType:         c.AuthType,
		QuickAccessHide:  c.QuickAccessHide,
		QuickAccessTitle: c.QuickAccessTitle,
		QuickAccessTerm:  c.QuickAccessTerm,
		QuickAccessSort:  c.QuickAccessSort,
		QuickAccessLimit: c.QuickAccessLimit,
	}
}

func ParseLogLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid log level %q", value)
	}
}

func (c Config) validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("FLATNOTES_PORT must be between 1 and 65535")
	}
	switch c.AuthType {
	case AuthNone, AuthReadOnly:
	case AuthPassword, AuthTOTP:
		if c.Username == "" {
			return fmt.Errorf("FLATNOTES_USERNAME must be set")
		}
		if c.Password == "" {
			return fmt.Errorf("FLATNOTES_PASSWORD must be set")
		}
		if c.SecretKey == "" {
			return fmt.Errorf("FLATNOTES_SECRET_KEY must be set")
		}
		if len(c.SecretKey) < MinSecretKeyLength {
			return fmt.Errorf("FLATNOTES_SECRET_KEY must be at least %d characters", MinSecretKeyLength)
		}
		if c.AuthType == AuthTOTP && c.TOTPKey == "" {
			return fmt.Errorf("FLATNOTES_TOTP_KEY must be set")
		}
	default:
		return fmt.Errorf("invalid FLATNOTES_AUTH_TYPE %q", c.AuthType)
	}
	if c.PathPrefix != "" && (!strings.HasPrefix(c.PathPrefix, "/") || strings.HasSuffix(c.PathPrefix, "/")) {
		return fmt.Errorf("FLATNOTES_PATH_PREFIX must start with '/' and not end with '/'")
	}
	switch c.QuickAccessSort {
	case "score", "title", "lastModified":
	default:
		return fmt.Errorf("FLATNOTES_QUICK_ACCESS_SORT must be one of: score, title, lastModified")
	}
	if c.SessionExpiryDays < 1 {
		return fmt.Errorf("FLATNOTES_SESSION_EXPIRY_DAYS must be greater than 0")
	}
	if c.QuickAccessLimit < 1 {
		return fmt.Errorf("FLATNOTES_QUICK_ACCESS_LIMIT must be greater than 0")
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("TLS cert and key must be supplied together")
	}
	return nil
}
