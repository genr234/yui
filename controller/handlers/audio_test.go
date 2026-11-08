package handlers

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/pad/session"
)

func TestAudioCommands(t *testing.T) {
	registry := NewRegistry(config.Config{})
	t.Cleanup(func() { _ = registry.Audio().Close() })

	got, err := registry.Dispatch("audio.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if st := got.(session.AudioStatus); st.Source != session.RouteAuto || st.Capturing || st.Volume != 1 {
		t.Fatalf("status %+v", st)
	}
	got, err = registry.Dispatch("audio.route", json.RawMessage(`{"source":"native"}`))
	if err != nil {
		t.Fatal(err)
	}
	if st := got.(session.AudioStatus); st.Source != session.RouteNative || st.Capturing {
		t.Fatalf("route %+v", st)
	}
	if _, err := registry.Dispatch("audio.route", json.RawMessage(`{"source":"tv"}`)); err == nil {
		t.Fatal("bad route accepted")
	}
	got, err = registry.Dispatch("pad.volume", json.RawMessage(`{"value":0.4}`))
	if err != nil {
		t.Fatal(err)
	}
	if st := got.(session.AudioStatus); st.Volume != 0.4 {
		t.Fatalf("volume %+v", st)
	}
	if _, err := registry.Dispatch("pad.volume", json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing value accepted")
	}
	if _, err := registry.Dispatch("pad.speaker", json.RawMessage(`{"player_id":"p1"}`)); err == nil {
		t.Fatal("pad.speaker without a session")
	}
	if registry.AudioSource() == nil {
		t.Fatal("no audio source hook")
	}
}

func TestChromePIDFallsBackToStatusFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "status.json")
	if err := os.WriteFile(path, []byte(`{"event":"chrome_started","chrome_pid":4321}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Port 0 means remote debugging is off, so CDP is skipped.
	registry := NewRegistry(config.Config{StatusPath: path})
	pid, err := registry.chromePID(context.Background())
	if err != nil || pid != 4321 {
		t.Fatalf("pid %d err %v", pid, err)
	}
	if _, err := statusChromePID(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing status accepted")
	}
}
