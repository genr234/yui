package protocol

import (
	"errors"
	"fmt"
)

// Haptic limits for custom patterns (on/off ms steps).
const (
	HapticMaxSteps   = 16
	HapticMaxStepMS  = 1000
	HapticMaxTotalMS = 4000
)

// HapticNames is the pattern vocabulary. The phone owns the timings.
var HapticNames = []string{"tap", "success", "fail", "hit", "heavy", "heartbeat", "countdown", "win"}

// Validate checks a haptic message: exactly one of a known name or a custom
// pattern within the limits, and an intensity in 0–1. A nil Pattern means
// "absent"; an empty non-nil one is invalid.
func (h Haptic) Validate() error {
	hasPattern := h.Pattern != nil
	hasName := h.Name != ""
	if hasPattern == hasName {
		return errors.New("need exactly one of pattern or name")
	}
	if hasName {
		known := false
		for _, n := range HapticNames {
			if n == h.Name {
				known = true
			}
		}
		if !known {
			return fmt.Errorf("unknown haptic name %q", h.Name)
		}
	}
	if hasPattern {
		if len(h.Pattern) == 0 || len(h.Pattern) > HapticMaxSteps {
			return fmt.Errorf("pattern needs 1-%d steps", HapticMaxSteps)
		}
		total := 0
		for _, ms := range h.Pattern {
			if ms < 0 || ms > HapticMaxStepMS {
				return fmt.Errorf("pattern steps are 0-%d ms", HapticMaxStepMS)
			}
			total += ms
		}
		if total > HapticMaxTotalMS {
			return fmt.Errorf("pattern is longer than %d ms", HapticMaxTotalMS)
		}
	}
	if h.Intensity != nil && (*h.Intensity < 0 || *h.Intensity > 1) {
		return errors.New("intensity must be 0-1")
	}
	return nil
}
