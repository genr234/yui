// Package telemetry keeps a small in-memory log of controller health events
// (Chrome starts, exits, hangs, restarts, updates) and uploads it to the
// active account's server, so a stuck kiosk can be diagnosed without going to
// it. Nothing here blocks the caller.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxEvents = 300

// Event is one thing that happened. Data holds small, non-secret details.
type Event struct {
	At   string         `json:"at"`
	Kind string         `json:"kind"`
	Data map[string]any `json:"data,omitempty"`
}

var (
	mu        sync.Mutex
	events    []Event
	state     = map[string]any{}
	startedAt = time.Now()
	wake      = make(chan struct{}, 1)
	// Extra snapshot fields filled in by other packages (e.g. log lines dropped).
	snapshotHooks []func(map[string]any)
)

// Record adds an event; kinds in urgent are uploaded within seconds.
func Record(kind string, data map[string]any) {
	mu.Lock()
	events = append(events, Event{At: time.Now().UTC().Format(time.RFC3339Nano), Kind: kind, Data: data})
	if len(events) > maxEvents {
		events = append([]Event(nil), events[len(events)-maxEvents:]...)
	}
	mu.Unlock()
	if urgent(kind) {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

// Set records a current-state field for the snapshot (e.g. "chrome" -> "running").
func Set(key string, value any) {
	mu.Lock()
	state[key] = value
	mu.Unlock()
}

// OnSnapshot registers a function that adds fields to every snapshot.
func OnSnapshot(hook func(map[string]any)) {
	mu.Lock()
	snapshotHooks = append(snapshotHooks, hook)
	mu.Unlock()
}

func urgent(kind string) bool {
	return strings.HasPrefix(kind, "chrome_") || strings.HasPrefix(kind, "controller_") || strings.HasPrefix(kind, "update_")
}

// Snapshot is the current state plus process facts.
func Snapshot(version, commit string) map[string]any {
	mu.Lock()
	out := map[string]any{}
	for key, value := range state {
		out[key] = value
	}
	hooks := append([]func(map[string]any){}, snapshotHooks...)
	mu.Unlock()
	for _, hook := range hooks {
		hook(out)
	}
	out["version"] = version
	out["commit"] = commit
	out["controller_started_at"] = startedAt.UTC().Format(time.RFC3339)
	out["uptime_seconds"] = int(time.Since(startedAt).Seconds())
	return out
}

// Endpoint returns the server URL and device token, ok=false when not paired.
type Endpoint func() (serverURL, token string, ok bool)

// Run uploads pending events and a snapshot every interval, and soon after
// urgent events, until ctx ends. Events stay queued (up to maxEvents) while
// the server is unreachable.
func Run(ctx context.Context, endpoint Endpoint, version, commit string, interval time.Duration) {
	client := &http.Client{Timeout: 15 * time.Second}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			// Let a burst (exit, restart, start) land in one upload.
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		case <-timer.C:
		}
		_ = flush(ctx, client, endpoint, version, commit)
		timer.Reset(interval)
	}
}

func flush(ctx context.Context, client *http.Client, endpoint Endpoint, version, commit string) error {
	serverURL, token, ok := endpoint()
	if !ok {
		return nil
	}
	mu.Lock()
	batch := append([]Event(nil), events...)
	mu.Unlock()
	body, err := json.Marshal(map[string]any{"events": batch, "snapshot": Snapshot(version, commit)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(serverURL, "/")+"/api/kiosk/telemetry", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("telemetry upload: HTTP %d", resp.StatusCode)
	}
	// Drop what was sent; keep anything recorded meanwhile.
	mu.Lock()
	if len(events) >= len(batch) {
		events = append([]Event(nil), events[len(batch):]...)
	}
	mu.Unlock()
	return nil
}
