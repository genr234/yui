package native

import "regexp"

// FocusToken identifies the window that had focus before a session started
// (on Windows, an HWND). Zero means nothing was captured.
type FocusToken uintptr

// Focuser moves keyboard/window focus between Chrome and a native program.
type Focuser interface {
	// Capture remembers the window that has focus now.
	Capture() FocusToken
	// Give brings pid's main window to the front once it appears, never a
	// window whose title matches one of skip (a surface window). It returns
	// when that has happened, after a timeout, or when stop is closed.
	Give(pid int, skip []*regexp.Regexp, stop <-chan struct{})
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

// FocusKeeper is an optional Focuser extension that keeps a program in front
// for as long as it runs: a tap on the page around the game (the letterbox
// bars) or another window popping up must not leave the game behind Chrome.
type FocusKeeper interface {
	// Hold brings pid's main window back whenever focus leaves the program,
	// letting a press in progress finish first. It returns when stop or
	// release is closed.
	Hold(pid int, skip []*regexp.Regexp, stop, release <-chan struct{})
}

type noFocus struct{}

func (noFocus) Capture() FocusToken                         { return 0 }
func (noFocus) Give(int, []*regexp.Regexp, <-chan struct{}) {}
func (noFocus) Restore(FocusToken)                          {}

// titledWindow is a window handle and its title.
type titledWindow struct {
	id    uintptr
	title string
}

// pickWindow returns the first window whose title matches none of skip.
func pickWindow(wins []titledWindow, skip []*regexp.Regexp) (titledWindow, bool) {
	for _, w := range wins {
		if !titleSkipped(w.title, skip) {
			return w, true
		}
	}
	return titledWindow{}, false
}

func titleSkipped(title string, skip []*regexp.Regexp) bool {
	for _, re := range skip {
		if re.MatchString(title) {
			return true
		}
	}
	return false
}
