// Package useractivity keeps the kiosk's OS from treating a game as an idle
// machine.
//
// Phone input never reaches Windows as mouse or keyboard input, so to the OS
// the kiosk looks idle while people play. Idle-based tools installed on a
// kiosk then act in the middle of a game: the screensaver starts, the display
// sleeps, or a "reset after N minutes without mouse activity" tool closes
// Chrome (kiosk 1 ran one that closed Chrome every five minutes).
//
// Two things keep the OS awake:
//
//   - Touch: player input from phones. Each call becomes, at most once per
//     Interval, a zero-distance mouse nudge that also closes a running screensaver.
//   - Holds (wake locks): an app asks to keep the kiosk awake even with no
//     input at all (a video, a cutscene, a game with no controls). While any
//     hold is active the display and system stay on, the screensaver is kept
//     closed and the nudge repeats every Interval. A hold is a lease: it ends
//     when released, and also by itself when its owner stops renewing it
//     (the app closed, the page reloaded or Chrome died). When the last hold
//     ends, the OS's own idle behaviour comes back untouched; nothing is
//     changed persistently, so even a controller crash leaves no trace.
package useractivity

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"sort"
	"sync"
	"time"
)

const (
	// DefaultInterval is the shortest gap between two nudges. Idle timers on
	// kiosks count in minutes (Windows' screensaver minimum is one), so this
	// keeps them from ever firing at negligible cost.
	DefaultInterval = 30 * time.Second
	// DefaultHoldTTL is how long a hold lasts without a renewal. Owners
	// renew well before it (the platform every TTL/3).
	DefaultHoldTTL = 60 * time.Second
	// DefaultTick is how often an active keeper checks leases and nudges.
	DefaultTick = 5 * time.Second
)

// Platform is what the keeper asks of the OS. Tests replace it.
type Platform interface {
	// Nudge reports user input to the OS without moving the cursor for good
	// and closes a screensaver that is running.
	Nudge() error
	// SetAwake keeps the display and system from sleeping (true) or gives
	// that control back to the OS (false).
	SetAwake(on bool) error
}

// Hold is one active wake lock.
type Hold struct {
	ID     string    `json:"id"`
	Owner  string    `json:"owner"`
	Reason string    `json:"reason,omitempty"`
	Since  time.Time `json:"since"`
	// Expires is when the lease runs out unless renewed; zero for a pinned
	// hold, which only ends when released.
	Expires time.Time `json:"expires,omitempty"`
}

// Keeper turns player activity and wake locks into OS activity.
type Keeper struct {
	Interval time.Duration
	HoldTTL  time.Duration
	Tick     time.Duration
	Now      func() time.Time
	Platform Platform

	mu       sync.Mutex
	onChange func(holds []Hold)
	last     time.Time // last nudge
	warned   bool
	holds    map[string]Hold
	running  bool          // the hold loop is active
	stop     chan struct{} // closes the hold loop early (tests, Close)
}

// New returns a Keeper that uses the platform's OS hooks.
func New() *Keeper {
	return &Keeper{
		Interval: DefaultInterval,
		HoldTTL:  DefaultHoldTTL,
		Tick:     DefaultTick,
		Now:      time.Now,
		Platform: osPlatform{},
	}
}

var (
	defaultOnce sync.Once
	defaultK    *Keeper
)

// Default is the controller-wide keeper shared by the pad hub and the wake
// lock commands, so both count toward the same nudge budget.
func Default() *Keeper {
	defaultOnce.Do(func() { defaultK = New() })
	return defaultK
}

// Touch records that a player did something. It never blocks for long.
func (k *Keeper) Touch() {
	if k == nil || k.Platform == nil {
		return
	}
	now := k.Now()
	k.mu.Lock()
	if !k.last.IsZero() && now.Sub(k.last) < k.Interval {
		k.mu.Unlock()
		return
	}
	k.last = now
	k.mu.Unlock()
	k.nudge()
}

// Acquire starts a hold for owner (an app id, for example). It lasts
// HoldTTL unless renewed.
func (k *Keeper) Acquire(owner, reason string) Hold {
	now := k.Now()
	return k.add(Hold{ID: newID(), Owner: owner, Reason: reason, Since: now, Expires: now.Add(k.HoldTTL)})
}

// AcquirePinned starts a hold with no lease, for owners inside the
// controller that always release (the native session, for example). It
// lasts until Release, ReleaseOwner or Close.
func (k *Keeper) AcquirePinned(owner, reason string) Hold {
	return k.add(Hold{ID: newID(), Owner: owner, Reason: reason, Since: k.Now()})
}

// HasOwner reports whether owner holds at least one hold.
func (k *Keeper) HasOwner(owner string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	for _, h := range k.holds {
		if h.Owner == owner {
			return true
		}
	}
	return false
}

func (k *Keeper) add(h Hold) Hold {
	k.mu.Lock()
	if k.holds == nil {
		k.holds = map[string]Hold{}
	}
	k.holds[h.ID] = h
	start := !k.running
	if start {
		k.running = true
		k.stop = make(chan struct{})
	}
	stop := k.stop
	holds := k.snapshotLocked()
	k.mu.Unlock()

	log.Printf("wake lock: %s acquired by %s (%s); %d active", h.ID, h.Owner, reasonOr(h.Reason), len(holds))
	if start {
		if err := k.Platform.SetAwake(true); err != nil {
			log.Printf("wake lock: keep awake failed: %v", err)
		}
		go k.loop(stop)
	}
	// Close a screensaver that is already up right away instead of at the
	// next tick: the app wants its screen visible now.
	k.forceNudge()
	k.changed(holds)
	return h
}

