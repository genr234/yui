package webapps

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const testPort = 26780

type fixture struct {
	t         *testing.T
	srv       *Server
	installed string
	assets    map[string]string
	now       time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, installed: t.TempDir(), assets: map[string]string{}, now: time.Unix(1_700_000_000, 0)}
	dev := fstest.MapFS{
		"app-web/dev.demo/index.html":     {Data: []byte("<h1>dev</h1>")},
		"app-web/dev.demo/sub/index.html": {Data: []byte("sub")},
		"app-web/dev.demo/engine.wasm":    {Data: []byte("\x00asm")},
		"app-web/dev.demo/blob.data":      {Data: []byte("data")},
	}
	f.srv = New(Config{
		Port:         testPort,
		DevBundles:   dev,
		InstalledDir: f.installed,
		Assets: func(id string) (string, bool) {
			p, ok := f.assets[id]
			return p, ok
		},
		Now: func() time.Time { return f.now },
	})
	return f
}

func (f *fixture) origin(appID string) string {
	f.t.Helper()
	o, _, err := f.srv.Register(appID)
	if err != nil {
		f.t.Fatalf("register %s: %v", appID, err)
	}
	return o
}

func hostOf(origin string) string { return strings.TrimPrefix(origin, "http://") }

// get sends a request straight to the handler with the given Host.
func (f *fixture) do(method, host, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestOriginShape(t *testing.T) {
	f := newFixture(t)
	origin, has, err := f.srv.Register("dev.demo")
	if err != nil || !has {
		t.Fatalf("register: has=%v err=%v", has, err)
	}
	want := "http://" + AppKey("dev.demo") + ".apps.localhost:26780"
	if origin != want {
		t.Fatalf("origin = %q, want %q", origin, want)
	}
	if len(AppKey("dev.demo")) != 16 {
		t.Fatal("app key must be 16 hex characters")
	}
	if _, has, _ := f.srv.Register("other.app"); has {
		t.Fatal("app without a bundle must report has_bundle=false")
	}
	if _, _, err := f.srv.Register("../evil"); err == nil {
		t.Fatal("invalid app id must be refused")
	}
}

func TestDisabledServer(t *testing.T) {
	s := New(Config{Port: 0})
	if _, _, err := s.Register("dev.demo"); err != ErrDisabled {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestHostCheck(t *testing.T) {
	f := newFixture(t)
	host := hostOf(f.origin("dev.demo"))
	key := AppKey("dev.demo")
	unknown := AppKey("never.asked")

	cases := []struct {
		name string
		host string
		want int
	}{
		{"registered", host, 200},
		{"uppercase", strings.ToUpper(host), 200},
		{"no port", key + ".apps.localhost", 421},
		{"wrong port", key + ".apps.localhost:1234", 421},
		{"plain localhost", "localhost:26780", 421},
		{"loopback ip", "127.0.0.1:26780", 421},
		{"unknown app", unknown + ".apps.localhost:26780", 421},
		{"rebind domain", key + ".apps.localhost.evil.test:26780", 421},
		{"prefix trick", "x" + key + ".apps.localhost:26780", 421},
		{"short key", key[:15] + ".apps.localhost:26780", 421},
		{"empty", "", 421},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := f.do("GET", c.host, "/", nil)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d", rec.Code, c.want)
			}
		})
	}
}

func TestMethods(t *testing.T) {
	f := newFixture(t)
	host := hostOf(f.origin("dev.demo"))
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS"} {
		if rec := f.do(m, host, "/", nil); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status %d, want 405", m, rec.Code)
		}
	}
	head := f.do("HEAD", host, "/", nil)
	if head.Code != 200 || head.Body.Len() != 0 {
		t.Fatalf("HEAD: status %d, body %d bytes", head.Code, head.Body.Len())
	}
}

