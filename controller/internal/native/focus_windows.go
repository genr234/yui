//go:build windows

package native

import (
	"os"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop    = user32.NewProc("BringWindowToTop")
	procShowWindow          = user32.NewProc("ShowWindow")
	procIsIconic            = user32.NewProc("IsIconic")
	procGetWindow           = user32.NewProc("GetWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procAttachThreadInput   = user32.NewProc("AttachThreadInput")
	procKeybdEvent          = user32.NewProc("keybd_event")
)

const (
	gwOwner        = 4
	swRestore      = 9
	wmClose        = 0x0010
	vkMenu         = 0x12
	keyeventfKeyUp = 0x0002

	focusPoll    = 250 * time.Millisecond
	focusTimeout = 20 * time.Second
)

// windowSearch is shared by the single EnumWindows callback below.
// windows.NewCallback slots are limited, so the callback is created once
// and searches run one at a time.
var (
	windowSearchMu  sync.Mutex
	windowSearchPID uint32
	windowSearchOut []windows.HWND
	enumCallback    = windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		var pid uint32
		if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid != windowSearchPID {
			return 1
		}
		if !windows.IsWindowVisible(hwnd) {
			return 1
		}
		if owner, _, _ := procGetWindow.Call(uintptr(hwnd), gwOwner); owner != 0 {
			return 1
		}
		windowSearchOut = append(windowSearchOut, hwnd)
		return 1
	})
)

// topWindows returns the visible, unowned top-level windows of pid.
func topWindows(pid int) []windows.HWND {
	if pid <= 0 {
		return nil
	}
	windowSearchMu.Lock()
	defer windowSearchMu.Unlock()
	windowSearchPID = uint32(pid)
	windowSearchOut = nil
	_ = windows.EnumWindows(enumCallback, nil)
	out := windowSearchOut
	windowSearchOut = nil
	return out
}

func windowPID(hwnd windows.HWND) int {
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil {
		return 0
	}
	return int(pid)
}

// bringToFront works around the foreground lock: the controller is a
// background process, so a plain SetForegroundWindow is usually refused.
// It first attaches to the foreground thread's input, then falls back to a
// synthetic Alt tap, which Windows treats as user input.
func bringToFront(hwnd windows.HWND) bool {
	if hwnd == 0 || !windows.IsWindow(hwnd) {
		return false
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if iconic, _, _ := procIsIconic.Call(uintptr(hwnd)); iconic != 0 {
		procShowWindow.Call(uintptr(hwnd), swRestore)
	}
	if windows.GetForegroundWindow() == hwnd {
		return true
	}

	fg := windows.GetForegroundWindow()
	current := windows.GetCurrentThreadId()
	var fgThread uint32
	if fg != 0 {
		fgThread, _ = windows.GetWindowThreadProcessId(fg, nil)
	}
	attached := false
	if fgThread != 0 && fgThread != current {
		r, _, _ := procAttachThreadInput.Call(uintptr(current), uintptr(fgThread), 1)
		attached = r != 0
	}
	procBringWindowToTop.Call(uintptr(hwnd))
	procSetForegroundWindow.Call(uintptr(hwnd))
	if attached {
		procAttachThreadInput.Call(uintptr(current), uintptr(fgThread), 0)
	}
	if windows.GetForegroundWindow() == hwnd {
		return true
	}

	procKeybdEvent.Call(vkMenu, 0, 0, 0)
	procSetForegroundWindow.Call(uintptr(hwnd))
	procKeybdEvent.Call(vkMenu, 0, keyeventfKeyUp, 0)
	return windows.GetForegroundWindow() == hwnd
}

func (SystemFocus) Capture() FocusToken {
	return FocusToken(windows.GetForegroundWindow())
}

func (SystemFocus) Give(pid int, stop <-chan struct{}) {
	deadline := time.Now().Add(focusTimeout)
	ticker := time.NewTicker(focusPoll)
	defer ticker.Stop()
	for {
		if fg := windows.GetForegroundWindow(); fg != 0 && windowPID(fg) == pid {
			return
		}
		if wins := topWindows(pid); len(wins) > 0 && bringToFront(wins[0]) {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
	}
}

func (f SystemFocus) Restore(token FocusToken) {
	if hwnd := windows.HWND(token); hwnd != 0 && windows.IsWindow(hwnd) && bringToFront(hwnd) {
		return
	}
	if f.FallbackPID == nil {
		return
	}
	if wins := topWindows(f.FallbackPID()); len(wins) > 0 {
		bringToFront(wins[0])
	}
}

// terminate posts WM_CLOSE to the process's top-level windows, the closest
// Windows has to SIGTERM for a GUI program. It reports false when the
// process has no window to ask, so the caller kills right away.
func terminate(p *os.Process) bool {
	wins := topWindows(p.Pid)
	for _, hwnd := range wins {
		procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
	}
	return len(wins) > 0
}
