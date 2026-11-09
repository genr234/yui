package supervisor

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"kiosk/controller/internal/config"
	"kiosk/controller/internal/status"
)

func longProcess(t *testing.T) *exec.Cmd {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses sleep")
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip("no sleep:", err)
	}
	return cmd
}

func TestRestartRequestKillsChrome(t *testing.T) {
	s := New(config.Config{}, (*status.Writer)(nil))
	cmd := longProcess(t)
	go func() {
		time.Sleep(50 * time.Millisecond)
		RequestChromeRestart("test")
	}()
	done := make(chan error, 1)
	go func() { done <- s.wait(context.Background(), cmd) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("restart request did not end the wait")
	}
}

func TestWatchdogKillsChromeWhoseDebugPortIsGone(t *testing.T) {
	oldInterval, oldGrace, oldHang := watchdogInterval, startupGrace, hangAfter
	watchdogInterval, startupGrace, hangAfter = 20*time.Millisecond, 50*time.Millisecond, 100*time.Millisecond
	defer func() { watchdogInterval, startupGrace, hangAfter = oldInterval, oldGrace, oldHang }()

	// Port 1 never answers, like a hung Chrome.
	s := New(config.Config{PlatformEnabled: true, PlatformDebugPort: 1}, (*status.Writer)(nil))
	cmd := longProcess(t)
	done := make(chan error, 1)
	go func() { done <- s.wait(context.Background(), cmd) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not restart a chrome with no debugging port")
	}
}