// Renew extends a hold by HoldTTL. It reports false when the hold is gone
// (released, expired, or the controller restarted); the owner should then
// acquire a new one.
func (k *Keeper) Renew(id string) (Hold, bool) {
	now := k.Now()
	k.mu.Lock()
	defer k.mu.Unlock()
	h, ok := k.holds[id]
	if !ok {
		return Hold{}, false
	}
	if h.Expires.IsZero() {
		return h, true // pinned
	}
	h.Expires = now.Add(k.HoldTTL)
	k.holds[id] = h
	return h, true
}

// Release ends a hold. It reports whether the hold existed.
func (k *Keeper) Release(id string) bool {
	k.mu.Lock()
	h, ok := k.holds[id]
	if ok {
		delete(k.holds, id)
	}
	holds := k.snapshotLocked()
	k.mu.Unlock()
	if !ok {
		return false
	}
	log.Printf("wake lock: %s released by %s; %d active", id, h.Owner, len(holds))
	k.changed(holds)
	k.settle()
	return true
}

// ReleaseOwner ends every hold of owner and returns how many there were.
func (k *Keeper) ReleaseOwner(owner string) int {
	k.mu.Lock()
	n := 0
	for id, h := range k.holds {
		if h.Owner == owner {
			delete(k.holds, id)
			n++
		}
	}
	holds := k.snapshotLocked()
	k.mu.Unlock()
	if n > 0 {
		log.Printf("wake lock: released %d held by %s; %d active", n, owner, len(holds))
		k.changed(holds)
		k.settle()
	}
	return n
}

// Holds returns the active holds, oldest first.
func (k *Keeper) Holds() []Hold {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.snapshotLocked()
}

// Active reports whether any hold is active.
func (k *Keeper) Active() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.holds) > 0
}

// loop runs while holds exist: it expires leases and keeps nudging. When the
// last hold is gone it hands sleep and idle back to the OS and exits.
func (k *Keeper) loop(stop chan struct{}) {
	ticker := time.NewTicker(k.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		if !k.expire() {
			return
		}
		now := k.Now()
		k.mu.Lock()
		due := k.last.IsZero() || now.Sub(k.last) >= k.Interval
		if due {
			k.last = now
		}
		k.mu.Unlock()
		if due {
			k.nudge()
		}
	}
}

// expire drops holds past their lease. It reports whether any remain; when
// none do, it has already given idle control back to the OS.
func (k *Keeper) expire() bool {
	now := k.Now()
	k.mu.Lock()
	var gone []Hold
	for id, h := range k.holds {
		if !h.Expires.IsZero() && !now.Before(h.Expires) {
			delete(k.holds, id)
			gone = append(gone, h)
		}
	}
	holds := k.snapshotLocked()
	k.mu.Unlock()
	for _, h := range gone {
		log.Printf("wake lock: %s held by %s expired (not renewed); %d active", h.ID, h.Owner, len(holds))
	}
	if len(gone) > 0 {
		k.changed(holds)
	}
	return !k.settle()
}

// settle stops keeping the OS awake once no hold is left. It reports
// whether it did (or nothing was running).
func (k *Keeper) settle() bool {
	k.mu.Lock()
	if len(k.holds) > 0 {
		k.mu.Unlock()
		return false
	}
	wasRunning := k.running
	if wasRunning {
		k.running = false
		close(k.stop)
		k.stop = nil
	}
	k.mu.Unlock()
	if wasRunning {
		if err := k.Platform.SetAwake(false); err != nil {
			log.Printf("wake lock: give idle back failed: %v", err)
		}
		log.Printf("wake lock: none active; screensaver and idle tools work normally again")
	}
	return true
}

// Close ends every hold and gives idle control back to the OS.
func (k *Keeper) Close() {
	k.mu.Lock()
	k.holds = nil
	holds := k.snapshotLocked()
	k.mu.Unlock()
	k.changed(holds)
	k.settle()
}

func (k *Keeper) forceNudge() {
	k.mu.Lock()
	k.last = k.Now()
	k.mu.Unlock()
	k.nudge()
}

func (k *Keeper) nudge() {
	if err := k.Platform.Nudge(); err != nil {
		k.mu.Lock()
		warn := !k.warned
		k.warned = true
		k.mu.Unlock()
		if warn {
			log.Printf("user activity: nudge failed: %v", err)
		}
	}
}

// SetOnChange registers fn to be called (outside the keeper's lock)
// whenever the set of holds changes, with the holds now active.
func (k *Keeper) SetOnChange(fn func(holds []Hold)) {
	k.mu.Lock()
	k.onChange = fn
	k.mu.Unlock()
}

func (k *Keeper) changed(holds []Hold) {
	k.mu.Lock()
	fn := k.onChange
	k.mu.Unlock()
	if fn != nil {
		fn(holds)
	}
}

func (k *Keeper) snapshotLocked() []Hold {
	out := make([]Hold, 0, len(k.holds))
	for _, h := range k.holds {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Since.Equal(out[j].Since) {
			return out[i].ID < out[j].ID
		}
		return out[i].Since.Before(out[j].Since)
	})
	return out
}

func reasonOr(reason string) string {
	if reason == "" {
		return "no reason given"
	}
	return reason
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "wl_" + hex.EncodeToString(b[:])
}
