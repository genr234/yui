package bridge

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

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

func TestBroadcastReachesConnectedSocket(t *testing.T) {
	r := New(config.Config{})
	server := httptest.NewServer(r.wsHandler())
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	waitFor(t, func() bool { return r.connCount() == 1 }, "connection was never registered")

	// Handlers reach Broadcast through the registry's emitter.
	r.commands.Emit("assets.changed", map[string]any{"kind": "theme"})

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var frame struct {
		ID    *string        `json:"id"`
		Event string         `json:"event"`
		Data  map[string]any `json:"data"`
	}
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatalf("read: %v", err)
	}
	if frame.ID != nil || frame.Event != "assets.changed" || frame.Data["kind"] != "theme" {
		t.Fatalf("unexpected frame: %+v", frame)
	}

	_ = conn.Close()
	waitFor(t, func() bool { return r.connCount() == 0 }, "connection was not removed after close")
	r.Broadcast("noop", nil) // no connections: must not panic or block
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
