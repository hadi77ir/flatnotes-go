package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hadi77ir/flatnotes-go/internal/attachments"
	"github.com/hadi77ir/flatnotes-go/internal/auth"
	"github.com/hadi77ir/flatnotes-go/internal/config"
	"github.com/hadi77ir/flatnotes-go/internal/notes"
	"github.com/hadi77ir/flatnotes-go/internal/privilege"
	"github.com/hadi77ir/flatnotes-go/internal/server"
	"github.com/hadi77ir/flatnotes-go/internal/storage"
	"github.com/urfave/cli/v3"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	cmd := &cli.Command{
		Name:    "flatnotes-go",
		Usage:   "Run the flatnotes-go server",
		Version: version,
		ExtraInfo: func() map[string]string {
			return map[string]string{
				"commit": commit,
				"date":   date,
			}
		},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "host", Value: "0.0.0.0", Usage: "host address to bind", Sources: cli.EnvVars("FLATNOTES_HOST")},
			&cli.IntFlag{Name: "port", Value: 8080, Usage: "port to bind", Sources: cli.EnvVars("FLATNOTES_PORT")},
			&cli.StringFlag{Name: "tls-cert-file", Usage: "TLS certificate file", Sources: cli.EnvVars("FLATNOTES_TLS_CERT_FILE")},
			&cli.StringFlag{Name: "tls-key-file", Usage: "TLS key file", Sources: cli.EnvVars("FLATNOTES_TLS_KEY_FILE")},
			&cli.StringFlag{Name: "log-level", Aliases: []string{"loglevel"}, Value: "", Usage: "log level: debug, info, warn, error", Sources: cli.EnvVars("FLATNOTES_LOG_LEVEL", "LOGLEVEL")},
			&cli.StringFlag{Name: "path", Value: "/data", Usage: "note storage path", Sources: cli.EnvVars("FLATNOTES_PATH")},
			&cli.StringFlag{Name: "path-prefix", Usage: "URL path prefix", Sources: cli.EnvVars("FLATNOTES_PATH_PREFIX")},
			&cli.StringFlag{Name: "auth-type", Value: "password", Usage: "auth mode: none, read_only, password, totp", Sources: cli.EnvVars("FLATNOTES_AUTH_TYPE")},
			&cli.StringFlag{Name: "username", Usage: "login username", Sources: cli.EnvVars("FLATNOTES_USERNAME")},
			&cli.StringFlag{Name: "password", Usage: "login password", Sources: cli.EnvVars("FLATNOTES_PASSWORD")},
			&cli.StringFlag{Name: "secret-key", Usage: "JWT signing secret", Sources: cli.EnvVars("FLATNOTES_SECRET_KEY")},
			&cli.IntFlag{Name: "session-expiry-days", Value: 30, Usage: "session expiry in days", Sources: cli.EnvVars("FLATNOTES_SESSION_EXPIRY_DAYS")},
			&cli.StringFlag{Name: "totp-key", Usage: "TOTP secret key", Sources: cli.EnvVars("FLATNOTES_TOTP_KEY")},
			&cli.BoolFlag{Name: "quick-access-hide", Aliases: []string{"hide-recently-modified"}, Usage: "hide quick access notes", Sources: cli.EnvVars("FLATNOTES_QUICK_ACCESS_HIDE", "FLATNOTES_HIDE_RECENTLY_MODIFIED")},
			&cli.StringFlag{Name: "quick-access-title", Value: "RECENTLY MODIFIED", Usage: "quick access title", Sources: cli.EnvVars("FLATNOTES_QUICK_ACCESS_TITLE")},
			&cli.StringFlag{Name: "quick-access-term", Value: "*", Usage: "quick access search term", Sources: cli.EnvVars("FLATNOTES_QUICK_ACCESS_TERM")},
			&cli.StringFlag{Name: "quick-access-sort", Value: "lastModified", Usage: "quick access sort: score, title, lastModified", Sources: cli.EnvVars("FLATNOTES_QUICK_ACCESS_SORT")},
			&cli.IntFlag{Name: "quick-access-limit", Value: 4, Usage: "quick access result limit", Sources: cli.EnvVars("FLATNOTES_QUICK_ACCESS_LIMIT")},
			&cli.StringFlag{Name: "client-dist", Usage: "override embedded frontend with a static asset directory", Sources: cli.EnvVars("FLATNOTES_CLIENT_DIST")},
		},
		Action: run,
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := runHealthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runHealthcheck() error {
	port := os.Getenv("FLATNOTES_PORT")
	if port == "" {
		port = "8080"
	}
	prefix := os.Getenv("FLATNOTES_PATH_PREFIX")
	scheme := "http"
	client := http.Client{Timeout: 5 * time.Second}
	if os.Getenv("FLATNOTES_TLS_CERT_FILE") != "" && os.Getenv("FLATNOTES_TLS_KEY_FILE") != "" {
		scheme = "https"
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // Local container healthcheck against a user-supplied certificate.
	}
	url := scheme + "://127.0.0.1:" + port + strings.TrimRight(prefix, "/") + "/health"
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("healthcheck failed with status %s", resp.Status)
	}
	return nil
}

func run(_ context.Context, cmd *cli.Command) error {
	cfg, err := config.FromOptions(config.Options{
		Host:              cmd.String("host"),
		Port:              cmd.Int("port"),
		TLSCertFile:       cmd.String("tls-cert-file"),
		TLSKeyFile:        cmd.String("tls-key-file"),
		LogLevel:          cmd.String("log-level"),
		Path:              cmd.String("path"),
		PathPrefix:        cmd.String("path-prefix"),
		AuthType:          cmd.String("auth-type"),
		Username:          cmd.String("username"),
		Password:          cmd.String("password"),
		SecretKey:         cmd.String("secret-key"),
		SessionExpiryDays: cmd.Int("session-expiry-days"),
		TOTPKey:           cmd.String("totp-key"),
		QuickAccessHide:   cmd.Bool("quick-access-hide"),
		QuickAccessTitle:  cmd.String("quick-access-title"),
		QuickAccessTerm:   cmd.String("quick-access-term"),
		QuickAccessSort:   cmd.String("quick-access-sort"),
		QuickAccessLimit:  cmd.Int("quick-access-limit"),
		ClientDistPath:    cmd.String("client-dist"),
	})
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
	if err := privilege.DropFromEnv(cfg.Path, logger); err != nil {
		return err
	}
	fileSystem := storage.NewOSFileSystem(cfg.Path)
	noteService, err := notes.NewFileSystemService(fileSystem, logger)
	if err != nil {
		return err
	}
	defer noteService.Close()
	attachmentService, err := attachments.NewFileSystemService(fileSystem)
	if err != nil {
		return err
	}
	var authService auth.Service = auth.NoopService{}
	if cfg.AuthType == config.AuthPassword || cfg.AuthType == config.AuthTOTP {
		authService = auth.NewLocalService(cfg)
	}
	app, err := server.New(cfg, authService, noteService, attachmentService, logger)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              cfg.Host + ":" + strconv.Itoa(cfg.Port),
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if cfg.TLSCertFile != "" {
		logger.Info("starting flatnotes-go", "addr", httpServer.Addr, "tls", true)
		return httpServer.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
	}
	logger.Info("starting flatnotes-go", "addr", httpServer.Addr, "tls", false)
	return httpServer.ListenAndServe()
}
