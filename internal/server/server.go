package server

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hadi77ir/flatnotes-go/internal/attachments"
	"github.com/hadi77ir/flatnotes-go/internal/auth"
	"github.com/hadi77ir/flatnotes-go/internal/config"
	"github.com/hadi77ir/flatnotes-go/internal/messages"
	"github.com/hadi77ir/flatnotes-go/internal/notes"
	"github.com/hadi77ir/flatnotes-go/internal/web"
)

const (
	maxJSONBodyBytes  = 1 << 20
	maxUploadBytes    = 100 << 20
	maxSearchLimit    = 10000
	loginLimitWindow  = 15 * time.Minute
	loginLimitBlock   = 15 * time.Minute
	loginLimitMax     = 5
	loginLimitEntries = 10000
)

type Server struct {
	cfg         config.Config
	auth        auth.Service
	notes       notes.Service
	attachments attachments.Service
	log         *slog.Logger
	mux         *http.ServeMux
	staticFS    fs.FS
	loginLimits *loginLimiter
}

func New(cfg config.Config, authSvc auth.Service, noteSvc notes.Service, attachmentSvc attachments.Service, logger *slog.Logger) (*Server, error) {
	staticFS, err := staticFileSystem(cfg.ClientDistPath)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:         cfg,
		auth:        authSvc,
		notes:       noteSvc,
		attachments: attachmentSvc,
		log:         logger,
		mux:         http.NewServeMux(),
		staticFS:    staticFS,
		loginLimits: newLoginLimiter(loginLimitWindow, loginLimitBlock, loginLimitMax),
	}
	s.routes()
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.securityHeaders(s.withPrefix(s.logRequests(s.mux)))
}

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.health)
	s.mux.HandleFunc("/api/config", s.config)
	s.mux.HandleFunc("/api/auth-check", s.requireAuth(s.authCheck))
	if s.cfg.AuthType != config.AuthNone && s.cfg.AuthType != config.AuthReadOnly {
		s.mux.HandleFunc("/api/token", s.token)
	}
	s.mux.HandleFunc("/api/search", s.requireAuth(s.search))
	s.mux.HandleFunc("/api/tags", s.requireAuth(s.tags))
	s.mux.HandleFunc("/api/notes", s.requireAuth(s.notesCollection))
	s.mux.HandleFunc("/api/notes/", s.requireAuth(s.noteItem))
	s.mux.HandleFunc("/api/attachments", s.requireAuth(s.attachmentCollection))
	s.mux.HandleFunc("/api/attachments/", s.requireAuth(s.attachmentItem))
	s.mux.HandleFunc("/attachments/", s.requireAuth(s.attachmentItem))
	s.mux.HandleFunc("/", s.ui)
}

func (s *Server) withPrefix(next http.Handler) http.Handler {
	if s.cfg.PathPrefix == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, s.cfg.PathPrefix+"/") && r.URL.Path != s.cfg.PathPrefix {
			http.NotFound(w, r)
			return
		}
		r.URL.Path = strings.TrimPrefix(r.URL.Path, s.cfg.PathPrefix)
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path.Join(s.cfg.PathPrefix, "/health") {
			s.log.Debug("request", "method", r.Method, "path", r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthType == config.AuthNone {
			next(w, r)
			return
		}
		if err := s.auth.Authenticate(r); err != nil {
			writeError(w, http.StatusUnauthorized, "Invalid authentication credentials")
			return
		}
		next(w, r)
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, "OK")
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.cfg.Response())
}

func (s *Server) authCheck(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, "OK")
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var data auth.LoginRequest
	if err := decodeJSON(w, r, &data); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	limitKey := loginLimitKey(r, data.Username)
	if !s.loginLimits.allow(limitKey) {
		writeError(w, http.StatusTooManyRequests, "Too many login attempts. Please try again later.")
		return
	}
	token, err := s.auth.Login(data)
	if err != nil {
		s.loginLimits.fail(limitKey)
		writeError(w, http.StatusUnauthorized, messages.LoginFailed)
		return
	}
	s.loginLimits.reset(limitKey)
	writeJSON(w, http.StatusOK, token)
}

func (s *Server) notesCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.cfg.AuthType == config.AuthReadOnly {
		methodNotAllowed(w)
		return
	}
	var data notes.CreateRequest
	if err := decodeJSON(w, r, &data); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	note, err := s.notes.Create(data)
	s.writeNoteResult(w, note, err)
}

