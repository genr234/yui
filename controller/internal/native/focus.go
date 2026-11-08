package native

// FocusToken identifies the window that had focus before a session started
// (on Windows, an HWND). Zero means nothing was captured.
type FocusToken uintptr

// Focuser moves keyboard/window focus between Chrome and a native program.
type Focuser interface {
	// Capture remembers the window that has focus now.
	Capture() FocusToken
	// Give brings pid's main window to the front once it appears. It returns
	// when that has happened, after a timeout, or when stop is closed.
	Give(pid int, stop <-chan struct{})
	// Restore hands focus back to the captured window.
	Restore(FocusToken)
}

// SystemFocus is the platform Focuser. On Windows it uses
// SetForegroundWindow; elsewhere every method is a no-op.
type SystemFocus struct {
	// FallbackPID returns the process to focus on Restore when the captured
	// window is gone (the supervised Chrome). Optional.
	FallbackPID func() int
}

type noFocus struct{}

func (noFocus) Capture() FocusToken       { return 0 }
func (noFocus) Give(int, <-chan struct{}) {}
func (noFocus) Restore(FocusToken)        {}
