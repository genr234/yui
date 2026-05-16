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
