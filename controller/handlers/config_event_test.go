package handlers

import (
	"testing"
	"time"

	"kiosk/controller/internal/config"
)

func TestEventIdleTimeout(t *testing.T) {
	if got := eventIdleTimeout(config.Config{EventIdleMinutes: 5}); got != 0 {
		t.Fatalf("off by default: %v", got)
	}
	if got := eventIdleTimeout(config.Config{EventMode: true, EventIdleMinutes: 3}); got != 3*time.Minute {
		t.Fatalf("got %v", got)
	}
	if got := eventIdleTimeout(config.Config{EventMode: true}); got != 5*time.Minute {
		t.Fatalf("default: %v", got)
	}
}