func TestSecurityHeadersAndMIME(t *testing.T) {
	f := newFixture(t)
	host := hostOf(f.origin("dev.demo"))
	for _, target := range []string{"/", "/engine.wasm", "/missing", "/_yui/embed.js"} {
		rec := f.do("GET", host, target, nil)
		h := rec.Header()
		if h.Get("X-Content-Type-Options") != "nosniff" ||
			h.Get("Cross-Origin-Resource-Policy") != "same-origin" ||
			h.Get("Referrer-Policy") != "no-referrer" {
			t.Errorf("%s: missing security headers: %v", target, h)
		}
		csp := h.Get("Content-Security-Policy")
		for _, part := range []string{
			"default-src 'self' blob: data:",
			"script-src 'self' 'unsafe-eval' 'wasm-unsafe-eval' blob:",
			"style-src 'self' 'unsafe-inline'",
			"media-src 'self' blob: data:",
			"connect-src 'self' blob: data:",
			"frame-ancestors *",
		} {
			if !strings.Contains(csp, part) {
				t.Errorf("%s: CSP %q lacks %q", target, csp, part)
			}
		}
	}
	// Even a refused Host gets the headers.
	if rec := f.do("GET", "evil.test", "/", nil); rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("421 response lacks CSP")
	}
	if ct := f.do("GET", host, "/engine.wasm", nil).Header().Get("Content-Type"); ct != "application/wasm" {
		t.Errorf("wasm content type = %q", ct)
	}
	if ct := f.do("GET", host, "/blob.data", nil).Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf(".data content type = %q", ct)
	}
	if ct := f.do("GET", host, "/", nil).Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("index content type = %q", ct)
	}
}

func TestIndexAndNoListing(t *testing.T) {
	f := newFixture(t)
	host := hostOf(f.origin("dev.demo"))
	if rec := f.do("GET", host, "/", nil); rec.Code != 200 || rec.Body.String() != "<h1>dev</h1>" {
		t.Fatalf("/: %d %q", rec.Code, rec.Body.String())
	}
	if rec := f.do("GET", host, "/sub/", nil); rec.Code != 200 || rec.Body.String() != "sub" {
		t.Fatalf("/sub/: %d %q", rec.Code, rec.Body.String())
	}
	if rec := f.do("GET", host, "/sub", nil); rec.Code != 404 {
		t.Fatalf("/sub without slash: %d, want 404", rec.Code)
	}
	if rec := f.do("GET", host, "/nothing/", nil); rec.Code != 404 {
		t.Fatalf("/nothing/: %d, want 404", rec.Code)
	}
}

