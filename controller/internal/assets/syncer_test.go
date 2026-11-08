package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"kiosk/controller/internal/store"
)

type fakeAsset struct {
	ID, Kind, Title, Filename string
	Data                      []byte
	// ServedSHA overrides the sha advertised in the manifest (to force a mismatch).
	ManifestSHA string
}

func (a fakeAsset) sha() string {
	sum := sha256.Sum256(a.Data)
	return hex.EncodeToString(sum[:])
}

type reportRec struct {
	ID, State string
	Bytes     int64
}

type fakeServer struct {
	t  *testing.T
	ts *httptest.Server

	mu          sync.Mutex
	assets      map[string]fakeAsset
	manifestErr bool
	cutAt       int64 // if >0, the next download body is cut after this many bytes
	ranges      []string
	downloads   int
	reports     []reportRec
	failReports bool
}

func newFakeServer(t *testing.T, assets ...fakeAsset) *fakeServer {
	f := &fakeServer{t: t, assets: map[string]fakeAsset{}}
	for _, a := range assets {
		f.assets[a.ID] = a
	}
	f.ts = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeServer) set(a fakeAsset) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assets[a.ID] = a
}

func (f *fakeServer) remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.assets, id)
}

func (f *fakeServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, "unauthorized", 401)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/api/kiosk/assets" && r.Method == "GET":
		if f.manifestErr {
			http.Error(w, "boom", 500)
			return
		}
		out := []map[string]any{}
		for _, a := range f.assets {
			sha := a.ManifestSHA
			if sha == "" {
				sha = a.sha()
			}
			out = append(out, map[string]any{"id": a.ID, "kind": a.Kind, "title": a.Title, "filename": a.Filename,
				"byte_size": len(a.Data), "sha256": sha, "metadata": map[string]any{"game_id": "X"}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"assets": out})
	case strings.HasSuffix(r.URL.Path, "/download"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/kiosk/assets/"), "/download")
		a, ok := f.assets[id]
		if !ok {
			http.NotFound(w, r)
			return
		}
		f.downloads++
		start := int64(0)
		if rg := r.Header.Get("Range"); rg != "" {
			f.ranges = append(f.ranges, rg)
			s := strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-")
			start, _ = strconv.ParseInt(s, 10, 64)
		}
		total := int64(len(a.Data))
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("X-Asset-Sha256", a.sha())
		body := a.Data[start:]
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, total-1, total))
			w.WriteHeader(206)
		}
		if f.cutAt > 0 && int64(len(body)) > f.cutAt {
			_, _ = w.Write(body[:f.cutAt])
			f.cutAt = 0
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler) // drop the connection mid-body
		}
		_, _ = w.Write(body)
	case strings.HasSuffix(r.URL.Path, "/status") && r.Method == "PATCH":
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/kiosk/assets/"), "/status")
		var p struct {
			State     string `json:"state"`
			BytesDone int64  `json:"bytes_done"`
		}
		_ = json.NewDecoder(r.Body).Decode(&p)
		f.reports = append(f.reports, reportRec{id, p.State, p.BytesDone})
		if f.failReports {
			http.Error(w, "nope", 500)
			return
		}
		w.WriteHeader(204)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeServer) states(id string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var s []string
	for _, r := range f.reports {
		if r.ID == id {
			s = append(s, r.State)
		}
	}
	return s
}

type harness struct {
	s       *Syncer
	srv     *fakeServer
	dir     string
	db      *store.DB
	changes *int
	free    *uint64
	mu      sync.Mutex
}

