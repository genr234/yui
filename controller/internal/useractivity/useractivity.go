// Package useractivity tells the operating system that someone is using the
// kiosk when the only input comes from phones.
//
// Phone input never reaches Windows as mouse or keyboard input, so to the OS
// the kiosk looks idle while people play. Idle-based tools installed on a
// kiosk then act in the middle of a game: the screensaver starts, or a
// "reset after N minutes without mouse activity" tool closes Chrome (kiosk 1
// ran one that closed Chrome every five minutes). Keeper turns player input
// into a tiny, net-zero OS input event so those tools see a person.
package useractivity

import (
	"log"
	"sync"
	"time"
)

// DefaultInterval is the shortest gap between two nudges. Idle timers on
// kiosks count in minutes, so this is plenty and keeps the cost negligible.
const DefaultInterval = 30 * time.Second

// Keeper reports user activity to the OS at most once per Interval.
type Keeper struct {
	Interval time.Duration
	Now      func() time.Time
	Nudge    func() error // platform nudge; tests replace it

	mu     sync.Mutex
	last   time.Time
	warned bool
}

// New returns a Keeper that uses the platform nudge.
func New() *Keeper {
	return &Keeper{Interval: DefaultInterval, Now: time.Now, Nudge: nudge}
}

// Touch records that a player did something. It never blocks for long.
func (k *Keeper) Touch() {
	if k == nil || k.Nudge == nil {
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

	if err := k.Nudge(); err != nil {
		k.mu.Lock()
		warn := !k.warned
		k.warned = true
		k.mu.Unlock()
		if warn {
			log.Printf("user activity: nudge failed: %v", err)
		}
	}
}