func TestInstalledBundleOnDisk(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.installed, "disk.app")
	if err := os.MkdirAll(filepath.Join(dir, "js"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "index.html"), "disk index")
	mustWrite(t, filepath.Join(dir, "js", "a.js"), "console.log(1)")
	host := hostOf(f.origin("disk.app"))
	if rec := f.do("GET", host, "/", nil); rec.Body.String() != "disk index" {
		t.Fatalf("index: %q", rec.Body.String())
	}
	rec := f.do("GET", host, "/js/a.js", nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("a.js: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTraversal(t *testing.T) {
	f := newFixture(t)
	// A secret next to the installed bundle root, reachable by traversal.
	mustWrite(t, filepath.Join(f.installed, "secret.txt"), "TOP SECRET")
	dir := filepath.Join(f.installed, "disk.app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "index.html"), "ok")
	host := hostOf(f.origin("disk.app"))

	paths := []string{
		"/../secret.txt",
		"/..%2fsecret.txt",
		"/%2e%2e/secret.txt",
		"/%2E%2E/secret.txt",
		"/.%2e/secret.txt",
		"/a/../../secret.txt",
		"/a%2f..%2f..%2fsecret.txt",
		"/..%5csecret.txt",
		"/..\\secret.txt",
		"/%5c..%5csecret.txt",
		"/index.html%00.txt",
		"/index.html%00",
		"//etc/passwd",
		"/%2fetc/passwd",
		"/C:/Windows/win.ini",
		"/C%3A/Windows/win.ini",
		"/./index.html",
		"/_runtime/../secret.txt",
		"/_asset/../secret.txt",
	}
	for _, p := range paths {
		req := &http.Request{Method: "GET", Host: host, URL: &url.URL{}, Header: http.Header{}}
		if u, err := url.ParseRequestURI(p); err == nil {
			req.URL = u
		} else {
			req.URL.Path = p
		}
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		if rec.Code == 200 || strings.Contains(rec.Body.String(), "TOP SECRET") {
			t.Errorf("%q: status %d body %q", p, rec.Code, rec.Body.String())
		}
	}

	// Raw NUL in the decoded path, as a client could not send but a proxy might.
	req := httptest.NewRequest("GET", "/index.html", nil)
	req.Host = host
	req.URL.Path = "/index.html\x00.txt"
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("NUL path: %d", rec.Code)
	}
}

func TestSymlinkEscapeRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	f := newFixture(t)
	outside := t.TempDir()
	mustWrite(t, filepath.Join(outside, "secret.txt"), "TOP SECRET")
	dir := filepath.Join(f.installed, "disk.app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "index.html"), "ok")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "dirlink")); err != nil {
		t.Fatal(err)
	}
	// A symlink that stays inside the root is fine.
	if err := os.Symlink(filepath.Join(dir, "index.html"), filepath.Join(dir, "inside.html")); err != nil {
		t.Fatal(err)
	}
	host := hostOf(f.origin("disk.app"))
	for _, p := range []string{"/link.txt", "/dirlink/secret.txt"} {
		rec := f.do("GET", host, p, nil)
		if rec.Code != 404 || strings.Contains(rec.Body.String(), "TOP SECRET") {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
	}
	if rec := f.do("GET", host, "/inside.html", nil); rec.Code != 200 {
		t.Errorf("inside symlink: %d", rec.Code)
	}
}

func TestEmbedHelperServedToEveryBundle(t *testing.T) {
	f := newFixture(t)
	for _, app := range []string{"dev.demo", "no.bundle"} {
		host := hostOf(f.origin(app))
		rec := f.do("GET", host, "/_yui/embed.js", nil)
		if rec.Code != 200 {
			t.Fatalf("%s: %d", app, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
			t.Errorf("content type %q", ct)
		}
		if !strings.Contains(rec.Body.String(), "window.yui") {
			t.Errorf("%s: body is not the embed helper", app)
		}
	}
	// The avatar renderer and its atlas ride along the same way.
	host := hostOf(f.origin("dev.demo"))
	if rec := f.do("GET", host, "/_yui/avatar.js", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "yui-avatar") {
		t.Errorf("avatar.js: %d", rec.Code)
	}
	if rec := f.do("GET", host, "/_yui/avatar-atlas.png", nil); rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("avatar-atlas.png: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := f.do("GET", hostOf(f.origin("dev.demo")), "/_yui/other.js", nil); rec.Code != 404 {
		t.Errorf("unknown _yui path: %d", rec.Code)
	}
}

func TestRuntimeMountRequiresMount(t *testing.T) {
	f := newFixture(t)
	host := hostOf(f.origin("dev.demo"))
	other := hostOf(f.origin("other.app"))
	if rec := f.do("GET", host, "/_runtime/engine/core.wasm", nil); rec.Code != 404 {
		t.Fatalf("unmounted: %d, want 404", rec.Code)
	}

	rt := t.TempDir()
	mustWrite(t, filepath.Join(rt, "core.wasm"), "\x00asm")
	mustWrite(t, filepath.Join(rt, "index.html"), "rt index")
	if _, err := f.srv.Mount("dev.demo", "../engine", rt); err != ErrBadName {
		t.Fatalf("bad name: %v", err)
	}
	if _, err := f.srv.Mount("never.registered", "engine", rt); err == nil {
		t.Fatal("mount for an app that never asked must fail")
	}
	url, err := f.srv.Mount("dev.demo", "engine", rt)
	if err != nil || url != f.origin("dev.demo")+"/_runtime/engine/" {
		t.Fatalf("mount: %q %v", url, err)
	}
	rec := f.do("GET", host, "/_runtime/engine/core.wasm", nil)
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/wasm" {
		t.Fatalf("mounted: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := f.do("GET", host, "/_runtime/engine/", nil); rec.Body.String() != "rt index" {
		t.Fatalf("runtime index: %q", rec.Body.String())
	}
	// Another app does not get the mount.
	if rec := f.do("GET", other, "/_runtime/engine/core.wasm", nil); rec.Code != 404 {
		t.Fatalf("other app: %d, want 404", rec.Code)
	}
	for _, p := range []string{"/_runtime/engine/../core.wasm", "/_runtime/engine/%2e%2e/x"} {
		if rec := f.do("GET", host, p, nil); rec.Code == 200 {
			t.Errorf("%s served", p)
		}
	}
}

func (f *fixture) assetFile(id string, content []byte, name string) {
	f.t.Helper()
	p := filepath.Join(f.t.TempDir(), name)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		f.t.Fatal(err)
	}
	f.assets[id] = p
}

func tokenPath(t *testing.T, assetURL string) string {
	t.Helper()
	u, err := url.Parse(assetURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Path
}

func TestAssetRangeAndHeaders(t *testing.T) {
	f := newFixture(t)
	content := []byte("0123456789abcdefghij") // 20 bytes
	f.assetFile("7", content, "song.mp3")
	origin := f.origin("dev.demo")
	host := hostOf(origin)
	assetURL, exp, err := f.srv.AssetURL("dev.demo", "7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(assetURL, origin+"/_asset/") {
		t.Fatalf("url = %q", assetURL)
	}
	if tok := strings.TrimPrefix(tokenPath(t, assetURL), "/_asset/"); len(tok) != 32 {
		t.Fatalf("token %q must be 128 bits of hex", tok)
	}
	if want := f.now.Add(12 * time.Hour); !exp.Equal(want) {
		t.Fatalf("expires %v, want %v", exp, want)
	}
	p := tokenPath(t, assetURL)

	rec := f.do("GET", host, p, nil)
	if rec.Code != 200 || rec.Body.String() != string(content) {
		t.Fatalf("full: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "private, max-age=3600" {
		t.Errorf("cache-control = %q", rec.Header().Get("Cache-Control"))
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("missing ETag")
	}
	if rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Error("missing Accept-Ranges")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("content type = %q", ct)
	}
	etag := rec.Header().Get("ETag")

	// Single range.
	rec = f.do("GET", host, p, map[string]string{"Range": "bytes=2-5"})
	if rec.Code != 206 || rec.Body.String() != "2345" || rec.Header().Get("Content-Range") != "bytes 2-5/20" {
		t.Fatalf("range: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Range"))
	}
	// Open-ended.
	rec = f.do("GET", host, p, map[string]string{"Range": "bytes=15-"})
	if rec.Code != 206 || rec.Body.String() != "fghij" || rec.Header().Get("Content-Range") != "bytes 15-19/20" {
		t.Fatalf("open-ended: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Range"))
	}
	// Suffix.
	rec = f.do("GET", host, p, map[string]string{"Range": "bytes=-3"})
	if rec.Code != 206 || rec.Body.String() != "hij" {
		t.Fatalf("suffix: %d %q", rec.Code, rec.Body.String())
	}
	// Unsatisfiable.
	rec = f.do("GET", host, p, map[string]string{"Range": "bytes=50-60"})
	if rec.Code != http.StatusRequestedRangeNotSatisfiable || rec.Header().Get("Content-Range") != "bytes */20" {
		t.Fatalf("416: %d %q", rec.Code, rec.Header().Get("Content-Range"))
	}
	// Conditional.
	rec = f.do("GET", host, p, map[string]string{"If-None-Match": etag})
	if rec.Code != http.StatusNotModified {
		t.Fatalf("etag: %d", rec.Code)
	}
	// HEAD
	if rec = f.do("HEAD", host, p, nil); rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != "20" {
		t.Fatalf("HEAD: %d len=%d cl=%q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Length"))
	}
	// Unknown extension.
	f.assetFile("8", content, "blob.zzzunknown")
	u8, _, _ := f.srv.AssetURL("dev.demo", "8")
	if ct := f.do("GET", host, tokenPath(t, u8), nil).Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("unknown ext type = %q", ct)
	}
}

