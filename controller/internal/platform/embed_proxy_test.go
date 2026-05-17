package platform

import (
	"net/http/httptest"
	"strings"
	"testing"

	"kiosk/controller/internal/config"
)

func TestEmbedProxyRejectsPrivateAndLocalTargets(t *testing.T) {
	blocked := []string{
		"http://localhost/",
		"http://127.0.0.1/",
		"http://10.0.0.1/",
		"http://172.16.0.1/",
		"http://192.168.1.1/",
		"http://[::1]/",
	}

	for _, rawURL := range blocked {
		if upstream, ok := embedProxyUpstream("/embed-proxy/"+encodeEmbedProxyURL(rawURL), ""); ok {
			t.Fatalf("expected %s to be blocked, got %s", rawURL, upstream)
		}
	}
}

func TestEmbedProxyRequiresRuntimeToken(t *testing.T) {
	r := &Runtime{cfg: config.Config{PlatformBridgeToken: "secret"}}
	req := httptest.NewRequest("GET", "/embed-proxy/"+encodeEmbedProxyURL("https://example.com/"), nil)
	if r.validPlatformHTTPToken(req) {
		t.Fatalf("request without token was accepted")
	}

	req = httptest.NewRequest("GET", "/embed-proxy/"+encodeEmbedProxyURL("https://example.com/")+"?token=secret", nil)
	if !r.validPlatformHTTPToken(req) {
		t.Fatalf("request with token was rejected")
	}

	req = httptest.NewRequest("GET", "/embed-proxy/secret/"+encodeEmbedProxyURL("https://example.com/"), nil)
	if !r.validPlatformHTTPToken(req) {
		t.Fatalf("request with path token was rejected")
	}
}

func TestEmbedProxyDoesNotForwardRuntimeToken(t *testing.T) {
	upstream, ok := embedProxyUpstream("/embed-proxy/"+encodeEmbedProxyURL("https://example.com/page?keep=1"), "token=secret")
	if !ok {
		t.Fatalf("expected upstream URL")
	}
	if strings.Contains(upstream, "token=secret") {
		t.Fatalf("runtime token leaked upstream: %s", upstream)
	}
	if !strings.Contains(upstream, "keep=1") {
		t.Fatalf("upstream query was not preserved: %s", upstream)
	}
}

func TestEmbedProxyPathTokenPreservesRelativeSubresources(t *testing.T) {
	r := &Runtime{cfg: config.Config{PlatformBridgeToken: "secret"}}
	path := r.embedProxyURLFor("https://example.com/path/index.html")
	if !strings.HasPrefix(path, "/embed-proxy/secret/") {
		t.Fatalf("expected token in proxy path, got %s", path)
	}

	upstream, ok := r.embedProxyUpstream(path+"/assets/app.css", "")
	if !ok {
		t.Fatalf("expected subresource upstream URL")
	}
	if upstream != "https://example.com/path/assets/app.css" {
		t.Fatalf("unexpected subresource upstream: %s", upstream)
	}

	html := string(rewriteEmbedProxyHTML("https://example.com/path/index.html", []byte("<html><head></head></html>"), "secret"))
	if !strings.Contains(html, `<base href="/embed-proxy/secret/`) {
		t.Fatalf("expected path token in rewritten base: %s", html)
	}
}

func TestEmbedProxyAllowsConfiguredDevServerOrigin(t *testing.T) {
	r := &Runtime{cfg: config.Config{
		PlatformBridgeToken: "secret",
		PlatformDevServer:   "http://127.0.0.1:5173",
	}}
	req := httptest.NewRequest("GET", "/embed-proxy/"+encodeEmbedProxyURL("https://example.com/"), nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	if !r.validPlatformHTTPToken(req) {
		t.Fatalf("configured dev server origin was rejected")
	}
}

func TestEmbedProxyAllowsConfiguredDevServerReferer(t *testing.T) {
	r := &Runtime{cfg: config.Config{
		PlatformBridgeToken: "secret",
		PlatformDevServer:   "http://127.0.0.1:5173",
	}}
	req := httptest.NewRequest("GET", "/embed-proxy/"+encodeEmbedProxyURL("https://example.com/"), nil)
	req.Header.Set("Referer", "http://127.0.0.1:5173/src/main.ts")
	if !r.validPlatformHTTPToken(req) {
		t.Fatalf("configured dev server referer was rejected")
	}
}
