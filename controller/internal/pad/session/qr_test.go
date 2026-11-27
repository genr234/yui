package session

import "testing"

// A real-looking qr_key for fixtures. The server still sends one (older
// controllers require it), but the QR no longer uses it.
const (
	sharedQRB64 = "cXIta2V5LWJ5dGVzLWZvci1zaGFyZWQtdmVjdG9yISE="
	// A fixed clock for tests (2026-01-01T00:00:00Z).
	sharedQRAt = int64(1767225600)
)

func TestJoinURL(t *testing.T) {
	got, err := JoinURL("https://yui.example/play/KQXM")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://yui.example/play/KQXM" {
		t.Fatalf("join url %s", got)
	}
	if _, err := JoinURL("/play/KQXM"); err == nil {
		t.Fatal("expected relative url error")
	}
}
