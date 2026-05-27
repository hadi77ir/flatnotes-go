package server

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hadi77ir/flatnotes-go/internal/attachments"
	"github.com/hadi77ir/flatnotes-go/internal/auth"
	"github.com/hadi77ir/flatnotes-go/internal/config"
	"github.com/hadi77ir/flatnotes-go/internal/notes"
)

type mockAuth struct {
	loginResp  auth.TokenResponse
	loginErr   error
	authErr    error
	loginCalls int
}

func (m *mockAuth) Login(auth.LoginRequest) (auth.TokenResponse, error) {
	m.loginCalls++
	return m.loginResp, m.loginErr
}

func (m *mockAuth) Authenticate(*http.Request) error {
	return m.authErr
}

type mockNotes struct {
	note        notes.Note
	err         error
	searchErr   error
	searchArgs  []interface{}
	deleteErr   error
	tags        []string
	tagsErr     error
	createCalls int
	updateCalls int
	deleteCalls int
}

func (m *mockNotes) Create(notes.CreateRequest) (notes.Note, error) {
	m.createCalls++
	return m.note, m.err
}

func (m *mockNotes) Get(string) (notes.Note, error) {
	return m.note, m.err
}

func (m *mockNotes) Update(string, notes.UpdateRequest) (notes.Note, error) {
	m.updateCalls++
	return m.note, m.err
}

func (m *mockNotes) Delete(string) error {
	m.deleteCalls++
	return m.deleteErr
}

func (m *mockNotes) Search(term, sort, order string, limit int) ([]notes.SearchResult, error) {
	m.searchArgs = []interface{}{term, sort, order, limit}
	return []notes.SearchResult{{Title: "Result", LastModified: 1}}, m.searchErr
}

func (m *mockNotes) GetTags() ([]string, error) {
	return m.tags, m.tagsErr
}

func (m *mockNotes) Close() error {
	return nil
}

type mockAttachments struct {
	resp CreateResponseAlias
	err  error
	path string
}

type CreateResponseAlias = attachments.CreateResponse

func (m *mockAttachments) Create(string, []byte) (attachments.CreateResponse, error) {
	return m.resp, m.err
}

func (m *mockAttachments) Open(string) (string, error) {
	return m.path, m.err
}

