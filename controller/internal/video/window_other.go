//go:build !windows

package video

import "regexp"

func platformSupported() bool { return false }

func platformWindows() WindowSystem { return stubWindows{} }

// stubWindows never finds a window: window capture is not available here.
type stubWindows struct{}

func (stubWindows) Find(int, *regexp.Regexp) (Window, bool) { return 0, false }
func (stubWindows) Hide(Window) error                       { return nil }
func (stubWindows) Alive(Window) bool                       { return false }
