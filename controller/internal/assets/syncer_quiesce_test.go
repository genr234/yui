package assets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"kiosk/controller/internal/store"
)

// Quiesce must return while Reconcile is preempted after deciding to run and
// before it publishes a cancel or takes reconMu. The attempt must not adopt
// the bumped generation and start the old download underneath that switch.
func TestQuiesceBeforeCancelBindDoesNotDownload(t *testing.T) {
	a := fakeAsset{ID: "game1", Kind: "game", Title: "G", Filename: "g.bin", Data: data(32, 1)}
	srv := newFakeServer(t, a)
	h := newHarness(t, srv)

	entered := make(chan struct{})
	release := make(chan struct{})
	decided := make(chan struct{})
	var unbound bool
	h.s.mu.Lock()
	h.s.beforeBind = func() {
		h.s.mu.Lock()
		unbound = h.s.bound == nil && !h.s.reconciling && !h.s.quiescing
		h.s.mu.Unlock()
		close(entered)
		<-release
	}
	h.s.onQuiesceWait = func() { close(decided) }
	h.s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		errCh <- h.s.Reconcile(context.Background(), true)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("reconcile did not reach the pre-bind window")
	}
	if !unbound {
		t.Fatal("cancel was published before the bind window")
	}

	quiesced := make(chan struct{})
	go func() {
		h.s.Quiesce()
		close(quiesced)
	}()
	select {
	case <-quiesced:
	case <-time.After(2 * time.Second):
		t.Fatal("Quiesce blocked before the reconcile published a cancel")
	}

	h.s.mu.Lock()
	bumped := h.s.generation == 1 && h.s.bound == nil && h.s.quiescing
	h.s.mu.Unlock()
	if !bumped {
		t.Fatal("quiesce did not publish the generation bump before cancel registration")
	}
	srv.mu.Lock()
	downloads := srv.downloads
	srv.mu.Unlock()
	if downloads != 0 {
		t.Fatalf("download started before the reconcile bound, downloads=%d", downloads)
	}

	close(release)
	select {
	case <-decided:
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile adopted the generation Quiesce already published")
	}
	h.s.mu.Lock()
	adopted := h.s.bound != nil || !h.s.quiescing
	h.s.mu.Unlock()
	if adopted {
		t.Fatal("reconcile published a cancel for the quiesced generation")
	}
	select {
	case err := <-errCh:
		t.Fatalf("reconcile finished while the switch held it off: %v", err)
	default:
	}

	h.s.Resume()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconcile did not run after Resume")
	}
	srv.mu.Lock()
	downloads = srv.downloads
	srv.mu.Unlock()
	if downloads != 1 {
		t.Fatalf("reconcile after Resume downloaded %d times", downloads)
	}
}

// Quiesce during an open download body must cancel that stream instead of
// waiting for the rest of the file.
func TestQuiesceCancelsStreamingDownload(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/kiosk/assets" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"assets":[{"id":"game1","kind":"game","title":"G","filename":"g.bin","byte_size":` +
				"8388608" + `,"sha256":"` + strings.Repeat("ab", 32) + `"}]}`))
		case strings.HasSuffix(r.URL.Path, "/download"):
			w.Header().Set("Content-Length", "8388608")
			w.WriteHeader(http.StatusOK)
			flusher, _ := w.(http.Flusher)
			if flusher != nil {
				flusher.Flush()
			}
			if _, err := w.Write([]byte{1}); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			once.Do(func() { close(started) })
			<-r.Context().Done()
			close(cancelled)
		case strings.HasSuffix(r.URL.Path, "/status"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "store.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	syncer := NewSyncer(Config{
		DataDir:    filepath.Join(dir, "assets"),
		Remote:     &HTTPRemote{Endpoint: func() (string, string, bool) { return srv.URL, "tok", true }},
		Store:      func() (*store.DB, error) { return db, nil },
		DiskFree:   func(string) (uint64, error) { return 1 << 40, nil },
		RetryDelay: time.Millisecond,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- syncer.Reconcile(context.Background(), true)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download stream did not start")
	}

	quiesced := make(chan struct{})
	go func() {
		syncer.Quiesce()
		close(quiesced)
	}()
	select {
	case <-quiesced:
	case <-time.After(3 * time.Second):
		t.Fatal("Quiesce waited for the rest of the streaming download")
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("download request was not cancelled")
	}
	syncer.Resume()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reconcile did not return after the stream was cancelled")
	}
	if _, ok := syncer.LocalPath("game1"); ok {
		t.Fatal("cancelled stream was published as a ready file")
	}
}
