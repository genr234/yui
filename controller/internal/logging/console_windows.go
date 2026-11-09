//go:build windows

package logging

import (
	"log"

	"golang.org/x/sys/windows"
)

const (
	enableQuickEditMode = 0x0040
	enableExtendedFlags = 0x0080
)

// disableQuickEdit stops touches or clicks on the console window from starting
// a text selection, which pauses the process's console output.
func disableQuickEdit() {
	handle, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil || handle == windows.InvalidHandle || handle == 0 {
		return
	}
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return // no console (service, redirected input)
	}
	next := (mode &^ enableQuickEditMode) | enableExtendedFlags
	if next == mode {
		return
	}
	if err := windows.SetConsoleMode(handle, next); err != nil {
		log.Printf("console: could not turn off QuickEdit: %v", err)
	}
}
