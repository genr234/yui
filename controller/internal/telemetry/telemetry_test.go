package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFlushSendsEventsAndSnapshotThenForgetsThem(t *testing.T) {
	mu.Lock()
	events = nil
	mu.Unlock()
	var got struct {
		Events   []Event        `json:"events"`
		Snapshot map[string]any `json:"snapshot"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/kiosk/telemetry" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", 401)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(204)
	}))
	defer srv.Close()

	Record("chrome_exited", map[string]any{"code": 0})
	Set("chrome", "restarting")
	endpoint := func() (string, string, bool) { return srv.URL, "tok", true }
	if err := flush(context.Background(), srv.Client(), endpoint, "1.0", "abc"); err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 1 || got.Events[0].Kind != "chrome_exited" || got.Snapshot["chrome"] != "restarting" || got.Snapshot["commit"] != "abc" {
		t.Fatalf("got %+v", got)
	}
	mu.Lock()
	left := len(events)
	mu.Unlock()
	if left != 0 {
		t.Fatalf("%d events left after a successful upload", left)
	}
}

func TestEventsAreCapped(t *testing.T) {
	for i := 0; i < maxEvents+50; i++ {
		Record("tick", nil)
	}
	mu.Lock()
	n := len(events)
	mu.Unlock()
	if n != maxEvents {
		t.Fatalf("kept %d events", n)
	}
}
