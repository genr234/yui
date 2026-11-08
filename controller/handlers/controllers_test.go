package handlers

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/native"
	"kiosk/controller/internal/pad/apps"
	"kiosk/controller/internal/pad/hub"
)

func newControllersRegistry(t *testing.T) *Registry {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		ConfigPath: filepath.Join(dir, "controller.json"),
		ConfigDir:  dir,
		StorePath:  filepath.Join(dir, "store.db"),
	}
	r := NewRegistry(cfg)
	r.padDSUAddrs = [2]string{"127.0.0.1:0", "127.0.0.1:0"}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.BindPadLifecycle(ctx)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func dispatch(t *testing.T, r *Registry, method, params string) (any, error) {
	t.Helper()
	return r.Dispatch(method, json.RawMessage(params))
}

func TestPadAppFocusLayoutAndKeymap(t *testing.T) {
	r := newControllersRegistry(t)

	// Blurring before anything exists does not build the hub.
	if _, err := dispatch(t, r, "pad.app.focus", `{"active":false}`); err != nil {
		t.Fatal(err)
	}
	if r.padControllersIfStarted() != nil {
		t.Fatal("blur built the hub")
	}

	// A manifest keymap is validated by the controller too.
	_, err := dispatch(t, r, "pad.app.focus", `{"app_id":"dev.genr.subway-surfers","active":true,"keymap":{"controls":[{"type":"dpad","keys":{"up":"F13"}}]}}`)
	if err == nil || !strings.Contains(err.Error(), "invalid keymap") {
		t.Fatalf("bad keymap: %v", err)
	}

	res, err := dispatch(t, r, "pad.app.focus", `{"app_id":"dev.genr.subway-surfers","name":"Subway Surfers","active":true,
		"keymap":{"controls":[{"type":"swipe","keys":{"up":"ArrowUp","down":"ArrowDown","left":"ArrowLeft","right":"ArrowRight"}}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	if m := res.(map[string]any); m["active"] != true || m["app_id"] != "dev.genr.subway-surfers" {
		t.Fatalf("focus result %v", m)
	}
	if layer, _ := r.padRouter.Stack().Top(); layer != hub.LayerApp {
		t.Fatalf("top = %v", layer)
	}

	// Layouts for the focused app.
	if _, err := dispatch(t, r, "pad.app.layout", `{"app_id":"dev.genr.subway-surfers","layouts":{"*":{"type":"button","props":{"id":"jump","label":"Jump"}}}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch(t, r, "pad.app.layout", `{"app_id":"dev.genr.subway-surfers","layouts":{"*":{"props":{}}}}`); err != nil {
		t.Fatalf("an untyped entry is dropped, not an error: %v", err)
	}
	if _, err := dispatch(t, r, "pad.app.layout", `{"app_id":"dev.genr.subway-surfers","layouts":{"*":{"type":"column","children":[{"props":{}}]}}}`); err == nil {
		t.Fatal("untyped child accepted")
	}

	// Haptics and sounds need ownership and a session.
	if _, err := dispatch(t, r, "pad.app.haptic", `{"app_id":"dev.other.app","player_id":"p1","pattern":[20]}`); err == nil {
		t.Fatal("haptic from a background app")
	}
	if _, err := dispatch(t, r, "pad.app.haptic", `{"app_id":"dev.genr.subway-surfers","player_id":"p1","pattern":[]}`); err == nil {
		t.Fatal("empty pattern accepted")
	}
	if _, err := dispatch(t, r, "pad.app.sound", `{"app_id":"dev.genr.subway-surfers","player_id":"p1","url":"http://example.com/a.mp3"}`); err == nil {
		t.Fatal("http sound accepted")
	}
	if _, err := dispatch(t, r, "pad.app.haptic", `{"app_id":"dev.genr.subway-surfers","player_id":"p1","pattern":[20]}`); err == nil || !strings.Contains(err.Error(), "no play session") {
		t.Fatalf("haptic without a session: %v", err)
	}

	if _, err := dispatch(t, r, "pad.app.focus", `{"app_id":"dev.genr.subway-surfers","active":false}`); err != nil {
		t.Fatal(err)
	}
	if layer, _ := r.padRouter.Stack().Top(); layer != hub.LayerShell {
		t.Fatalf("top after blur = %v", layer)
	}
}

func TestPadVirtualPadsAndNativeLayer(t *testing.T) {
	r := newControllersRegistry(t)
	// Attachments and templates name the configured DSU addresses (an
	// ephemeral port here).

	res, err := dispatch(t, r, "pad.vpads.attach", `{"app_id":"dev.example.arcade","player_id":"p1","backend":"dsu","slot":2}`)
	if err != nil {
		t.Fatal(err)
	}
	if a := res.(apps.Attachment); a.Address != "127.0.0.1:0/2" || a.Slot != 2 {
		t.Fatalf("attach %+v", a)
	}
	if _, err := dispatch(t, r, "pad.vpads.attach", `{"app_id":"dev.example.arcade","player_id":"p1"}`); err == nil {
		t.Fatal("attach without a slot")
	}
	res, err = dispatch(t, r, "pad.vpads.system_pad", `{}`)
	if err != nil || res.(map[string]any)["address"] != "127.0.0.1:0/0" {
		t.Fatalf("system pad %v %v", res, err)
	}
	// Pressing system pad buttons needs the controllers.
	if _, err := dispatch(t, r, "pad.vpads.press", `{"app_id":"dev.example.arcade","button":"Start","action":"tap"}`); err == nil {
		t.Fatal("press without owning the controllers")
	}

	// {{virtualPad:n}} and {{systemPad}} resolve through the registry.
	pads := r.dsuResolver()
	if got, err := pads.VirtualPad("dev.example.arcade", 1); err != nil || got != "127.0.0.1:0/1" {
		t.Fatalf("resolver %q %v", got, err)
	}

	// A native session gets the native layer while it runs.
	info := native.Info{ID: "s1", AppID: "dev.example.arcade", VirtualPads: []int{0}}
	obs := nativeHubObserver{r: r}
	obs.SessionStarted(info)
	if layer, _ := r.padRouter.Stack().Top(); layer != hub.LayerNative {
		t.Fatalf("top = %v", layer)
	}
	if _, err := dispatch(t, r, "pad.vpads.press", `{"app_id":"dev.example.arcade","button":"Start","action":"tap"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatch(t, r, "pad.vpads.press", `{"app_id":"dev.example.arcade","button":"Home","action":"tap"}`); err == nil {
		t.Fatal("Home on the system pad")
	}
	res, err = dispatch(t, r, "pad.vpads.list", `{"app_id":"dev.example.arcade"}`)
	if err != nil || len(res.([]apps.Attachment)) != 1 {
		t.Fatalf("list %v %v", res, err)
	}
	obs.SessionEnded(info, native.Status{State: native.StateExited})
	if layer, _ := r.padRouter.Stack().Top(); layer != hub.LayerShell {
		t.Fatalf("top after end = %v", layer)
	}

	// No host is connected (no play session): the watchdog is wired to it.
	if r.padHasHost() {
		t.Fatal("host without a session")
	}
}