func newHarness(t *testing.T, srv *fakeServer) *harness {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := &harness{srv: srv, dir: filepath.Join(dir, "assets"), db: db}
	changes := 0
	h.changes = &changes
	free := uint64(100) << 30
	h.free = &free
	remote := &HTTPRemote{Endpoint: func() (string, string, bool) { return srv.ts.URL, "tok", true }}
	h.s = NewSyncer(Config{
		DataDir:    h.dir,
		Remote:     remote,
		Store:      func() (*store.DB, error) { return db, nil },
		DiskFree:   func(string) (uint64, error) { h.mu.Lock(); defer h.mu.Unlock(); return *h.free, nil },
		OnChange:   func() { h.mu.Lock(); *h.changes++; h.mu.Unlock() },
		RetryDelay: time.Millisecond,
	})
	return h
}

func (h *harness) setFree(n uint64) { h.mu.Lock(); *h.free = n; h.mu.Unlock() }

func data(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7) + seed
	}
	return b
}

func (h *harness) stateOf(t *testing.T, id string) Item {
	t.Helper()
	items, err := h.s.List("")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == id {
			return it
		}
	}
	return Item{}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFreshDownloadAndVerify(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "game", Title: "Game", Filename: "game.rvz", Data: data(1<<20+123, 1)}
	srv := newFakeServer(t, a)
	h := newHarness(t, srv)

	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	path, ok := h.s.LocalPath("1")
	if !ok {
		t.Fatal("expected local path")
	}
	want := filepath.Join(h.dir, "game", "1", "game.rvz")
	if path != want {
		t.Fatalf("path %s want %s", path, want)
	}
	if string(readFile(t, path)) != string(a.Data) {
		t.Fatal("content mismatch")
	}
	if _, err := os.Stat(filepath.Join(h.dir, ".partial", "1")); !os.IsNotExist(err) {
		t.Fatal("partial should be gone")
	}
	it := h.stateOf(t, "1")
	if it.State != StateReady || it.BytesDone != int64(len(a.Data)) {
		t.Fatalf("bad item %+v", it)
	}
	states := srv.states("1")
	if states[0] != "queued" || states[len(states)-1] != "ready" {
		t.Fatalf("states %v", states)
	}
	if st := h.s.Status(); st.State != "idle" || st.LastReconcileAt == nil || st.Current != nil {
		t.Fatalf("status %+v", st)
	}
}

func TestInterruptedDownloadResumes(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "m.bin", Data: data(2<<20, 3)}
	srv := newFakeServer(t, a)
	srv.cutAt = 700 << 10
	h := newHarness(t, srv)

	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	path, ok := h.s.LocalPath("1")
	if !ok {
		t.Fatalf("not ready: %+v", h.stateOf(t, "1"))
	}
	if string(readFile(t, path)) != string(a.Data) {
		t.Fatal("content mismatch after resume")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.ranges) == 0 || !strings.HasPrefix(srv.ranges[0], "bytes=") || srv.ranges[0] == "bytes=0-" {
		t.Fatalf("expected Range resume, got %v", srv.ranges)
	}
}

func TestResumeFromExistingPartialAcrossRestart(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "m.bin", Data: data(1<<20, 9)}
	srv := newFakeServer(t, a)
	h := newHarness(t, srv)
	if err := os.MkdirAll(filepath.Join(h.dir, ".partial"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dir, ".partial", "1"), a.Data[:300000], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.s.LocalPath("1"); !ok {
		t.Fatal("not ready")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.ranges) != 1 || srv.ranges[0] != "bytes=300000-" {
		t.Fatalf("ranges %v", srv.ranges)
	}
}

func TestChecksumMismatchFails(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "m.bin", Data: data(5000, 2),
		ManifestSHA: strings.Repeat("a", 64)}
	srv := newFakeServer(t, a)
	h := newHarness(t, srv)
	// Server header sha equals manifest sha only when consistent; here the
	// header (real sha) differs from the manifest, which is also a failure.
	// To exercise the post-download hash check, serve without the header.
	srv.ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/download") {
			w.Header().Set("Content-Length", strconv.Itoa(len(a.Data)))
			_, _ = w.Write(a.Data)
			return
		}
		srv.handle(w, r)
	})
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	it := h.stateOf(t, "1")
	if it.State != StateFailed {
		t.Fatalf("state %s", it.State)
	}
	if _, err := os.Stat(filepath.Join(h.dir, ".partial", "1")); !os.IsNotExist(err) {
		t.Fatal("partial should be deleted")
	}
	if _, ok := h.s.LocalPath("1"); ok {
		t.Fatal("should not be local")
	}
	var l Local
	if _, err := h.db.Collection(Collection).Decode("1", &l); err != nil || !strings.Contains(l.Error, "checksum mismatch") {
		t.Fatalf("error %q", l.Error)
	}
	if got := srv.states("1"); got[len(got)-1] != "failed" {
		t.Fatalf("states %v", got)
	}
}

