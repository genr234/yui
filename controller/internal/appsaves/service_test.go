package appsaves

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testApp   = "dev.demo.game"
	testToken = "kiosk-token"
)

type fakeSave struct {
	data    []byte
	updated time.Time
}

// fakeRails implements the §6 kiosk endpoints in memory.
type fakeRails struct {
	mu     sync.Mutex
	saves  map[string]fakeSave // player|key
	srv    *httptest.Server
	puts   int
	lastUA string
}

func newFakeRails(t *testing.T) *fakeRails {
	t.Helper()
	f := &fakeRails{saves: map[string]fakeSave{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func writeErr(w http.ResponseWriter, status int, code string, extra map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"error": code}
	for k, v := range extra {
		body[k] = v
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeRails) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/kiosk/app_saves/")
	parts := strings.SplitN(rest, "/", 2)
	if parts[0] != testApp {
		writeErr(w, http.StatusUnprocessableEntity, "invalid_app_id", nil)
		return
	}
	player := r.URL.Query().Get("player_id")
	switch player {
	case "guest":
		writeErr(w, http.StatusConflict, "not_linked", nil)
		return
	case "nogrant":
		writeErr(w, http.StatusForbidden, "not_granted", nil)
		return
	case "absent":
		writeErr(w, http.StatusNotFound, "not_found", nil)
		return
	}
	if len(parts) == 1 {
		var list []map[string]any
		for k, v := range f.saves {
			if !strings.HasPrefix(k, player+"|") {
				continue
			}
			sum := sha256.Sum256(v.data)
			list = append(list, map[string]any{
				"key": strings.TrimPrefix(k, player+"|"), "size": len(v.data),
				"sha256": hex.EncodeToString(sum[:]), "updated_at": v.updated.Format(TimeFormat),
			})
		}
		writeJSON(w, 200, map[string]any{"saves": list})
		return
	}
	key := parts[1]
	slot := player + "|" + key
	switch r.Method {
	case http.MethodGet:
		v, ok := f.saves[slot]
		if !ok {
			writeErr(w, http.StatusNotFound, "not_found", nil)
			return
		}
		sum := sha256.Sum256(v.data)
		w.Header().Set("X-Save-Sha256", hex.EncodeToString(sum[:]))
		w.Header().Set("X-Updated-At", v.updated.Format(TimeFormat))
		_, _ = w.Write(v.data)
	case http.MethodPut:
		if !keyPattern.MatchString(key) {
			writeErr(w, http.StatusUnprocessableEntity, "invalid_key", nil)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, MaxBlobBytes+2))
		if len(body) > MaxBlobBytes {
			writeErr(w, http.StatusRequestEntityTooLarge, "too_large", nil)
			return
		}
		updated := time.Now().UTC()
		if h := r.Header.Get("X-Updated-At"); h != "" {
			t, err := time.Parse(time.RFC3339Nano, h)
			if err != nil {
				writeErr(w, http.StatusUnprocessableEntity, "invalid_updated_at", nil)
				return
			}
			if t.Before(updated) {
				updated = t
			}
		}
		if old, ok := f.saves[slot]; ok && old.updated.After(updated) {
			writeErr(w, http.StatusConflict, "newer_save_exists", map[string]any{"updated_at": old.updated.Format(TimeFormat)})
			return
		} else if !ok && len(f.saves) >= MaxKeys {
			writeErr(w, http.StatusConflict, "too_many", nil)
			return
		}
		f.saves[slot] = fakeSave{data: body, updated: updated}
		f.puts++
		sum := sha256.Sum256(body)
		writeJSON(w, 200, map[string]any{"key": key, "size": len(body), "sha256": hex.EncodeToString(sum[:]), "updated_at": updated.Format(TimeFormat)})
	case http.MethodDelete:
		delete(f.saves, slot)
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type env struct {
	t      *testing.T
	rails  *fakeRails
	svc    *Service
	root   string
	down   bool
	acct   string
	now    time.Time
	dead   string
	remote *HTTPRemote
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, rails: newFakeRails(t), root: t.TempDir(), acct: "acc1", now: time.Now().UTC().Add(-time.Minute)}
	dead := httptest.NewServer(http.NotFoundHandler())
	e.dead = dead.URL
	dead.Close()
	e.remote = &HTTPRemote{
		Endpoint: func() (string, string, bool) {
			if e.acct == "" {
				return "", "", false
			}
			if e.down {
				return e.dead, testToken, true
			}
			return e.rails.srv.URL, testToken, true
		},
		Client: &http.Client{Timeout: 5 * time.Second},
	}
	e.svc = &Service{Root: e.root, Remote: e.remote, Account: func() string { return e.acct }, Now: func() time.Time { return e.now }}
	return e
}

var ctx = context.Background()

func kiosk() Scope { return Scope{AppID: testApp} }
func player(id, profile string) Scope {
	return Scope{AppID: testApp, PlayerID: id, ProfileID: profile}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("err = %v, want code %s", err, code)
	}
	if !strings.HasPrefix(err.Error(), code+": ") {
		t.Fatalf("message %q must start with %q", err.Error(), code+": ")
	}
}

func TestKioskRoundTripLargeBlob(t *testing.T) {
	e := newEnv(t)
	blob := bytes.Repeat([]byte("0123456789abcdef"), 6<<16) // 6 MiB
	res, err := e.svc.Put(ctx, kiosk(), "slot-1.state", blob)
	if err != nil || res.Queued || res.Size != int64(len(blob)) || res.UpdatedAt == "" {
		t.Fatalf("put: %+v %v", res, err)
	}
	got, err := e.svc.Get(ctx, kiosk(), "slot-1.state")
	if err != nil || got == nil || !bytes.Equal(got.Data, blob) {
		t.Fatalf("get: %v", err)
	}
	list, err := e.svc.List(ctx, kiosk())
	if err != nil || len(list) != 1 || list[0].Key != "slot-1.state" || list[0].Size != int64(len(blob)) {
		t.Fatalf("list: %+v %v", list, err)
	}
	// The local copy lives where the contract says.
	local := filepath.Join(e.root, "acc1", testApp, "kiosk", "slot-1.state")
	if st, err := os.Stat(local); err != nil || st.Size() != int64(len(blob)) {
		t.Fatalf("local copy: %v", err)
	}
	if err := e.svc.Delete(ctx, kiosk(), "slot-1.state"); err != nil {
		t.Fatal(err)
	}
	if got, err := e.svc.Get(ctx, kiosk(), "slot-1.state"); err != nil || got != nil {
		t.Fatalf("after delete: %v %v", got, err)
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Fatal("local copy should be gone")
	}
	if list, _ := e.svc.List(ctx, kiosk()); len(list) != 0 {
		t.Fatalf("list after delete: %+v", list)
	}
}

func TestPlayerAndKioskScopesAreSeparate(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Put(ctx, kiosk(), "save", []byte("kiosk")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Put(ctx, player("p1", "prof-9"), "save", []byte("player")); err != nil {
		t.Fatal(err)
	}
	k, _ := e.svc.Get(ctx, kiosk(), "save")
	p, _ := e.svc.Get(ctx, player("p1", "prof-9"), "save")
	if string(k.Data) != "kiosk" || string(p.Data) != "player" {
		t.Fatalf("scopes mixed: %q %q", k.Data, p.Data)
	}
	if _, err := os.Stat(filepath.Join(e.root, "acc1", testApp, "player-prof-9", "save")); err != nil {
		t.Fatalf("player local copy: %v", err)
	}
}

func TestPlayerScopeErrors(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		player string
		code   string
	}{{"guest", "not_linked"}, {"nogrant", "not_granted"}, {"absent", "not_linked"}}
	for _, c := range cases {
		sc := player(c.player, "prof-1")
		_, err := e.svc.Put(ctx, sc, "k", []byte("x"))
		wantCode(t, err, c.code)
		_, err = e.svc.Get(ctx, sc, "k")
		if c.player != "absent" { // a missing blob and a missing player both answer 404 on GET
			wantCode(t, err, c.code)
		}
		_, err = e.svc.List(ctx, sc)
		wantCode(t, err, c.code)
		err = e.svc.Delete(ctx, sc, "k")
		if c.player != "absent" {
			wantCode(t, err, c.code)
		}
	}
	// Nothing was queued for a refused put.
	if len(e.svc.listLocal("acc1", player("guest", "prof-1"))) != 0 {
		t.Fatal("a refused put must not leave a local copy")
	}
}

func TestLimitsAndKeys(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.Put(ctx, kiosk(), "big", make([]byte, MaxBlobBytes+1))
	wantCode(t, err, "too_large")
	if _, err := e.svc.Put(ctx, kiosk(), "edge", make([]byte, MaxBlobBytes)); err != nil {
		t.Fatalf("exactly 8 MiB must be accepted: %v", err)
	}
	for _, key := range []string{"", "Upper", ".hidden", "-x", "a/b", "a b", strings.Repeat("a", 81), "../x"} {
		_, err := e.svc.Put(ctx, kiosk(), key, []byte("x"))
		wantCode(t, err, "invalid_key")
		_, err = e.svc.Get(ctx, kiosk(), key)
		wantCode(t, err, "invalid_key")
	}
	if _, err := e.svc.Put(ctx, kiosk(), strings.Repeat("a", 80), []byte("x")); err != nil {
		t.Fatalf("80-char key: %v", err)
	}

	// 64 keys per scope (one already used by "edge" and the 80-char key).
	e = newEnv(t)
	for i := 0; i < MaxKeys; i++ {
		if _, err := e.svc.Put(ctx, kiosk(), "k"+string(rune('a'+i/26))+string(rune('a'+i%26)), []byte("x")); err != nil {
			t.Fatalf("key %d: %v", i, err)
		}
	}
	_, err = e.svc.Put(ctx, kiosk(), "one-too-many", []byte("x"))
	wantCode(t, err, "too_many")
	// Replacing an existing key is not a new key.
	if _, err := e.svc.Put(ctx, kiosk(), "kaa", []byte("y")); err != nil {
		t.Fatalf("replace: %v", err)
	}
}

func TestNoAccountIsOffline(t *testing.T) {
	e := newEnv(t)
	e.acct = ""
	_, err := e.svc.Put(ctx, kiosk(), "k", []byte("x"))
	wantCode(t, err, "offline")
	_, err = e.svc.Get(ctx, kiosk(), "k")
	wantCode(t, err, "offline")
	_, err = e.svc.List(ctx, kiosk())
	wantCode(t, err, "offline")
	wantCode(t, e.svc.Delete(ctx, kiosk(), "k"), "offline")
}

func TestOfflineQueueAndFlush(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Put(ctx, kiosk(), "synced", []byte("v1")); err != nil {
		t.Fatal(err)
	}

	e.down = true
	e.now = e.now.Add(time.Second)
	res, err := e.svc.Put(ctx, kiosk(), "queued", []byte("offline data"))
	if err != nil || !res.Queued {
		t.Fatalf("offline put: %+v %v", res, err)
	}
	// Offline overwrite of a synced key.
	if res, err := e.svc.Put(ctx, kiosk(), "synced", []byte("v2")); err != nil || !res.Queued {
		t.Fatalf("offline overwrite: %+v %v", res, err)
	}
	// Reads fall back to the local copy; list merges.
	got, err := e.svc.Get(ctx, kiosk(), "queued")
	if err != nil || got == nil || string(got.Data) != "offline data" {
		t.Fatalf("offline get: %v %v", got, err)
	}
	if got, err := e.svc.Get(ctx, kiosk(), "nothing"); err != nil || got != nil {
		t.Fatalf("offline get of unknown key: %v %v", got, err)
	}
	list, err := e.svc.List(ctx, kiosk())
	if err != nil || len(list) != 2 || list[0].Key != "queued" || list[1].Key != "synced" {
		t.Fatalf("offline list: %+v %v", list, err)
	}
	// Delete needs Rails.
	wantCode(t, e.svc.Delete(ctx, kiosk(), "synced"), "offline")
	// Flush while down changes nothing and reports the outage.
	if n, err := e.svc.Flush(ctx); err == nil || n != 0 {
		t.Fatalf("flush while down: %d %v", n, err)
	}

	e.down = false
	n, err := e.svc.Flush(ctx)
	if err != nil || n != 2 {
		t.Fatalf("flush: %d %v", n, err)
	}
	e.rails.mu.Lock()
	if string(e.rails.saves["|queued"].data) != "offline data" || string(e.rails.saves["|synced"].data) != "v2" {
		t.Fatalf("remote after flush: %+v", e.rails.saves)
	}
	e.rails.mu.Unlock()
	if n, _ := e.svc.Flush(ctx); n != 0 {
		t.Fatalf("second flush sent %d", n)
	}
	for key, info := range e.svc.listLocal("acc1", kiosk()) {
		if info.Queued {
			t.Fatalf("%s still queued", key)
		}
	}
}

func TestFlushDropsQueuedBlobWhenAccountHasNewer(t *testing.T) {
	e := newEnv(t)
	e.down = true
	e.now = e.now.Add(-time.Hour)
	if res, err := e.svc.Put(ctx, kiosk(), "k", []byte("old, written offline")); err != nil || !res.Queued {
		t.Fatalf("%+v %v", res, err)
	}
	// Meanwhile another kiosk saved a newer one.
	e.rails.mu.Lock()
	e.rails.saves["|k"] = fakeSave{data: []byte("newer"), updated: time.Now().UTC().Add(-time.Minute)}
	e.rails.mu.Unlock()
	e.down = false
	if n, err := e.svc.Flush(ctx); err != nil || n != 0 {
		t.Fatalf("flush: %d %v", n, err)
	}
	got, err := e.svc.Get(ctx, kiosk(), "k")
	if err != nil || string(got.Data) != "newer" {
		t.Fatalf("get after conflict: %v %v", got, err)
	}
}

func TestGetPrefersNewerQueuedLocalCopy(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.Put(ctx, kiosk(), "k", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	e.down = true
	e.now = time.Now().UTC().Add(-time.Second)
	if _, err := e.svc.Put(ctx, kiosk(), "k", []byte("v2")); err != nil {
		t.Fatal(err)
	}
	e.down = false // reachable again, but not flushed yet
	got, err := e.svc.Get(ctx, kiosk(), "k")
	if err != nil || string(got.Data) != "v2" {
		t.Fatalf("get: %v %v", got, err)
	}
}

func TestPutWithSkewedClockStillWins(t *testing.T) {
	e := newEnv(t)
	// The account's copy is stamped later than this kiosk's idea of now.
	e.rails.mu.Lock()
	e.rails.saves["|k"] = fakeSave{data: []byte("future"), updated: time.Now().UTC().Add(-time.Second)}
	e.rails.mu.Unlock()
	e.now = time.Now().UTC().Add(-time.Hour)
	if res, err := e.svc.Put(ctx, kiosk(), "k", []byte("mine")); err != nil || res.Queued {
		t.Fatalf("put: %+v %v", res, err)
	}
	e.rails.mu.Lock()
	defer e.rails.mu.Unlock()
	if string(e.rails.saves["|k"].data) != "mine" {
		t.Fatal("the user's latest save must win over a skewed clock")
	}
}

func TestRequestShape(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("X-Updated-At")+" "+r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	h := &HTTPRemote{Endpoint: func() (string, string, bool) { return srv.URL, "t", true }}
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	_, _ = h.Put(ctx, "dev.app", "k.1", "p 1", []byte("x"), at)
	_ = h.Delete(ctx, "dev.app", "k.1", "")
	want := []string{
		"PUT /api/kiosk/app_saves/dev.app/k.1?player_id=p+1 2026-01-02T03:04:05.000Z application/octet-stream",
		"DELETE /api/kiosk/app_saves/dev.app/k.1  ",
	}
	if len(seen) != 2 || seen[0] != want[0] || seen[1] != want[1] {
		t.Fatalf("requests:\n got %q\nwant %q", seen, want)
	}
}

func TestGetRejectsChecksumMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Save-Sha256", strings.Repeat("0", 64))
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()
	h := &HTTPRemote{Endpoint: func() (string, string, bool) { return srv.URL, "t", true }}
	if _, _, _, err := h.Get(ctx, "dev.app", "k", ""); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}
