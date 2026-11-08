// Package assets keeps the kiosk's local asset library in sync with the
// account's manifest on the server: background, resumable, verified downloads.
package assets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"kiosk/controller/internal/store"
)

const (
	// Collection is the Bolt collection holding local asset records.
	Collection = "assets-local"

	StateQueued      = "queued"
	StateDownloading = "downloading"
	StatePaused      = "paused"
	StateVerifying   = "verifying"
	StateReady       = "ready"
	StateFailed      = "failed"

	DefaultReserve        = uint64(5) << 30
	DefaultReportInterval = 5 * time.Second
	DefaultInterval       = 15 * time.Minute
	// DefaultOrphanGrace is how long a file a native session still has open
	// survives after it disappears from the account manifest.
	DefaultOrphanGrace = 2 * time.Minute

	chunkSize = 256 << 10
)

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// Local is the persisted record for one asset (contract: assets-local).
type Local struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Filename  string          `json:"filename"`
	ByteSize  int64           `json:"byte_size"`
	SHA256    string          `json:"sha256"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	State     string          `json:"state"`
	BytesDone int64           `json:"bytes_done"`
	Error     string          `json:"error,omitempty"`
	Path      string          `json:"path,omitempty"`
	UpdatedAt string          `json:"updated_at"`
}

// Item is the public view of an asset. It never includes a filesystem path.
type Item struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	State     string          `json:"state"`
	BytesDone int64           `json:"bytes_done"`
	ByteSize  int64           `json:"byte_size"`
}

type Current struct {
	ID        string `json:"id"`
	BytesDone int64  `json:"bytes_done"`
	ByteSize  int64  `json:"byte_size"`
}

type Status struct {
	State           string   `json:"state"`
	PausedReasons   []string `json:"paused_reasons"`
	Current         *Current `json:"current"`
	LastReconcileAt *string  `json:"last_reconcile_at"`
	Error           *string  `json:"error"`
}

// Config holds the injected dependencies of a Syncer.
type Config struct {
	DataDir  string // <ConfigDir>/assets
	Remote   Remote
	Store    func() (*store.DB, error)
	DiskFree func(path string) (uint64, error)
	Now      func() time.Time
	OnChange func()

	Reserve        uint64        // default 5 GB
	ReportInterval time.Duration // default 5 s
	Interval       time.Duration // default 15 min
	RetryDelay     time.Duration // pause between in-process resume attempts (default 2 s)
	MaxAttempts    int           // in-process attempts per reconcile (default 3)

	// InUse reports that a native session still has this asset open. While it
	// returns true the file is not deleted. After it returns false the file
	// stays until OrphanGrace (default 2 minutes) from the reconcile that first
	// saw it in use. Nil means nothing is in use (plan 05 has not wired the
	// native session registry yet), so leftovers are removed immediately.
	InUse       func(id string) bool
	OrphanGrace time.Duration
}

type failure struct {
	count int
	next  time.Time
}

// reconcileBind is one reconcile attempt's cancel. Pointer identity lets a
// finishing attempt clear only its own registration.
type reconcileBind struct {
	cancel context.CancelFunc
}

type Syncer struct {
	cfg Config

	reconMu sync.Mutex // serialises reconciles; Quiesce holds it across an account switch

	mu         sync.Mutex
	generation uint64
	// quiescing is set for the whole switch, before Quiesce waits on reconMu,
	// so an attempt cannot adopt the bumped generation and download while the
	// switch is blocked.
	quiescing bool
	// resumed is closed by Resume. Waiters must copy the channel under mu.
	resumed chan struct{}
	// binds holds every attempt that has published a cancel and not yet
	// released it. A later Reconcile must not replace an earlier one: Quiesce
	// cancels the whole set, including a stream that already holds reconMu
	// while another attempt waits on it.
	binds       map[*reconcileBind]struct{}
	reasons     map[string]struct{}
	wake        chan struct{}
	reconciling bool
	// beforeBind, when set, runs once with no syncer lock held after Reconcile
	// sees a clear switch and before it publishes its cancel or takes reconMu.
	// Tests freeze that window. The hook must not call Reconcile or Quiesce on
	// this goroutine.
	beforeBind func()
	// beforeReconLock, when set, runs with no syncer lock held after this
	// attempt has published its cancel and before it takes reconMu.
	beforeReconLock func()
	// onQuiesceWait, when set, runs once with no syncer lock held when an
	// attempt notices a switch and waits instead of publishing a cancel.
	onQuiesceWait func()
	lastReconcile time.Time
	lastErr       string
	current       *Current
	failures      map[string]failure
	orphans       map[string]time.Time // id -> first reconcile that saw it in use but unwanted

	trigger chan struct{}
	forced  bool
}

func NewSyncer(cfg Config) *Syncer {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.DiskFree == nil {
		cfg.DiskFree = DiskFree
	}
	if cfg.Reserve == 0 {
		cfg.Reserve = DefaultReserve
	}
	if cfg.ReportInterval == 0 {
		cfg.ReportInterval = DefaultReportInterval
	}
	if cfg.Interval == 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = 2 * time.Second
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 3
	}
	if cfg.OrphanGrace == 0 {
		cfg.OrphanGrace = DefaultOrphanGrace
	}
	return &Syncer{
		cfg:      cfg,
		resumed:  make(chan struct{}),
		binds:    map[*reconcileBind]struct{}{},
		reasons:  map[string]struct{}{},
		wake:     make(chan struct{}),
		failures: map[string]failure{},
		orphans:  map[string]time.Time{},
		trigger:  make(chan struct{}, 1),
	}
}

// Run reconciles on start, on TriggerReconcile and on every interval until ctx ends.
func (s *Syncer) Run(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	s.runReconcile(ctx, false)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runReconcile(ctx, false)
		case <-s.trigger:
			s.mu.Lock()
			force := s.forced
			s.forced = false
			s.mu.Unlock()
			s.runReconcile(ctx, force)
		}
	}
}

func (s *Syncer) runReconcile(ctx context.Context, force bool) {
	if err := s.Reconcile(ctx, force); err != nil && ctx.Err() == nil {
		log.Printf("assets reconcile failed: %v", err)
	}
}

// TriggerReconcile requests a reconcile without blocking; requests coalesce.
// An explicit trigger also clears retry backoff.
func (s *Syncer) TriggerReconcile() {
	s.mu.Lock()
	s.forced = true
	s.mu.Unlock()
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// Quiesce cancels the in-flight reconcile and holds the reconcile lock until
// Resume. Call Resume after the new account config and database are published
// so a reconcile that already read the previous account cannot write or delete
// on the new one. A reconcile that has not read yet waits here and then runs
// against the published account.
//
// quiescing and the generation bump are published before this waits on
// reconMu. An attempt publishes its cancel before it takes reconMu, and it
// will not keep a generation that this bump already made. Otherwise an attempt
// that held reconMu before registering its cancel would sample the new
// generation, skip cancellation, and block this switch on the old download.
func (s *Syncer) Quiesce() {
	s.mu.Lock()
	s.quiescing = true
	s.generation++
	cancels := make([]context.CancelFunc, 0, len(s.binds))
	for slot := range s.binds {
		if slot != nil && slot.cancel != nil {
			cancels = append(cancels, slot.cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	s.reconMu.Lock()
	s.mu.Lock()
	s.orphans = map[string]time.Time{}
	s.failures = map[string]failure{}
	s.mu.Unlock()
}

// Resume lets reconciles run again after Quiesce.
func (s *Syncer) Resume() {
	s.mu.Lock()
	s.quiescing = false
	resumed := s.resumed
	s.resumed = make(chan struct{})
	s.mu.Unlock()
	close(resumed)
	s.reconMu.Unlock()
}

// SetPaused adds or removes a pause reason. Downloads stop between chunks
// while any reason is set.
func (s *Syncer) SetPaused(reason string, paused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if paused {
		s.reasons[reason] = struct{}{}
		return
	}
	if _, ok := s.reasons[reason]; !ok {
		return
	}
	delete(s.reasons, reason)
	if len(s.reasons) == 0 {
		close(s.wake)
		s.wake = make(chan struct{})
	}
}

// waitUnpaused blocks while paused. It reports whether it had to wait.
func (s *Syncer) waitUnpaused(ctx context.Context, onPause func()) (bool, error) {
	waited := false
	for {
		s.mu.Lock()
		if len(s.reasons) == 0 {
			s.mu.Unlock()
			return waited, ctx.Err()
		}
		ch := s.wake
		s.mu.Unlock()
		if !waited {
			waited = true
			if onPause != nil {
				onPause()
			}
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return waited, ctx.Err()
		}
	}
}

func (s *Syncer) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{PausedReasons: []string{}}
	for r := range s.reasons {
		st.PausedReasons = append(st.PausedReasons, r)
	}
	sort.Strings(st.PausedReasons)
	if s.current != nil {
		c := *s.current
		st.Current = &c
	}
	if !s.lastReconcile.IsZero() {
		t := s.lastReconcile.UTC().Format(time.RFC3339)
		st.LastReconcileAt = &t
	}
	if s.lastErr != "" {
		e := s.lastErr
		st.Error = &e
	}
	switch {
	case len(s.reasons) > 0:
		st.State = "paused"
	case s.reconciling:
		st.State = "syncing"
	case s.lastErr != "":
		st.State = "error"
	default:
		st.State = "idle"
	}
	return st
}

func (s *Syncer) records() (map[string]Local, error) {
	db, err := s.cfg.Store()
	if err != nil {
		return nil, err
	}
	docs, err := db.Collection(Collection).List(store.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make(map[string]Local, len(docs))
	for _, d := range docs {
		var l Local
		if json.Unmarshal(d.Value, &l) == nil && l.ID != "" {
			out[l.ID] = l
		}
	}
	return out, nil
}

func (s *Syncer) save(l Local) error {
	db, err := s.cfg.Store()
	if err != nil {
		return err
	}
	l.UpdatedAt = s.cfg.Now().UTC().Format(time.RFC3339)
	return db.Collection(Collection).Put(l.ID, l)
}

// List returns the public view of local assets, optionally filtered by kind.
func (s *Syncer) List(kind string) ([]Item, error) {
	recs, err := s.records()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	cur := s.current
	s.mu.Unlock()
	items := make([]Item, 0, len(recs))
	for _, l := range recs {
		if kind != "" && l.Kind != kind {
			continue
		}
		done := l.BytesDone
		if cur != nil && cur.ID == l.ID && l.State != StateReady {
			done = cur.BytesDone
		}
		items = append(items, Item{ID: l.ID, Kind: l.Kind, Title: l.Title, Metadata: l.Metadata, State: l.State, BytesDone: done, ByteSize: l.ByteSize})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Title != items[j].Title {
			return items[i].Title < items[j].Title
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

// LocalPath returns the path of a fully downloaded asset. Go-internal only.
func (s *Syncer) LocalPath(id string) (string, bool) {
	db, err := s.cfg.Store()
	if err != nil {
		return "", false
	}
	var l Local
	ok, err := db.Collection(Collection).Decode(id, &l)
	if err != nil || !ok || l.State != StateReady || l.Path == "" {
		return "", false
	}
	if _, err := os.Stat(l.Path); err != nil {
		return "", false
	}
	return l.Path, true
}

func (s *Syncer) changed() {
	if s.cfg.OnChange != nil {
		s.cfg.OnChange()
	}
}

func (s *Syncer) finalDir(kind, id string) string {
	return filepath.Join(s.cfg.DataDir, kind, id)
}

func (s *Syncer) partialPath(id string) string {
	return filepath.Join(s.cfg.DataDir, ".partial", id)
}

func validItem(it ManifestItem) error {
	switch {
	case !safeName.MatchString(it.ID):
		return fmt.Errorf("unsafe id %q", it.ID)
	case !safeName.MatchString(it.Kind):
		return fmt.Errorf("unsafe kind %q", it.Kind)
	case it.Filename == "" || it.Filename != filepath.Base(it.Filename) || strings.ContainsAny(it.Filename, `/\:`) || it.Filename == "." || it.Filename == "..":
		return fmt.Errorf("unsafe filename %q", it.Filename)
	case it.ByteSize < 0:
		return fmt.Errorf("negative size")
	case len(it.SHA256) != 64:
		return fmt.Errorf("bad sha256")
	}
	return nil
}

// Reconcile diffs the manifest against local state and downloads what is
// missing. force clears retry backoff.
func (s *Syncer) Reconcile(ctx context.Context, force bool) error {
	gen, recCtx, slot, err := s.lockReconcile(ctx)
	if err != nil {
		return err
	}
	err = s.reconcileLocked(ctx, recCtx, gen, force)
	s.unlockReconcile(slot)
	return err
}

// lockReconcile publishes this attempt's cancel before taking reconMu. mu is
// never held while waiting for reconMu. Quiesce takes mu and drops it before
// taking reconMu, so the two locks are not inverted.
func (s *Syncer) lockReconcile(ctx context.Context) (uint64, context.Context, *reconcileBind, error) {
	for {
		if err := ctx.Err(); err != nil {
			return 0, nil, nil, err
		}
		gen, recCtx, slot, wait, err := s.bindReconcile(ctx)
		if err != nil {
			return 0, nil, nil, err
		}
		if wait != nil {
			select {
			case <-ctx.Done():
				return 0, nil, nil, ctx.Err()
			case <-wait:
			}
			continue
		}
		if s.beforeReconLock != nil {
			s.beforeReconLock()
		}
		s.reconMu.Lock()
		parentDone := ctx.Err() != nil
		stale := recCtx.Err() != nil || s.superseded(gen) || s.quiesceActive()
		if parentDone || stale {
			s.unlockReconcile(slot)
			if parentDone {
				return 0, nil, nil, ctx.Err()
			}
			if err := s.waitUntilResumed(ctx); err != nil {
				return 0, nil, nil, err
			}
			continue
		}
		return gen, recCtx, slot, nil
	}
}

// bindReconcile registers cancel for the current generation, or reports that
// the caller has to wait out a switch. It does not take reconMu.
func (s *Syncer) bindReconcile(ctx context.Context) (uint64, context.Context, *reconcileBind, <-chan struct{}, error) {
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return 0, nil, nil, nil, err
	}
	if s.quiescing {
		return 0, nil, nil, s.takeQuiesceWait(), nil
	}
	if s.beforeBind != nil {
		hook := s.beforeBind
		s.beforeBind = nil
		s.mu.Unlock()
		hook()
		s.mu.Lock()
		if err := ctx.Err(); err != nil {
			s.mu.Unlock()
			return 0, nil, nil, nil, err
		}
		if s.quiescing {
			return 0, nil, nil, s.takeQuiesceWait(), nil
		}
	}
	gen := s.generation
	recCtx, cancel := context.WithCancel(ctx)
	slot := &reconcileBind{cancel: cancel}
	if s.binds == nil {
		s.binds = map[*reconcileBind]struct{}{}
	}
	s.binds[slot] = struct{}{}
	s.reconciling = true
	s.mu.Unlock()
	return gen, recCtx, slot, nil, nil
}

// takeQuiesceWait releases mu and returns the channel Resume closes.
func (s *Syncer) takeQuiesceWait() <-chan struct{} {
	wait := s.resumed
	hook := s.onQuiesceWait
	s.onQuiesceWait = nil
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return wait
}

func (s *Syncer) unlockReconcile(slot *reconcileBind) {
	if slot != nil && slot.cancel != nil {
		slot.cancel()
	}
	s.mu.Lock()
	delete(s.binds, slot)
	if len(s.binds) == 0 {
		s.reconciling = false
	}
	s.current = nil
	s.mu.Unlock()
	s.reconMu.Unlock()
}

func (s *Syncer) quiesceActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quiescing
}

func (s *Syncer) waitUntilResumed(ctx context.Context) error {
	s.mu.Lock()
	quiescing := s.quiescing
	resumed := s.resumed
	s.mu.Unlock()
	if !quiescing {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-resumed:
		return nil
	}
}

func (s *Syncer) reconcileLocked(ctx, recCtx context.Context, gen uint64, force bool) error {
	manifest, err := s.cfg.Remote.Manifest(recCtx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if recCtx.Err() != nil || s.superseded(gen) {
			return nil
		}
		s.mu.Lock()
		if errors.Is(err, ErrNoAccount) {
			s.lastErr = ""
		} else {
			s.lastErr = err.Error()
		}
		s.mu.Unlock()
		if errors.Is(err, ErrNoAccount) {
			return nil
		}
		return err // never delete anything without a manifest
	}
	if s.superseded(gen) || recCtx.Err() != nil {
		return nil
	}

	recs, err := s.records()
	if err != nil {
		s.setErr(err)
		return err
	}

	wanted := map[string]ManifestItem{}
	for _, it := range manifest {
		if err := validItem(it); err != nil {
			log.Printf("assets: skipping manifest item %q: %v", it.ID, err)
			continue
		}
		it.SHA256 = strings.ToLower(it.SHA256)
		wanted[it.ID] = it
	}

	changed := false
	s.forgetWantedOrphans(wanted)
	// Delete records that are no longer wanted. Files a native session still
	// has open stay on disk until the orphan grace passes; the disk sweep
	// below also removes leftovers that are not in this account's bolt DB
	// (the asset directory is shared across accounts).
	for id, l := range recs {
		if s.superseded(gen) || recCtx.Err() != nil {
			return nil
		}
		if _, ok := wanted[id]; ok {
			continue
		}
		if s.keepOrphan(id) {
			continue
		}
		s.removeFiles(l)
		if db, err := s.cfg.Store(); err == nil {
			_ = db.Collection(Collection).Delete(id)
		}
		s.mu.Lock()
		delete(s.failures, id)
		s.mu.Unlock()
		changed = true
	}
	if s.superseded(gen) || recCtx.Err() != nil {
		return nil
	}
	if s.sweepUnwanted(wanted) {
		changed = true
	}

	ids := make([]string, 0, len(wanted))
	for id := range wanted {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var pending []ManifestItem
	for _, id := range ids {
		it := wanted[id]
		old, had := recs[id]
		if had && old.SHA256 == it.SHA256 && old.State == StateReady && old.Filename == it.Filename && old.Kind == it.Kind {
			if st, err := os.Stat(old.Path); err == nil && st.Size() == it.ByteSize {
				if old.Title != it.Title || string(old.Metadata) != string(it.Metadata) || old.ByteSize != it.ByteSize {
					old.Title, old.Metadata, old.ByteSize = it.Title, it.Metadata, it.ByteSize
					_ = s.save(old)
					changed = true
				}
				continue
			}
		}
		if had && (old.SHA256 != it.SHA256 || old.Filename != it.Filename || old.Kind != it.Kind) {
			s.removeFiles(old) // different version: drop the old file and partial
		}
		pending = append(pending, it)
	}
	if changed {
		s.changed()
	}

	// Queue everything first so the UI shows the whole backlog.
	for _, it := range pending {
		rec := Local{ID: it.ID, Kind: it.Kind, Title: it.Title, Filename: it.Filename, ByteSize: it.ByteSize, SHA256: it.SHA256, Metadata: it.Metadata, State: StateQueued}
		if prev, ok := recs[it.ID]; ok && prev.SHA256 == it.SHA256 {
			rec.BytesDone = prev.BytesDone
		}
		s.setState(recCtx, &rec, StateQueued, "")
	}

	for _, it := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if s.superseded(gen) || recCtx.Err() != nil {
			return nil
		}
		s.mu.Lock()
		f := s.failures[it.ID]
		s.mu.Unlock()
		if !force && !f.next.IsZero() && s.cfg.Now().Before(f.next) {
			continue
		}
		s.downloadOne(recCtx, it)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A switch that cancelled this stream must not record success or
		// continue into another asset's download.
		if s.superseded(gen) || recCtx.Err() != nil {
			return nil
		}
	}

	s.mu.Lock()
	s.lastReconcile = s.cfg.Now()
	s.lastErr = ""
	s.mu.Unlock()
	return nil
}

func (s *Syncer) setErr(err error) {
	s.mu.Lock()
	s.lastErr = err.Error()
	s.mu.Unlock()
}

func (s *Syncer) forgetWantedOrphans(wanted map[string]ManifestItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.orphans {
		if _, ok := wanted[id]; ok {
			delete(s.orphans, id)
		}
	}
}

func (s *Syncer) superseded(gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation != gen
}

// keepOrphan reports whether an unwanted file stays on disk. While InUse
// reports the file open, grace never expires. Once it is not open, it stays
// only until OrphanGrace after the reconcile that first saw it in use. A nil
// InUse hook means nothing is open. The stamp is left in place after the
// window closes so a second check in the same reconcile does not restart it.
func (s *Syncer) keepOrphan(id string) bool {
	inUse := s.cfg.InUse != nil && s.cfg.InUse(id)
	now := s.cfg.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if inUse {
		if _, ok := s.orphans[id]; !ok {
			s.orphans[id] = now
		}
		return true
	}
	first, ok := s.orphans[id]
	if !ok {
		return false
	}
	return now.Sub(first) < s.cfg.OrphanGrace
}

// sweepUnwanted removes asset directories and partials whose id is not in the
// manifest. Returns whether anything was removed.
func (s *Syncer) sweepUnwanted(wanted map[string]ManifestItem) bool {
	entries, err := os.ReadDir(s.cfg.DataDir)
	if err != nil {
		return false
	}
	removed := false
	for _, ent := range entries {
		name := ent.Name()
		if name == ".partial" {
			if s.sweepPartial(wanted) {
				removed = true
			}
			continue
		}
		if !ent.IsDir() || !safeName.MatchString(name) {
			continue
		}
		kindDir := filepath.Join(s.cfg.DataDir, name)
		ids, err := os.ReadDir(kindDir)
		if err != nil {
			continue
		}
		left := 0
		for _, idEnt := range ids {
			id := idEnt.Name()
			if !idEnt.IsDir() || !safeName.MatchString(id) {
				left++
				continue
			}
			if _, ok := wanted[id]; ok {
				left++
				continue
			}
			if s.keepOrphan(id) {
				left++
				continue
			}
			path := filepath.Join(kindDir, id)
			if err := os.RemoveAll(path); err != nil {
				log.Printf("assets: remove orphan %s: %v", path, err)
				left++
				continue
			}
			log.Printf("assets: removed %s/%s (not in manifest)", name, id)
			removed = true
		}
		if left == 0 {
			_ = os.Remove(kindDir)
		}
	}
	return removed
}

func (s *Syncer) sweepPartial(wanted map[string]ManifestItem) bool {
	dir := filepath.Join(s.cfg.DataDir, ".partial")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	removed := false
	for _, ent := range entries {
		id := ent.Name()
		if !safeName.MatchString(id) {
			continue
		}
		if _, ok := wanted[id]; ok || s.keepOrphan(id) {
			continue
		}
		path := filepath.Join(dir, id)
		if err := os.RemoveAll(path); err != nil {
			log.Printf("assets: remove partial %s: %v", path, err)
			continue
		}
		removed = true
	}
	return removed
}

func (s *Syncer) removeFiles(l Local) {
	if safeName.MatchString(l.ID) {
		_ = os.Remove(s.partialPath(l.ID))
		if safeName.MatchString(l.Kind) {
			_ = os.RemoveAll(s.finalDir(l.Kind, l.ID))
		}
	}
}

// setState persists a record, reports the change to the server (best effort)
// and notifies listeners.
func (s *Syncer) setState(ctx context.Context, rec *Local, state, errText string) {
	rec.State = state
	rec.Error = errText
	if err := s.save(*rec); err != nil {
		log.Printf("assets: save %s: %v", rec.ID, err)
	}
	s.report(ctx, rec)
	s.changed()
}

func (s *Syncer) report(ctx context.Context, rec *Local) {
	if err := s.cfg.Remote.ReportStatus(ctx, rec.ID, rec.State, rec.BytesDone, rec.Error); err != nil && !errors.Is(err, ErrNoAccount) {
		log.Printf("assets: report %s %s failed: %v", rec.ID, rec.State, err)
	}
}

type fatalError struct{ error }

func (s *Syncer) downloadOne(ctx context.Context, it ManifestItem) {
	rec := Local{ID: it.ID, Kind: it.Kind, Title: it.Title, Filename: it.Filename, ByteSize: it.ByteSize, SHA256: it.SHA256, Metadata: it.Metadata}
	var err error
	for attempt := 1; attempt <= s.cfg.MaxAttempts; attempt++ {
		err = s.attempt(ctx, it, &rec)
		if err == nil {
			s.mu.Lock()
			delete(s.failures, it.ID)
			s.mu.Unlock()
			return
		}
		var fatal fatalError
		if ctx.Err() != nil || errors.As(err, &fatal) || errors.Is(err, ErrNoAccount) {
			break
		}
		log.Printf("assets: download %s attempt %d: %v", it.ID, attempt, err)
		select {
		case <-ctx.Done():
		case <-time.After(s.cfg.RetryDelay):
		}
	}
	if ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	f := s.failures[it.ID]
	f.count++
	delay := 30 * time.Second << min(f.count-1, 6)
	if delay > s.cfg.Interval {
		delay = s.cfg.Interval
	}
	f.next = s.cfg.Now().Add(delay)
	s.failures[it.ID] = f
	s.mu.Unlock()
	s.setState(ctx, &rec, StateFailed, err.Error())
}

func (s *Syncer) attempt(ctx context.Context, it ManifestItem, rec *Local) error {
	setDownloading := func() { s.setState(ctx, rec, StateDownloading, "") }
	if _, err := s.waitUnpaused(ctx, func() { s.setState(ctx, rec, StatePaused, "") }); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(s.partialPath(it.ID)), 0o755); err != nil {
		return err
	}
	partial := s.partialPath(it.ID)
	size := int64(0)
	if st, err := os.Stat(partial); err == nil {
		size = st.Size()
		if size > it.ByteSize {
			_ = os.Remove(partial)
			size = 0
		}
	}

	need := uint64(it.ByteSize - size)
	if free, err := s.cfg.DiskFree(s.cfg.DataDir); err != nil {
		log.Printf("assets: disk free check failed: %v", err)
	} else if free < need+s.cfg.Reserve {
		return fatalError{fmt.Errorf("not enough disk space: need %s plus %s reserve, %s free",
			humanBytes(need), humanBytes(s.cfg.Reserve), humanBytes(free))}
	}

	f, err := os.OpenFile(partial, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			f.Close()
		}
	}()

	h := sha256.New()
	if size > 0 {
		if _, err := io.Copy(h, io.LimitReader(f, size)); err != nil {
			return err
		}
	}
	if _, err := f.Seek(size, io.SeekStart); err != nil {
		return err
	}

	rec.BytesDone = size
	s.mu.Lock()
	s.current = &Current{ID: it.ID, BytesDone: size, ByteSize: it.ByteSize}
	s.mu.Unlock()
	setDownloading()

	if size < it.ByteSize {
		if err := s.stream(ctx, it, rec, f, h, size); err != nil {
			return err
		}
	}

	if st, err := f.Stat(); err != nil || st.Size() != it.ByteSize {
		return errors.New("download incomplete")
	}

	s.setState(ctx, rec, StateVerifying, "")
	sum := hex.EncodeToString(h.Sum(nil))
	if sum != it.SHA256 {
		f.Close()
		closed = true
		_ = os.Remove(partial)
		return fatalError{fmt.Errorf("checksum mismatch: expected %s, got %s", it.SHA256, sum)}
	}
	if err := f.Close(); err != nil {
		closed = true
		return err
	}
	closed = true

	dir := s.finalDir(it.Kind, it.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	final := filepath.Join(dir, it.Filename)
	_ = os.Remove(final)
	if err := os.Rename(partial, final); err != nil {
		return err
	}
	rec.Path = final
	rec.BytesDone = it.ByteSize
	s.mu.Lock()
	s.current = nil
	s.mu.Unlock()
	s.setState(ctx, rec, StateReady, "")
	return nil
}

func (s *Syncer) stream(ctx context.Context, it ManifestItem, rec *Local, f *os.File, h hash.Hash, offset int64) error {
	body, _, sha, err := s.cfg.Remote.Download(ctx, it.ID, offset)
	if err != nil {
		return err
	}
	defer body.Close()
	if sha != "" && !strings.EqualFold(sha, it.SHA256) {
		return fatalError{fmt.Errorf("server file changed (sha %s, manifest %s); will retry after next manifest", sha, it.SHA256)}
	}

	buf := make([]byte, chunkSize)
	done := offset
	lastReport := s.cfg.Now()
	for done < it.ByteSize {
		if waited, err := s.waitUnpaused(ctx, func() { s.setState(ctx, rec, StatePaused, "") }); err != nil {
			return err
		} else if waited {
			rec.BytesDone = done
			s.setState(ctx, rec, StateDownloading, "")
		}
		want := int64(len(buf))
		if rem := it.ByteSize - done; rem < want {
			want = rem
		}
		n, rerr := body.Read(buf[:want])
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			h.Write(buf[:n])
			done += int64(n)
			s.mu.Lock()
			if s.current != nil {
				s.current.BytesDone = done
			}
			s.mu.Unlock()
			if now := s.cfg.Now(); now.Sub(lastReport) >= s.cfg.ReportInterval {
				lastReport = now
				rec.BytesDone = done
				if err := s.save(*rec); err != nil {
					log.Printf("assets: save progress %s: %v", rec.ID, err)
				}
				s.report(ctx, rec)
				s.changed()
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				if done < it.ByteSize {
					return io.ErrUnexpectedEOF
				}
				break
			}
			return rerr
		}
	}
	rec.BytesDone = done
	return nil
}

func humanBytes(n uint64) string {
	const unit = 1 << 10
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