func TestChangedShaRedownloads(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "m.bin", Data: data(4000, 1)}
	srv := newFakeServer(t, a)
	h := newHarness(t, srv)
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	// Unchanged reconcile downloads nothing.
	srv.mu.Lock()
	before := srv.downloads
	srv.mu.Unlock()
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	if srv.downloads != before {
		t.Fatal("unexpected re-download")
	}
	srv.mu.Unlock()

	b := a
	b.Data = data(4500, 5)
	srv.set(b)
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	path, ok := h.s.LocalPath("1")
	if !ok || string(readFile(t, path)) != string(b.Data) {
		t.Fatal("expected new content")
	}
}

func TestRemovalDeletesFiles(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "m.bin", Data: data(3000, 1)}
	b := fakeAsset{ID: "2", Kind: "media", Title: "N", Filename: "n.bin", Data: data(3000, 2)}
	srv := newFakeServer(t, a, b)
	h := newHarness(t, srv)
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	p1, _ := h.s.LocalPath("1")
	srv.remove("1")
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Fatal("file should be deleted")
	}
	if _, err := os.Stat(filepath.Dir(p1)); !os.IsNotExist(err) {
		t.Fatal("dir should be deleted")
	}
	items, _ := h.s.List("")
	if len(items) != 1 || items[0].ID != "2" {
		t.Fatalf("items %+v", items)
	}
	if _, ok := h.s.LocalPath("2"); !ok {
		t.Fatal("other asset must stay")
	}
}

func TestManifestErrorDeletesNothing(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "m.bin", Data: data(3000, 1)}
	srv := newFakeServer(t, a)
	h := newHarness(t, srv)
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	srv.manifestErr = true
	srv.mu.Unlock()
	if err := h.s.Reconcile(context.Background(), true); err == nil {
		t.Fatal("expected error")
	}
	if _, ok := h.s.LocalPath("1"); !ok {
		t.Fatal("asset must survive a manifest failure")
	}
	if st := h.s.Status(); st.State != "error" || st.Error == nil {
		t.Fatalf("status %+v", st)
	}
}

