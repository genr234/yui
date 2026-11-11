package useractivity

import (
	"strings"
	"sync"
)

// DefaultIdleTools are programs paused while a hold is active. Kiosk 1's
// DetectMouseActivity closes Chrome after five minutes without real mouse
// input and ignores injected input, so no nudge reaches it. Absent programs
// are skipped, so the default is harmless on other kiosks.
var DefaultIdleTools = []string{"DetectMouseActivity.exe"}

var idleTools = struct {
	mu    sync.Mutex
	names []string
}{names: DefaultIdleTools}

// SetIdleTools replaces the programs (executable names, any case) paused
// while a hold is active. Nil keeps the default; an empty list pauses none.
func SetIdleTools(names []string) {
	if names == nil {
		names = DefaultIdleTools
	}
	clean := make([]string, 0, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			clean = append(clean, strings.ToLower(n))
		}
	}
	idleTools.mu.Lock()
	idleTools.names = clean
	idleTools.mu.Unlock()
}

func idleToolNames() map[string]bool {
	idleTools.mu.Lock()
	defer idleTools.mu.Unlock()
	set := make(map[string]bool, len(idleTools.names))
	for _, n := range idleTools.names {
		set[strings.ToLower(n)] = true
	}
	return set
}