func TestAssetTokenExpiryAndBinding(t *testing.T) {
	f := newFixture(t)
	f.assetFile("7", []byte("hello"), "a.txt")
	hostA := hostOf(f.origin("dev.demo"))
	hostB := hostOf(f.origin("other.app"))

	u, _, err := f.srv.AssetURL("dev.demo", "7")
	if err != nil {
		t.Fatal(err)
	}
	p := tokenPath(t, u)

	if rec := f.do("GET", hostA, p, nil); rec.Code != 200 {
		t.Fatalf("own origin: %d", rec.Code)
	}
	if rec := f.do("GET", hostB, p, nil); rec.Code != 404 {
		t.Fatalf("other app's origin: %d, want 404", rec.Code)
	}
	if rec := f.do("GET", hostA, "/_asset/"+strings.Repeat("0", 32), nil); rec.Code != 404 {
		t.Fatalf("guessed token: %d", rec.Code)
	}

	f.now = f.now.Add(12*time.Hour - time.Second)
	if rec := f.do("GET", hostA, p, nil); rec.Code != 200 {
		t.Fatalf("just before expiry: %d", rec.Code)
	}
	f.now = f.now.Add(time.Second)
	if rec := f.do("GET", hostA, p, nil); rec.Code != 404 {
		t.Fatalf("after expiry: %d, want 404", rec.Code)
	}

	// An app that never asked for its origin cannot get a token.
	if _, _, err := f.srv.AssetURL("never.asked", "7"); err == nil {
		t.Fatal("token for unregistered app")
	}
	// Asset gone from the library stops being served.
	f.now = f.now.Add(-13 * time.Hour)
	u, _, _ = f.srv.AssetURL("dev.demo", "7")
	delete(f.assets, "7")
	if rec := f.do("GET", hostA, tokenPath(t, u), nil); rec.Code != 404 {
		t.Fatalf("removed asset: %d", rec.Code)
	}
}

