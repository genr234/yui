// Package webapps serves per-app web bundles from a loopback origin
// (contracts §6). Each app gets http://<app-key>.apps.localhost:<port>/, so
// browsers keep apps' storage apart and treat the page as a secure context.
package webapps

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed embed.js
var embedJS []byte

const (
	// AssetTokenTTL is how long an /_asset/<token> URL stays valid.
	AssetTokenTTL = 12 * time.Hour
	maxTokens     = 4096

	hostSuffix = ".apps.localhost"

	contentSecurityPolicy = "default-src 'self' blob: data:; " +
		"script-src 'self' 'unsafe-eval' 'wasm-unsafe-eval' blob:; " +
		"style-src 'self' 'unsafe-inline'; " +
		"media-src 'self' blob: data:; " +
		"connect-src 'self' blob: data:; " +
		"frame-ancestors *"
)

var (
	appIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,126}[a-z0-9_-]$`)
	namePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	keyPattern   = regexp.MustCompile(`^[0-9a-f]{16}$`)

	// ErrDisabled means the server has no port (web_port 0).
	ErrDisabled = errors.New("web apps are disabled on this kiosk")
	// ErrBadApp means the app id is not a valid app id, or never asked for
	// its origin.
	ErrBadApp = errors.New("invalid app id")
	// ErrBadName means a runtime name is not a safe path segment.
	ErrBadName = errors.New("invalid runtime name")
)

// AppKey is the first 16 hex characters of SHA-256(app id).
func AppKey(appID string) string {
	sum := sha256.Sum256([]byte(appID))
	return hex.EncodeToString(sum[:])[:16]
}

// AssetResolver maps a library asset id to its local file. It is asked on
// every request, so an asset that went away stops being served.
type AssetResolver func(assetID string) (path string, ok bool)

// Config configures a Server.
type Config struct {
	// Port is the loopback port (0 disables the server).
	Port int
	// DevBundles holds embedded bundles at app-web/<app id>/ (may be nil).
	DevBundles fs.FS
	// InstalledDir is <ConfigDir>/apps-web; bundles live in <app id>/.
	InstalledDir string
	// Assets resolves library assets for /_asset/<token> (may be nil).
	Assets AssetResolver
	// Now replaces time.Now in tests.
	Now func() time.Time
}

type token struct {
	appKey  string
	assetID string
	expires time.Time
	created time.Time
}

// Server is the loopback web app server.
type Server struct {
	cfg Config

	mu     sync.Mutex
	port   int
	apps   map[string]string            // app key -> app id
	mounts map[string]map[string]string // app key -> runtime name -> dir
	tokens map[string]token
	srv    *http.Server
}

// New returns a Server. It does not listen until Serve.
func New(cfg Config) *Server {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Server{
		cfg:    cfg,
		port:   cfg.Port,
		apps:   map[string]string{},
		mounts: map[string]map[string]string{},
		tokens: map[string]token{},
	}
}

// Port is the port origins are built with: the configured one, or the
// listener's once Serve is running.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

func (s *Server) origin(key string) string {
	return "http://" + key + hostSuffix + ":" + strconv.Itoa(s.port)
}

// Register makes the app's origin answer and returns it. Only registered apps
// pass the Host check.
func (s *Server) Register(appID string) (origin string, hasBundle bool, err error) {
	if !appIDPattern.MatchString(appID) {
		return "", false, ErrBadApp
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.port <= 0 {
		return "", false, ErrDisabled
	}
	key := AppKey(appID)
	s.apps[key] = appID
	return s.origin(key), s.hasBundle(appID), nil
}

func (s *Server) hasBundle(appID string) bool {
	root, ok := s.bundleRoot(appID)
	if !ok {
		return false
	}
	f, err := root.open("index.html")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// Mount serves dir at /_runtime/<name>/ on the app's origin and returns that
// URL. The app must have been registered.
func (s *Server) Mount(appID, name, dir string) (string, error) {
	if !namePattern.MatchString(name) {
		return "", ErrBadName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.port <= 0 {
		return "", ErrDisabled
	}
	key := AppKey(appID)
	if s.apps[key] != appID {
		return "", ErrBadApp
	}
	if s.mounts[key] == nil {
		s.mounts[key] = map[string]string{}
	}
	s.mounts[key][name] = dir
	return s.origin(key) + "/_runtime/" + name + "/", nil
}

// AssetURL issues a URL for a library asset on the app's origin. The token is
// 128 random bits, bound to the app and the asset, and expires after
// AssetTokenTTL.
func (s *Server) AssetURL(appID, assetID string) (string, time.Time, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", time.Time{}, err
	}
	tok := hex.EncodeToString(raw[:])
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.port <= 0 {
		return "", time.Time{}, ErrDisabled
	}
	key := AppKey(appID)
	if s.apps[key] != appID {
		return "", time.Time{}, ErrBadApp
	}
	s.purgeLocked(now)
	exp := now.Add(AssetTokenTTL)
	s.tokens[tok] = token{appKey: key, assetID: assetID, expires: exp, created: now}
	return s.origin(key) + "/_asset/" + tok, exp, nil
}

func (s *Server) purgeLocked(now time.Time) {
	for k, t := range s.tokens {
		if !now.Before(t.expires) {
			delete(s.tokens, k)
		}
	}
	for len(s.tokens) >= maxTokens {
		var oldestKey string
		var oldest time.Time
		for k, t := range s.tokens {
			if oldestKey == "" || t.created.Before(oldest) {
				oldestKey, oldest = k, t.created
			}
		}
		delete(s.tokens, oldestKey)
	}
}

// Listen binds 127.0.0.1 on the configured port.
func (s *Server) Listen() (net.Listener, error) {
	if s.cfg.Port <= 0 {
		return nil, ErrDisabled
	}
	return net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(s.cfg.Port))
}

// Serve serves on ln until Close. It records the listener's port.
func (s *Server) Serve(ln net.Listener) error {
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
		s.mu.Lock()
		s.port = tcp.Port
		s.mu.Unlock()
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	s.mu.Lock()
	s.srv = srv
	s.mu.Unlock()
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Close stops the listener.
func (s *Server) Close() error {
	s.mu.Lock()
	srv := s.srv
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return srv.Close()
	}
	return nil
}

// Handler is the HTTP handler (exported for tests).
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serve) }

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", contentSecurityPolicy)

	key, appID, ok := s.hostApp(r.Host)
	if !ok {
		http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	segs, dir, ok := cleanPath(r)
	if !ok {
		http.NotFound(w, r)
		return
	}

	switch {
	case len(segs) == 2 && segs[0] == "_yui" && segs[1] == "embed.js":
		h.Set("Content-Type", "application/javascript; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, "embed.js", time.Time{}, bytes.NewReader(embedJS))
	case len(segs) == 2 && segs[0] == "_asset":
		s.serveAsset(w, r, key, segs[1])
	case len(segs) >= 2 && segs[0] == "_runtime":
		s.mu.Lock()
		base, ok := s.mounts[key][segs[1]]
		s.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.serveFrom(w, r, diskRoot{base}, segs[2:], dir)
	default:
		root, ok := s.bundleRoot(appID)
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.serveFrom(w, r, root, segs, dir)
	}
}

// hostApp returns the app key and id when host is exactly
// <16 hex>.apps.localhost:<port> for a registered app.
func (s *Server) hostApp(host string) (key, appID string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	suffix := hostSuffix + ":" + strconv.Itoa(s.port)
	host = strings.ToLower(host)
	if !strings.HasSuffix(host, suffix) {
		return "", "", false
	}
	key = strings.TrimSuffix(host, suffix)
	if !keyPattern.MatchString(key) {
		return "", "", false
	}
	appID, ok = s.apps[key]
	return key, appID, ok
}

// cleanPath splits the request path into safe segments. It refuses anything
// that is not a plain relative path: dot segments, empty segments, encoded
// slashes or backslashes, NUL, drive-style colons. dir is true when the path
// ended in "/" (or is the root).
func cleanPath(r *http.Request) (segs []string, dir bool, ok bool) {
	esc := strings.ToLower(r.URL.EscapedPath())
	if strings.Contains(esc, "%2f") || strings.Contains(esc, "%5c") || strings.Contains(esc, "%00") {
		return nil, false, false
	}
	p := r.URL.Path
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00") {
		return nil, false, false
	}
	p = p[1:]
	if p == "" {
		return nil, true, true
	}
	if strings.HasSuffix(p, "/") {
		dir = true
		p = strings.TrimSuffix(p, "/")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, ":") {
			return nil, false, false
		}
		for _, c := range seg {
			if c < 0x20 || c == 0x7f {
				return nil, false, false
			}
		}
		segs = append(segs, seg)
	}
	return segs, dir, true
}

// fileRoot opens files below one root. open takes a slash path that has
// already been cleaned.
type fileRoot interface {
	open(name string) (fs.File, error)
}

type fsRoot struct{ fsys fs.FS }

func (f fsRoot) open(name string) (fs.File, error) { return f.fsys.Open(name) }

// diskRoot refuses any name whose real path leaves the root (symlinks).
type diskRoot struct{ dir string }

func (d diskRoot) open(name string) (fs.File, error) {
	base, err := filepath.EvalSymlinks(d.dir)
	if err != nil {
		return nil, err
	}
	full, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(name)))
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, fs.ErrNotExist
	}
	return os.Open(full)
}

func (s *Server) bundleRoot(appID string) (fileRoot, bool) {
	if appID == "" {
		return nil, false
	}
	if s.cfg.InstalledDir != "" {
		dir := filepath.Join(s.cfg.InstalledDir, appID)
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return diskRoot{dir}, true
		}
	}
	if s.cfg.DevBundles != nil {
		sub := path.Join("app-web", appID)
		if st, err := fs.Stat(s.cfg.DevBundles, sub); err == nil && st.IsDir() {
			if f, err := fs.Sub(s.cfg.DevBundles, sub); err == nil {
				return fsRoot{f}, true
			}
		}
	}
	return nil, false
}

func (s *Server) serveFrom(w http.ResponseWriter, r *http.Request, root fileRoot, segs []string, dir bool) {
	name := path.Join(segs...)
	if dir {
		name = path.Join(name, "index.html")
	}
	if name == "" {
		http.NotFound(w, r)
		return
	}
	f, err := root.open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	serveFile(w, r, f, st)
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request, key, tok string) {
	now := s.cfg.Now()
	s.mu.Lock()
	t, ok := s.tokens[tok]
	if ok && !now.Before(t.expires) {
		delete(s.tokens, tok)
		ok = false
	}
	s.mu.Unlock()
	if !ok || t.appKey != key || s.cfg.Assets == nil {
		http.NotFound(w, r)
		return
	}
	p, ok := s.cfg.Assets(t.assetID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	serveFile(w, r, f, st)
}

func serveFile(w http.ResponseWriter, r *http.Request, f fs.File, st fs.FileInfo) {
	h := w.Header()
	h.Set("Content-Type", contentType(st.Name()))
	h.Set("ETag", fmt.Sprintf(`W/"%x-%x"`, st.Size(), st.ModTime().UnixNano()))
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(f)
		if err != nil {
			http.Error(w, "read failed", http.StatusInternalServerError)
			return
		}
		rs = bytes.NewReader(data)
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), rs)
}

func contentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".wasm":
		return "application/wasm"
	case ".data":
		return "application/octet-stream"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
