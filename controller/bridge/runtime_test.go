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
