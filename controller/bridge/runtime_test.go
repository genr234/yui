package bridge

import (
	"net/http/httptest"
	"testing"

	"kiosk/controller/internal/config"
)

func TestAuthorizeUpgradeRequiresRuntimeToken(t *testing.T) {
	r := New(config.Config{PlatformBridgeToken: "secret"})

	if r.authorizeUpgrade(httptest.NewRequest("GET", "/ws", nil)) {
		t.Fatal("expected missing token to fail")
	}
	if !r.authorizeUpgrade(httptest.NewRequest("GET", "/ws?token=secret", nil)) {
		t.Fatal("expected matching token to pass")
	}
}

func TestAuthorizeUpgradeAllowsConfiguredDevServerOrigin(t *testing.T) {
	r := New(config.Config{
		PlatformBridgeToken: "secret",
		PlatformDevServer:   "http://127.0.0.1:5173",
	})

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	if !r.authorizeUpgrade(req) {
		t.Fatal("expected configured dev server origin to pass")
	}
}

func TestAuthorizeUpgradeRejectsOtherDevOrigins(t *testing.T) {
	r := New(config.Config{
		PlatformBridgeToken: "secret",
		PlatformDevServer:   "http://127.0.0.1:5173",
	})

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	if r.authorizeUpgrade(req) {
		t.Fatal("expected unconfigured origin to fail")
	}
}