func TestNoAccountDeletesNothing(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "m.bin", Data: data(3000, 1)}
	srv := newFakeServer(t, a)
	h := newHarness(t, srv)
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	h.s.cfg.Remote = &HTTPRemote{Endpoint: func() (string, string, bool) { return "", "", false }}
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatalf("idle should not error: %v", err)
	}
	if _, ok := h.s.LocalPath("1"); !ok {
		t.Fatal("asset must survive with no account")
	}
	if st := h.s.Status(); st.State != "idle" {
		t.Fatalf("status %+v", st)
	}
	if !errors.Is(ErrNoAccount, ErrNoAccount) {
		t.Fatal()
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPauseResume(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "m.bin", Data: data(1<<20, 1)}
	srv := newFakeServer(t, a)
	h := newHarness(t, srv)
	h.s.SetPaused("play", true)
	h.s.SetPaused("other", true)

	done := make(chan error, 1)
	go func() { done <- h.s.Reconcile(context.Background(), true) }()

	waitFor(t, "paused state", func() bool { return h.stateOf(t, "1").State == StatePaused })
	if st := h.s.Status(); st.State != "paused" || len(st.PausedReasons) != 2 {
		t.Fatalf("status %+v", st)
	}
	srv.mu.Lock()
	if srv.downloads != 0 {
		t.Fatal("must not download while paused")
	}
	srv.mu.Unlock()

	h.s.SetPaused("play", false)
	time.Sleep(50 * time.Millisecond)
	if _, ok := h.s.LocalPath("1"); ok {
		t.Fatal("still one reason left; must stay paused")
	}
	h.s.SetPaused("other", false)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, ok := h.s.LocalPath("1"); !ok {
		t.Fatal("expected ready after resume")
	}
	if got := srv.states("1"); !contains(got, "paused") || got[len(got)-1] != "ready" {
		t.Fatalf("states %v", got)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestInsufficientDisk(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "m.bin", Data: data(1<<20, 1)}
	srv := newFakeServer(t, a)
	h := newHarness(t, srv)
	h.setFree(DefaultReserve + 1000) // less than reserve + size
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	var l Local
	if _, err := h.db.Collection(Collection).Decode("1", &l); err != nil {
		t.Fatal(err)
	}
	if l.State != StateFailed || !strings.Contains(l.Error, "not enough disk space") {
		t.Fatalf("record %+v", l)
	}
	srv.mu.Lock()
	if srv.downloads != 0 {
		t.Fatal("must not start downloading")
	}
	srv.mu.Unlock()

	// Within backoff a non-forced reconcile skips it; a forced one retries.
	h.setFree(DefaultReserve + 10<<20)
	if err := h.s.Reconcile(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.s.LocalPath("1"); ok {
		t.Fatal("backoff should have skipped retry")
	}
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.s.LocalPath("1"); !ok {
		t.Fatal("forced reconcile should retry and succeed")
	}
}

func TestListNeverExposesPaths(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "game", Title: "G", Filename: "secret-name.rvz", Data: data(2000, 1)}
	b := fakeAsset{ID: "2", Kind: "media", Title: "M", Filename: "m.bin", Data: data(2000, 2)}
	srv := newFakeServer(t, a, b)
	h := newHarness(t, srv)
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	items, err := h.s.List("game")
	if err != nil || len(items) != 1 || items[0].ID != "1" {
		t.Fatalf("items %+v err %v", items, err)
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "path") || strings.Contains(string(raw), "secret-name") || strings.Contains(string(raw), h.dir) {
		t.Fatalf("leaked path: %s", raw)
	}
}

func TestFailedReportsDoNotStopDownload(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "m.bin", Data: data(5000, 1)}
	srv := newFakeServer(t, a)
	srv.failReports = true
	h := newHarness(t, srv)
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.s.LocalPath("1"); !ok {
		t.Fatal("download must complete despite failing reports")
	}
}

func TestUnsafeManifestItemsSkipped(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "../evil", Data: data(100, 1)}
	srv := newFakeServer(t, a)
	h := newHarness(t, srv)
	if err := h.s.Reconcile(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if items, _ := h.s.List(""); len(items) != 0 {
		t.Fatalf("unsafe item accepted: %+v", items)
	}
}

func TestRunAndTrigger(t *testing.T) {
	a := fakeAsset{ID: "1", Kind: "media", Title: "M", Filename: "m.bin", Data: data(3000, 1)}
	srv := newFakeServer(t, a)
	h := newHarness(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.s.Run(ctx); close(done) }()
	waitFor(t, "initial reconcile", func() bool { _, ok := h.s.LocalPath("1"); return ok })

	srv.set(fakeAsset{ID: "2", Kind: "media", Title: "N", Filename: "n.bin", Data: data(3000, 4)})
	h.s.TriggerReconcile()
	h.s.TriggerReconcile() // coalesced, non-blocking
	waitFor(t, "triggered reconcile", func() bool { _, ok := h.s.LocalPath("2"); return ok })
	cancel()
	<-done
	h.mu.Lock()
	defer h.mu.Unlock()
	if *h.changes == 0 {
		t.Fatal("OnChange never called")
	}
}
