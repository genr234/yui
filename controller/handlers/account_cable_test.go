package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
)

func TestAccountCableHeaderUsesServerOrigin(t *testing.T) {
	var gotOrigin, gotAuth string
	upgraded := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrigin = r.Header.Get("Origin")
		gotAuth = r.Header.Get("Authorization")
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		close(upgraded)
		_ = conn.Close()
	}))
	defer srv.Close()

	header, err := accountCableHeader(srv.URL, "kiosk-token")
	if err != nil {
		t.Fatal(err)
	}
	if header.Get("Origin") != srv.URL {
		t.Fatalf("origin %q, want %q", header.Get("Origin"), srv.URL)
	}
	if header.Get("Authorization") != "Bearer kiosk-token" {
		t.Fatalf("authorization %q", header.Get("Authorization"))
	}

	wsURL, err := accountCableURL(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
	_ = conn.Close()
	<-upgraded
	if gotOrigin != srv.URL {
		t.Fatalf("dialed origin %q, want %q", gotOrigin, srv.URL)
	}
	if gotAuth != "Bearer kiosk-token" {
		t.Fatalf("dialed authorization %q", gotAuth)
	}
}

func TestAccountCableURLSchemes(t *testing.T) {
	wsURL, err := accountCableURL("https://yui.example/root")
	if err != nil {
		t.Fatal(err)
	}
	if wsURL != "wss://yui.example/root/cable" {
		t.Fatalf("ws url %s", wsURL)
	}
	header, err := accountCableHeader("https://yui.example/root", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if header.Get("Origin") != "https://yui.example" {
		t.Fatalf("origin %s", header.Get("Origin"))
	}
	if _, err := accountCableURL("ftp://yui.example"); err == nil {
		t.Fatal("expected unsupported scheme")
	}
}
