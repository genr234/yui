package assets

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSaveServer mimics the Rails kiosk saves API for one key.
type fakeSaveServer struct {
	mu      sync.Mutex
	blob    []byte
	sha     string
	savedAt string
	puts    int
	conflic bool
	lastHdr http.Header
	lastURL string
}

func (f *fakeSaveServer) handler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		const prefix = "/api/kiosk/saves/some.app/12"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == prefix:
			if f.blob == nil {
				http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
				return
			}
			io.WriteString(w, `{"id":"7","sha256":"`+f.sha+`","byte_size":`+itoa(len(f.blob))+`,"saved_at":"`+f.savedAt+`","metadata":{"kiosk_name":"Front"}}`)
		case r.Method == http.MethodGet && r.URL.Path == prefix+"/download":
			w.Write(f.blob)
		case r.Method == http.MethodPut && r.URL.Path == prefix:
			f.lastHdr = r.Header.Clone()
			f.lastURL = r.URL.RawQuery
			if f.conflic {
				http.Error(w, `{"error":"newer_backup_exists"}`, http.StatusConflict)
				return
			}
			body, _ := io.ReadAll(r.Body)
			f.blob, f.sha, f.savedAt = body, r.Header.Get("X-Save-Sha256"), r.URL.Query().Get("saved_at")
			f.puts++
			io.WriteString(w, "{}")
		default:
			http.NotFound(w, r)
		}
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

func saveRemote(srv *httptest.Server, token string) *HTTPRemote {
	return &HTTPRemote{Endpoint: func() (string, string, bool) { return srv.URL, token, true }}
}

func TestSaveClientUploadInfoDownload(t *testing.T) {
	fake := &fakeSaveServer{}
	srv := httptest.NewServer(fake.handler("tok"))
	defer srv.Close()
	r := saveRemote(srv, "tok")
	ctx := context.Background()

	if _, ok, err := r.SaveInfo(ctx, "some.app", "12"); err != nil || ok {
		t.Fatalf("empty server: ok=%v err=%v", ok, err)
	}

	savedAt := time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)
	data := "PK-zip-bytes"
	if err := r.SaveUpload(ctx, "some.app", "12", strings.NewReader(data), int64(len(data)), "abc123", savedAt); err != nil {
		t.Fatal(err)
	}
	if fake.lastHdr.Get("X-Save-Sha256") != "abc123" || fake.lastHdr.Get("Content-Length") != "12" {
		t.Fatalf("headers = %v", fake.lastHdr)
	}
	if !strings.Contains(fake.lastURL, "saved_at=2026-11-02T09%3A30%3A00Z") {
		t.Fatalf("query = %q", fake.lastURL)
	}

	info, ok, err := r.SaveInfo(ctx, "some.app", "12")
	if err != nil || !ok {
		t.Fatalf("info: ok=%v err=%v", ok, err)
	}
	if info.SHA256 != "abc123" || info.Size != 12 || !info.SavedAt.Equal(savedAt) || info.KioskName != "Front" {
		t.Fatalf("info = %+v", info)
	}

	body, err := r.SaveDownload(ctx, "some.app", "12")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(body)
	body.Close()
	if string(got) != data {
		t.Fatalf("download = %q", got)
	}
}

func TestSaveClientErrors(t *testing.T) {
	fake := &fakeSaveServer{}
	srv := httptest.NewServer(fake.handler("tok"))
	defer srv.Close()
	ctx := context.Background()

	bad := saveRemote(srv, "wrong")
	if _, _, err := bad.SaveInfo(ctx, "some.app", "12"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("unauthorized info: %v", err)
	}
	if _, err := bad.SaveDownload(ctx, "some.app", "12"); err == nil {
		t.Fatal("unauthorized download succeeded")
	}

	fake.conflic = true
	r := saveRemote(srv, "tok")
	err := r.SaveUpload(ctx, "some.app", "12", strings.NewReader("PKx"), 3, "", time.Now())
	if !errors.Is(err, ErrSaveNewer) {
		t.Fatalf("conflict: err = %v", err)
	}

	none := &HTTPRemote{Endpoint: func() (string, string, bool) { return "", "", false }}
	if _, _, err := none.SaveInfo(ctx, "some.app", "12"); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("no account: err = %v", err)
	}
}
