// Package inputstats adds up the phones' input_stats windows (how touch play
// felt: dead touches, touches between face buttons, d-pad flicker, ...) per
// layout and Touch assist setting, so the two settings can be compared. The
// totals go into the telemetry snapshot; nothing here names a player.
package inputstats

import (
	"sort"
	"sync"
	"time"

	"kiosk/controller/internal/pad/protocol"
)

const (
	// MaxGroups bounds the layout × assist groups kept (apps choose layout names).
	MaxGroups = 32
	// MaxDeadAt is how many recent dead-touch positions a group keeps.
	MaxDeadAt = 256
)

// Group is the running total for one layout and assist setting.
type Group struct {
	Layout   string             `json:"layout"`
	Assist   bool               `json:"assist"`
	Windows  int                `json:"windows"`
	PlayedMs float64            `json:"played_ms"`
	Counts   map[string]float64 `json:"counts"`
	// Rates per 100 touches (or per 100 presses for the face-button keys).
	Rates  map[string]float64 `json:"rates,omitempty"`
	DeadAt [][2]float64       `json:"dead_at,omitempty"`
}

// Totals is safe for concurrent use.
type Totals struct {
	mu     sync.Mutex
	since  time.Time
	groups map[string]*Group
}

// New starts empty totals.
func New() *Totals {
	return &Totals{since: time.Now(), groups: map[string]*Group{}}
}

func key(layout string, assist bool) string {
	if assist {
		return layout + "|assist"
	}
	return layout + "|plain"
}

// Add counts one window. Windows for a new group past MaxGroups are dropped.
func (t *Totals) Add(m protocol.InputStats) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := key(m.Layout, m.Assist)
	g, ok := t.groups[k]
	if !ok {
		if len(t.groups) >= MaxGroups {
			return
		}
		g = &Group{Layout: m.Layout, Assist: m.Assist, Counts: map[string]float64{}}
		t.groups[k] = g
	}
	g.Windows++
	g.PlayedMs += m.WindowMs
	for name, n := range m.Counts {
		if _, known := g.Counts[name]; !known && len(g.Counts) >= protocol.InputStatsMaxKeys {
			continue
		}
		g.Counts[name] += n
	}
	g.DeadAt = append(g.DeadAt, m.DeadAt...)
	if len(g.DeadAt) > MaxDeadAt {
		g.DeadAt = append([][2]float64(nil), g.DeadAt[len(g.DeadAt)-MaxDeadAt:]...)
	}
}

// per100 is which count each rate is taken against.
var per100 = map[string]string{
	"dead":          "touches",
	"gap":           "touches",
	"gap_miss":      "touches",
	"dpad_deadzone": "touches",
	"chord":         "press",
	"slide":         "press",
	"correction":    "press",
	"dpad_flicker":  "dpad_change",
}

// Snapshot is a copy of the totals for telemetry: {since, groups: [...]},
// groups sorted by layout then assist.
func (t *Totals) Snapshot() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	groups := make([]Group, 0, len(t.groups))
	for _, g := range t.groups {
		c := Group{Layout: g.Layout, Assist: g.Assist, Windows: g.Windows, PlayedMs: g.PlayedMs, Counts: map[string]float64{}}
		for k, v := range g.Counts {
			c.Counts[k] = v
		}
		for name, base := range per100 {
			if n, d := g.Counts[name], g.Counts[base]; d > 0 {
				if c.Rates == nil {
					c.Rates = map[string]float64{}
				}
				c.Rates[name] = float64(int(n/d*10000+0.5)) / 100
			}
		}
		c.DeadAt = append([][2]float64(nil), g.DeadAt...)
		groups = append(groups, c)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Layout != groups[j].Layout {
			return groups[i].Layout < groups[j].Layout
		}
		return !groups[i].Assist && groups[j].Assist
	})
	return map[string]any{"since": t.since.UTC().Format(time.RFC3339), "groups": groups}
}
