package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestPageOrigin(t *testing.T) {
	cases := map[string]string{
		"https://kiosk.example.com/?a=1#x": "https://kiosk.example.com",
		"http://localhost:5173/app":        "http://localhost:5173",
		"about:blank":                      "",
		"chrome://newtab/":                 "",
		"file:///tmp/x.html":               "",
		"":                                 "",
	}
	for in, want := range cases {
		if got := pageOrigin(in); got != want {
			t.Errorf("pageOrigin(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGrantVideoCaptureOnlyAsksForVideo(t *testing.T) {
	var got cdpMessage
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"webSocketDebuggerUrl": "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/browser/x"})
	})
	mux.HandleFunc("/devtools/browser/x", func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if conn.ReadJSON(&got) != nil {
			return
		}
		_ = conn.WriteJSON(map[string]any{"id": got.ID, "result": map[string]any{}})
	})

	port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
	if err := grantVideoCapture(context.Background(), port, "https://kiosk.example.com"); err != nil {
		t.Fatal(err)
	}
	if got.Method != "Browser.grantPermissions" || got.Params["origin"] != "https://kiosk.example.com" {
		t.Fatalf("unexpected call: %+v", got)
	}
	perms, _ := got.Params["permissions"].([]any)
	if len(perms) != 1 || perms[0] != "videoCapture" {
		t.Fatalf("permissions = %v, want only videoCapture", perms)
	}
}
