//go:build !windows

package useractivity

// nudge does nothing outside Windows: kiosks run on Windows, and other
// platforms have no idle tools the controller needs to keep quiet.
func nudge() error { return nil }