func TestTokenTableIsBounded(t *testing.T) {
	f := newFixture(t)
	f.origin("dev.demo")
	for i := 0; i < maxTokens+50; i++ {
		if _, _, err := f.srv.AssetURL("dev.demo", "7"); err != nil {
			t.Fatal(err)
		}
	}
	f.srv.mu.Lock()
	n := len(f.srv.tokens)
	f.srv.mu.Unlock()
	if n > maxTokens {
		t.Fatalf("%d tokens kept", n)
	}
}

func TestServeOverTCP(t *testing.T) {
	f := newFixture(t)
	f.srv.cfg.Port = 0
	ln, err := newLoopbackListener()
	if err != nil {
		t.Skip("cannot listen:", err)
	}
	done := make(chan error, 1)
	go func() { done <- f.srv.Serve(ln) }()
	defer func() {
		_ = f.srv.Close()
		<-done
	}()
	// Serve records the listener's port as it starts; wait for that.
	port := fmt.Sprintf(":%d", ln.Addr().(*net.TCPAddr).Port)
	var origin string
	for i := 0; i < 200; i++ {
		var err error
		if origin, _, err = f.srv.Register("dev.demo"); err == nil && strings.HasSuffix(origin, port) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.HasSuffix(origin, port) {
		t.Fatalf("origin %q never used port %s", origin, port)
	}
	req, _ := http.NewRequest("GET", "http://"+ln.Addr().String()+"/", nil)
	req.Host = hostOf(origin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || string(body) != "<h1>dev</h1>" {
		t.Fatalf("%d %q", resp.StatusCode, body)
	}
	// Plain loopback host is refused.
	resp, err = http.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 421 {
		t.Fatalf("plain host: %d", resp.StatusCode)
	}
}