func (s *Server) noteItem(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimPrefix(r.URL.Path, "/api/notes/")
	switch r.Method {
	case http.MethodGet:
		note, err := s.notes.Get(title)
		s.writeNoteResult(w, note, err)
	case http.MethodPatch:
		if s.cfg.AuthType == config.AuthReadOnly {
			methodNotAllowed(w)
			return
		}
		var data notes.UpdateRequest
		if err := decodeJSON(w, r, &data); err != nil {
			writeError(w, http.StatusBadRequest, "Invalid request body.")
			return
		}
		note, err := s.notes.Update(title, data)
		s.writeNoteResult(w, note, err)
	case http.MethodDelete:
		if s.cfg.AuthType == config.AuthReadOnly {
			methodNotAllowed(w)
			return
		}
		err := s.notes.Delete(title)
		if err != nil {
			s.writeNoteResult(w, notes.Note{}, err)
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("term")
	sortBy := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxSearchLimit {
			writeError(w, http.StatusBadRequest, "Invalid limit.")
			return
		}
		limit = parsed
	}
	if sortBy == "" {
		sortBy = "score"
	}
	if order == "" {
		order = "desc"
	}
	if sortBy != "score" && sortBy != "title" && sortBy != "lastModified" {
		writeError(w, http.StatusBadRequest, "Invalid sort.")
		return
	}
	if order != "asc" && order != "desc" {
		writeError(w, http.StatusBadRequest, "Invalid order.")
		return
	}
	results, err := s.notes.Search(term, sortBy, order, limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *Server) tags(w http.ResponseWriter, _ *http.Request) {
	tags, err := s.notes.GetTags()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *Server) attachmentCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if s.cfg.AuthType == config.AuthReadOnly {
		methodNotAllowed(w)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Missing file.")
		return
	}
	defer file.Close()
	data, err := readUpload(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := s.attachments.Create(header.Filename, data)
	if err != nil {
		switch {
		case errors.Is(err, attachments.ErrInvalidFilename):
			writeError(w, http.StatusBadRequest, messages.InvalidAttachmentFilename)
		case errors.Is(err, attachments.ErrExists):
			writeError(w, http.StatusConflict, messages.AttachmentExists)
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) attachmentItem(w http.ResponseWriter, r *http.Request) {
	var filename string
	if strings.HasPrefix(r.URL.Path, "/api/attachments/") {
		filename = strings.TrimPrefix(r.URL.Path, "/api/attachments/")
	} else {
		filename = strings.TrimPrefix(r.URL.Path, "/attachments/")
	}
	filepath, err := s.attachments.Open(filename)
	if err != nil {
		switch {
		case errors.Is(err, attachments.ErrInvalidFilename):
			writeError(w, http.StatusBadRequest, messages.InvalidAttachmentFilename)
		case errors.Is(err, attachments.ErrNotFound):
			writeError(w, http.StatusNotFound, messages.AttachmentNotFound)
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	http.ServeFile(w, r, filepath)
}

func (s *Server) ui(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	if r.URL.Path == "/" || r.URL.Path == "/login" || r.URL.Path == "/search" || r.URL.Path == "/new" || strings.HasPrefix(r.URL.Path, "/note/") {
		html, err := fs.ReadFile(s.staticFS, "index.html")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(replaceBaseHref(html, s.cfg.PathPrefix))
		return
	}
	http.FileServer(http.FS(s.staticFS)).ServeHTTP(w, r)
}

func staticFileSystem(overridePath string) (fs.FS, error) {
	if overridePath != "" {
		override, err := newSafeDirFS(overridePath)
		if err != nil {
			return nil, err
		}
		return noListFS{FS: override}, nil
	}
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return nil, err
	}
	return noListFS{FS: dist}, nil
}

type safeDirFS struct {
	root     string
	realRoot string
}

func newSafeDirFS(root string) (safeDirFS, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return safeDirFS{}, err
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return safeDirFS{}, err
	}
	if !info.IsDir() {
		return safeDirFS{}, fs.ErrInvalid
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return safeDirFS{}, err
	}
	return safeDirFS{root: absRoot, realRoot: realRoot}, nil
}

func (f safeDirFS) Open(name string) (fs.File, error) {
	if name != "." && !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	cleaned := filepath.Clean(filepath.FromSlash(name))
	target := filepath.Join(f.root, cleaned)
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, err
	}
	if realTarget != f.realRoot && !strings.HasPrefix(realTarget, f.realRoot+string(os.PathSeparator)) {
		return nil, fs.ErrPermission
	}
	return os.Open(realTarget)
}

type noListFS struct {
	fs.FS
}

func (f noListFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.IsDir() {
		if _, err := fs.Stat(f.FS, path.Join(name, "index.html")); err != nil {
			_ = file.Close()
			return nil, fs.ErrNotExist
		}
	}
	return file, nil
}

func (s *Server) writeNoteResult(w http.ResponseWriter, note notes.Note, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, note)
		return
	}
	switch {
	case errors.Is(err, notes.ErrInvalidTitle):
		writeError(w, http.StatusBadRequest, messages.InvalidNoteTitle)
	case errors.Is(err, notes.ErrExists):
		writeError(w, http.StatusConflict, messages.NoteExists)
	case errors.Is(err, notes.ErrNotFound):
		writeError(w, http.StatusNotFound, messages.NoteNotFound)
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func readUpload(file multipart.File) ([]byte, error) {
	return io.ReadAll(file)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func replaceBaseHref(input []byte, prefix string) []byte {
	text := string(input)
	lower := strings.ToLower(text)
	baseStart := strings.Index(lower, "<base")
	if baseStart < 0 {
		return input
	}
	tagEnd := strings.Index(text[baseStart:], ">")
	if tagEnd < 0 {
		return input
	}
	tagEnd += baseStart
	hrefStart := strings.Index(strings.ToLower(text[baseStart:tagEnd]), `href="`)
	if hrefStart < 0 {
		return input
	}
	hrefStart += baseStart + len(`href="`)
	hrefEnd := strings.Index(text[hrefStart:tagEnd], `"`)
	if hrefEnd < 0 {
		return input
	}
	hrefEnd += hrefStart
	return []byte(text[:hrefStart] + prefix + "/" + text[hrefEnd:])
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	data := mustJSON(value)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func mustJSON(value interface{}) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		return []byte(`{"detail":"JSON encoding failed."}`)
	}
	return append(data, '\n')
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "Method not allowed.")
}

type loginLimiter struct {
	window     time.Duration
	blockFor   time.Duration
	maxFails   int
	maxEntries int
	now        func() time.Time
	mu         sync.Mutex
	entries    map[string]loginLimitEntry
}

type loginLimitEntry struct {
	failures int
	first    time.Time
	blocked  time.Time
}

func newLoginLimiter(window, blockFor time.Duration, maxFails int) *loginLimiter {
	return &loginLimiter{
		window:     window,
		blockFor:   blockFor,
		maxFails:   maxFails,
		maxEntries: loginLimitEntries,
		now:        time.Now,
		entries:    make(map[string]loginLimitEntry),
	}
}

func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := l.entries[key]
	now := l.now()
	if !entry.blocked.IsZero() {
		if now.Sub(entry.blocked) < l.blockFor {
			return false
		}
		delete(l.entries, key)
		return true
	}
	if !entry.first.IsZero() && now.Sub(entry.first) > l.window {
		delete(l.entries, key)
	}
	return true
}

func (l *loginLimiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	entry := l.entries[key]
	if entry.first.IsZero() || now.Sub(entry.first) > l.window {
		entry = loginLimitEntry{first: now}
	}
	entry.failures++
	if entry.failures >= l.maxFails {
		entry.blocked = now
	}
	l.entries[key] = entry
	if len(l.entries) > l.maxEntries {
		l.prune(now)
	}
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *loginLimiter) prune(now time.Time) {
	for key, entry := range l.entries {
		if (!entry.blocked.IsZero() && now.Sub(entry.blocked) >= l.blockFor) ||
			(entry.blocked.IsZero() && now.Sub(entry.first) > l.window) {
			delete(l.entries, key)
		}
	}
}

func loginLimitKey(r *http.Request, username string) string {
	host := r.RemoteAddr
	if parsed, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = parsed
	}
	return strings.ToLower(strings.TrimSpace(username)) + "\x00" + host
}