func newTestServer(t *testing.T, cfg config.Config, authSvc auth.Service, noteSvc notes.Service, attachmentSvc attachments.Service) http.Handler {
	t.Helper()
	if cfg.AuthType == "" {
		cfg.AuthType = config.AuthNone
	}
	cfg.ClientDistPath = t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg.ClientDistPath, "index.html"), []byte(`<html><head><base href="/" /></head><body>app</body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
	if authSvc == nil {
		authSvc = &mockAuth{}
	}
	if noteSvc == nil {
		body := "content"
		noteSvc = &mockNotes{note: notes.Note{Title: "A", Content: &body, LastModified: 1}}
	}
	if attachmentSvc == nil {
		attachmentSvc = &mockAttachments{}
	}
	srv, err := New(cfg, authSvc, noteSvc, attachmentSvc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return srv.Handler()
}

func TestHealthConfigUIAndSecurityHeaders(t *testing.T) {
	handler := newTestServer(t, config.Config{AuthType: config.AuthNone, PathPrefix: "/flat"}, nil, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/flat/health", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d", rec.Code)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing security headers: %v", rec.Header())
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("missing CSP: %q", csp)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/flat/", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ui status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `<base href="/flat/"`) {
		t.Fatalf("base href not replaced: %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/wrong/health", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("wrong prefix status = %d", rec.Code)
	}
}

func TestAuthenticationAndTokenEndpoint(t *testing.T) {
	authSvc := &mockAuth{authErr: auth.ErrInvalidCredentials}
	handler := newTestServer(t, config.Config{AuthType: config.AuthPassword}, authSvc, nil, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth-check", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("auth-check status = %d", rec.Code)
	}
	authSvc.authErr = nil
	authSvc.loginResp = auth.TokenResponse{AccessToken: "token", TokenType: "bearer"}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/token", strings.NewReader(`{"username":"u","password":"p"}`))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("token status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/token", strings.NewReader(`{"username":"u","password":"p","extra":true}`))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown JSON field status = %d", rec.Code)
	}
}

func TestTokenEndpointRateLimitsFailedLogins(t *testing.T) {
	authSvc := &mockAuth{loginErr: auth.ErrInvalidCredentials}
	handler := newTestServer(t, config.Config{AuthType: config.AuthPassword}, authSvc, nil, nil)
	for i := 0; i < loginLimitMax; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/token", strings.NewReader(`{"username":"u","password":"bad"}`))
		req.RemoteAddr = "192.0.2.10:1234"
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d", i+1, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/token", strings.NewReader(`{"username":"u","password":"bad"}`))
	req.RemoteAddr = "192.0.2.10:1234"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("limited status=%d body=%s", rec.Code, rec.Body.String())
	}
	if authSvc.loginCalls != loginLimitMax {
		t.Fatalf("login calls = %d", authSvc.loginCalls)
	}
}

func TestTokenEndpointSuccessfulLoginClearsRateLimit(t *testing.T) {
	authSvc := &mockAuth{loginErr: auth.ErrInvalidCredentials}
	handler := newTestServer(t, config.Config{AuthType: config.AuthPassword}, authSvc, nil, nil)
	for i := 0; i < loginLimitMax-1; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/token", strings.NewReader(`{"username":"u","password":"bad"}`))
		req.RemoteAddr = "192.0.2.11:1234"
		handler.ServeHTTP(rec, req)
	}
	authSvc.loginErr = nil
	authSvc.loginResp = auth.TokenResponse{AccessToken: "token", TokenType: "bearer"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/token", strings.NewReader(`{"username":"u","password":"good"}`))
	req.RemoteAddr = "192.0.2.11:1234"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("success status=%d body=%s", rec.Code, rec.Body.String())
	}
	authSvc.loginErr = auth.ErrInvalidCredentials
	for i := 0; i < loginLimitMax-1; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/token", strings.NewReader(`{"username":"u","password":"bad"}`))
		req.RemoteAddr = "192.0.2.11:1234"
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("post-reset attempt %d status=%d", i+1, rec.Code)
		}
	}
}

func TestOverrideStaticFileSystemRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "leak.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	staticFS, err := staticFileSystem(root)
	if err != nil {
		t.Fatal(err)
	}
	file, err := staticFS.Open("leak.txt")
	if err == nil {
		_ = file.Close()
		t.Fatal("symlink escape opened successfully")
	}
	if !errors.Is(err, os.ErrPermission) && !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestNoteRoutesMapErrorsAndReadOnly(t *testing.T) {
	content := "x"
	noteSvc := &mockNotes{note: notes.Note{Title: "A", Content: &content, LastModified: 1}}
	handler := newTestServer(t, config.Config{AuthType: config.AuthNone}, nil, noteSvc, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/notes", strings.NewReader(`{"title":"A","content":"x"}`))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || noteSvc.createCalls != 1 {
		t.Fatalf("create status=%d calls=%d body=%s", rec.Code, noteSvc.createCalls, rec.Body.String())
	}
	noteSvc.err = notes.ErrInvalidTitle
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/notes/bad:name", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid title status = %d", rec.Code)
	}
	noteSvc.err = notes.ErrExists
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/api/notes/A", strings.NewReader(`{"newTitle":"B"}`))
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("exists status = %d", rec.Code)
	}
	noteSvc.err = notes.ErrNotFound
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/notes/A", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d", rec.Code)
	}
	readonly := newTestServer(t, config.Config{AuthType: config.AuthReadOnly}, &mockAuth{}, noteSvc, nil)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/notes", strings.NewReader(`{"title":"A"}`))
	readonly.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("read-only create status = %d", rec.Code)
	}
}

func TestSearchValidationAndTags(t *testing.T) {
	noteSvc := &mockNotes{tags: []string{"a", "b"}}
	handler := newTestServer(t, config.Config{AuthType: config.AuthNone}, nil, noteSvc, nil)
	for _, target := range []string{
		"/api/search?term=x&limit=bad",
		"/api/search?term=x&limit=0",
		"/api/search?term=x&limit=10001",
		"/api/search?term=x&sort=bad",
		"/api/search?term=x&order=bad",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d", target, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/search?term=x&sort=title&order=asc&limit=2", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := noteSvc.searchArgs; len(got) != 4 || got[0] != "x" || got[1] != "title" || got[2] != "asc" || got[3] != 2 {
		t.Fatalf("search args = %#v", got)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "a") {
		t.Fatalf("tags status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAttachmentRoutes(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	attachmentSvc := &mockAttachments{resp: attachments.CreateResponse{Filename: "file.txt", URL: "attachments/file.txt"}, path: file}
	handler := newTestServer(t, config.Config{AuthType: config.AuthNone}, nil, nil, attachmentSvc)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("content")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/attachments", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "file.txt") {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/attachments/file.txt", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "content" {
		t.Fatalf("download status=%d body=%q", rec.Code, rec.Body.String())
	}
	attachmentSvc.err = attachments.ErrInvalidFilename
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/attachments/bad:name", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid attachment status = %d", rec.Code)
	}
	attachmentSvc.err = errors.New("boom")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/attachments/file.txt", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("attachment internal status = %d", rec.Code)
	}
}
