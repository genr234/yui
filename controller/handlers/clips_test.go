package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"kiosk/controller/internal/config"
)

func TestFitRectClampsToTheScreen(t *testing.T) {
	r := fitRect(-0.1, 0.2, 0.6, 0.9)
	if r.X != 0 || r.Y != 0.2 || r.W < 0.499 || r.W > 0.501 || r.H < 0.799 || r.H > 0.801 {
		t.Fatalf("%+v", r)
	}
	if r := fitRect(0.99, 0, 0.5, 1); r.W != 1 || r.X != 0 {
		t.Fatalf("a sliver should fall back to the whole screen: %+v", r)
	}
}

func TestCameraHandoffFromOverlay(t *testing.T) {
	r := NewRegistry(config.Config{})
	events := make(chan map[string]any, 1)
	r.SetEmitter(func(event string, data any) {
		if event == "clips.camera.request" {
			events <- data.(map[string]any)
		}
	})
	from := time.UnixMilli(1_700_000_000_000)
	to := from.Add(5 * time.Second)

	if _, err := r.requestCamera(context.Background(), from, to); err == nil {
		t.Fatal("asked an overlay whose camera is off")
	}
	if _, err := r.Dispatch("clips.camera.status", json.RawMessage(`{"active":true,"width":640,"height":480}`)); err != nil {
		t.Fatal(err)
	}
	if !r.cameraLive() {
		t.Fatal("camera should be live")
	}

	go func() {
		ev := <-events
		if ev["from_ms"] != from.UnixMilli() || ev["to_ms"] != to.UnixMilli() {
			t.Errorf("request %+v", ev)
		}
		body, _ := json.Marshal(map[string]any{
			"req_id": ev["req_id"],
			"segments": []map[string]any{{
				"start_ms": from.UnixMilli() - 500, "end_ms": to.UnixMilli(), "mime": "video/webm",
				"data": base64.StdEncoding.EncodeToString([]byte("webm!")),
			}},
			"crop": map[string]float64{"x": 0.1, "y": 0.2, "w": 0.5, "h": 0.6},
		})
		if _, err := r.Dispatch("clips.camera.deliver", body); err != nil {
			t.Error(err)
		}
	}()
	cam, err := r.requestCamera(context.Background(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(cam.Segments) != 1 || string(cam.Segments[0].Data) != "webm!" || cam.Crop.W != 0.5 || cam.Crop.Y != 0.2 {
		t.Fatalf("%+v", cam)
	}
	if !cam.Segments[0].Start.Equal(from.Add(-500 * time.Millisecond)) {
		t.Fatalf("start %v", cam.Segments[0].Start)
	}
}

func TestClipCameraCommandsArePublic(t *testing.T) {
	for _, m := range []string{"clips.camera.status", "clips.camera.deliver"} {
		if !publicAuthMethod(m) {
			t.Errorf("%s needs the admin PIN", m)
		}
	}
}
