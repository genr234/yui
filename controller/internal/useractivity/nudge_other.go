//go:build !windows

package useractivity

// osPlatform does nothing outside Windows: kiosks run on Windows, and the
// controller has no idle tools to keep quiet elsewhere.
type osPlatform struct{}

func (osPlatform) Nudge() error        { return nil }
func (osPlatform) SetAwake(bool) error { return nil }

// ResumeIdleTools does nothing outside Windows.
func ResumeIdleTools() {}
