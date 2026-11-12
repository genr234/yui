package handlers

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"kiosk/controller/internal/native"
	"kiosk/controller/internal/pad/apps"
	"kiosk/controller/internal/video"
)

func TestMapSurfaceError(t *testing.T) {
	cases := map[error]string{
		video.ErrNotReady:       "YUI_SURFACES: not_ready",
		video.ErrNoVideo:        "YUI_SURFACES: no_video",
		video.ErrNoVideoLink:    "YUI_SURFACES: no_video_link",
		video.ErrUnknownSurface: "YUI_SURFACES: unknown_surface",
	}
	for in, want := range cases {
		if got := mapSurfaceError(in); got == nil || got.Error() != want {
			t.Errorf("mapSurfaceError(%v) = %v, want %q", in, got, want)
		}
	}
	if mapSurfaceError(nil) != nil {
		t.Error("nil must stay nil")
	}
	other := errors.New("boom")
	if got := mapSurfaceError(other); got != other {
		t.Errorf("other errors pass through, got %v", got)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"8.0", "7.1", 1},
		{"8.10", "8.9", 1},
		{"8.0", "8.0", 0},
		{"8", "8.0", 0},
		{"2.6", "2.6.1", -1},
		{"", "1", -1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSurfaceSpecsConversion(t *testing.T) {
	got := surfaceSpecs([]native.SurfaceSpec{{ID: "a", Title: "t", Hidden: true, Width: 854, Height: 480}})
	want := video.SurfaceSpec{ID: "a", Title: "t", Hidden: true, Width: 854, Height: 480}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("surfaceSpecs = %+v", got)
	}
}

func TestSurfacesAvailableAndListWithoutSession(t *testing.T) {
	r := newControllersRegistry(t)
	res, err := dispatch(t, r, "pad.surfaces.list", `{"app_id":"dev.example.arcade"}`)
	if err != nil {
		t.Fatal(err)
	}
	if list, ok := res.([]video.Status); !ok || list == nil || len(list) != 0 {
		t.Fatalf("list = %#v, want an empty non-nil slice", res)
	}
	// Reading never built the video service.
	if r.videoIfStarted() != nil {
		t.Fatal("list built the video service")
	}

	res, err = dispatch(t, r, "pad.surfaces.available", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	av, ok := res.(video.Availability)
	if !ok {
		t.Fatalf("available result is %T", res)
	}
	if av.Available && av.Reason != "" {
		t.Fatalf("available with a reason: %+v", av)
	}
	if !av.Available && av.Reason == "" {
		t.Fatalf("unavailable without a reason: %+v", av)
	}

	res, err = dispatch(t, r, "pad.surfaces.list", `{"app_id":"dev.example.arcade"}`)
	if err != nil {
		t.Fatal(err)
	}
	if list, ok := res.([]video.Status); !ok || list == nil || len(list) != 0 {
		t.Fatalf("list = %#v, want an empty non-nil slice", res)
	}
}

func TestSurfacesShowChecksOwnershipAndErrors(t *testing.T) {
	r := newControllersRegistry(t)
	const app = "dev.example.arcade"

	// Not the owner of the controllers.
	if _, err := dispatch(t, r, "pad.surfaces.show", `{"app_id":"`+app+`","surface":"screen","player_id":null}`); !errors.Is(err, apps.ErrNotOwner) {
		t.Fatalf("show without ownership = %v, want ErrNotOwner", err)
	}

	// A native session gives the app the controllers.
	nativeHubObserver{r: r}.SessionStarted(native.Info{ID: "s1", AppID: app, VirtualPads: []int{0}})

	// Another app still may not.
	if _, err := dispatch(t, r, "pad.surfaces.show", `{"app_id":"dev.other.app","surface":"screen","player_id":null}`); !errors.Is(err, apps.ErrNotOwner) {
		t.Fatalf("show by another app = %v", err)
	}
	// A player who is not in the roster.
	if _, err := dispatch(t, r, "pad.surfaces.show", `{"app_id":"`+app+`","surface":"screen","player_id":"ghost"}`); err == nil || err.Error() != "YUI_SURFACES: unknown_player" {
		t.Fatalf("unknown player = %v", err)
	}
	// A surface the session never declared.
	if _, err := dispatch(t, r, "pad.surfaces.show", `{"app_id":"`+app+`","surface":"screen","player_id":null}`); err == nil || err.Error() != "YUI_SURFACES: unknown_surface" {
		t.Fatalf("unknown surface = %v", err)
	}
	if _, err := dispatch(t, r, "pad.surfaces.show", `{"app_id":"`+app+`","surface":"","player_id":null}`); err == nil || err.Error() != "YUI_SURFACES: unknown_surface" {
		t.Fatalf("empty surface = %v", err)
	}
	if _, err := dispatch(t, r, "pad.surfaces.show", `{"app_id":`); err == nil {
		t.Fatal("malformed params accepted")
	}
}

func TestSurfacesEventPayload(t *testing.T) {
	r := newControllersRegistry(t)
	var mu sync.Mutex
	var events []map[string]any
	r.SetEmitter(func(event string, data any) {
		if event != "pad.surfaces" {
			return
		}
		raw, err := json.Marshal(data)
		if err != nil {
			t.Error(err)
			return
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		events = append(events, m)
		mu.Unlock()
	})

	obs := nativeSurfaceObserver{r: r}
	info := native.Info{ID: "s1", AppID: "dev.example.arcade", PID: 1234, Surfaces: []native.SurfaceSpec{
		{ID: "second-screen", Title: "(?i)view", Hidden: true, Width: 854, Height: 480},
	}}
	obs.SessionStarted(info)

	deadline := time.Now().Add(3 * time.Second)
	var first map[string]any
	for time.Now().Before(deadline) {
		mu.Lock()
		if len(events) > 0 {
			first = events[0]
		}
		mu.Unlock()
		if first != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if first == nil {
		t.Fatal("no pad.surfaces event after SessionStarted")
	}
	if first["app_id"] != "dev.example.arcade" {
		t.Fatalf("event = %v", first)
	}
	list, _ := first["surfaces"].([]any)
	if len(list) != 1 {
		t.Fatalf("event surfaces = %v", first["surfaces"])
	}
	s, _ := list[0].(map[string]any)
	if s["id"] != "second-screen" || s["state"] == nil || !strings.Contains("waiting ready streaming failed closed", s["state"].(string)) {
		t.Fatalf("surface = %v", s)
	}
	if v, present := s["player_id"]; !present || v != nil {
		t.Fatalf("player_id = %v (present %v), want JSON null", v, present)
	}
	for _, key := range []string{"width", "height", "fps"} {
		if _, ok := s[key]; !ok {
			t.Fatalf("surface has no %q: %v", key, s)
		}
	}

	// The app can list what the event showed.
	res, err := dispatch(t, r, "pad.surfaces.list", `{"app_id":"dev.example.arcade"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.([]video.Status); len(got) != 1 || got[0].ID != "second-screen" {
		t.Fatalf("list = %+v", got)
	}

	// The encoder asset counts as in use only while the session runs.
	r.videoMu.Lock()
	r.video.helperAsset = "ff1"
	r.videoMu.Unlock()
	if !r.videoAssetInUse("ff1") || r.videoAssetInUse("other") {
		t.Fatal("videoAssetInUse while the session runs")
	}
	obs.SessionEnded(info, native.Status{State: native.StateExited})
	if r.videoAssetInUse("ff1") {
		t.Fatal("encoder asset still in use after the session ended")
	}
	if got := r.Video().List("dev.example.arcade"); len(got) != 0 {
		t.Fatalf("surfaces left after the session: %+v", got)
	}
}

func TestSessionWithoutSurfacesLeavesVideoAlone(t *testing.T) {
	r := newControllersRegistry(t)
	obs := nativeSurfaceObserver{r: r}
	info := native.Info{ID: "s1", AppID: "dev.example.arcade"}
	obs.SessionStarted(info)
	obs.SessionEnded(info, native.Status{})
	if r.videoIfStarted() != nil {
		t.Fatal("a session without surfaces built the video service")
	}
}

func TestNativeLaunchWithSurfacesNeedsVideo(t *testing.T) {
	r := newControllersRegistry(t)
	if r.Video().Available().Available {
		t.Skip("this machine can stream surfaces")
	}
	_, err := dispatch(t, r, "native.launch", `{"app_id":"dev.example.arcade","runtime":"rt1","executable":"x.exe","surfaces":[{"id":"a","title":"t","hidden":true}]}`)
	if err == nil || !strings.Contains(err.Error(), "surfaces are not available") {
		t.Fatalf("launch with surfaces = %v", err)
	}
}

func TestVirtualPadAttachCarriesTouch(t *testing.T) {
	r := newControllersRegistry(t)
	res, err := dispatch(t, r, "pad.vpads.attach", `{"app_id":"dev.example.arcade","player_id":"p1","backend":"dsu","slot":0,"touch":"surface"}`)
	if err != nil {
		t.Fatal(err)
	}
	if a := res.(apps.Attachment); a.Touch != "surface" {
		t.Fatalf("attachment = %+v", a)
	}
	if _, err := dispatch(t, r, "pad.vpads.attach", `{"app_id":"dev.example.arcade","player_id":"p2","backend":"dsu","slot":1,"touch":"stylus"}`); !errors.Is(err, apps.ErrBadTouch) {
		t.Fatalf("bad touch = %v, want ErrBadTouch", err)
	}
}

func TestNativeSurfaceHiddenDefaultsToTrue(t *testing.T) {
	var p nativeLaunchParams
	if err := json.Unmarshal([]byte(`{"surfaces":[{"id":"a","title":"t"},{"id":"b","title":"t","hidden":false}]}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Surfaces[0].Hidden != nil || p.Surfaces[1].Hidden == nil || *p.Surfaces[1].Hidden {
		t.Fatalf("params = %+v", p.Surfaces)
	}
}
