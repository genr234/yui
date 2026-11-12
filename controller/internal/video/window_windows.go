//go:build windows

package video

import (
	"regexp"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// hideMode is the W-hide decision, a single constant (spike S1 confirms it).
type hideModeKind int

const (
	// hideOffscreen moves the window to x = y = -32000 and removes it from the taskbar.
	hideOffscreen hideModeKind = iota
	// hideBehind keeps the window at its place at the bottom of the z-order,
	// behind the program's fullscreen main window.
	hideBehind
)

const hideMode = hideOffscreen

const offscreenPos = -32000

const (
	gwlExStyle       = -20
	wsExToolWindow   = 0x00000080
	wsExAppWindow    = 0x00040000
	swpNoSize        = 0x0001
	swpNoMove        = 0x0002
	swpNoZOrder      = 0x0004
	swpNoActivate    = 0x0010
	swpFrameChanged  = 0x0020
	hwndBottom       = 1
	titleBufferChars = 512
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
	procGetWindowLongPtrW = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procIsWindow          = user32.NewProc("IsWindow")
	procGetWindowTextW    = user32.NewProc("GetWindowTextW")
)

func platformSupported() bool { return true }

func platformWindows() WindowSystem { return winSystem{} }

type winSystem struct{}

// The EnumWindows callback is created once (windows.NewCallback slots are
// limited), so searches run one at a time and share these variables.
var (
	searchMu   sync.Mutex
	searchPIDs map[uint32]struct{}
	searchRe   *regexp.Regexp
	searchHit  windows.HWND
	enumFind   = windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		var pid uint32
		if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil {
			return 1
		}
		if _, ok := searchPIDs[pid]; !ok || !windows.IsWindowVisible(hwnd) {
			return 1
		}
		buf := make([]uint16, titleBufferChars)
		r, _, _ := procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		n := int(r)
		if n == 0 {
			return 1
		}
		if searchRe.MatchString(windows.UTF16ToString(buf[:n])) {
			searchHit = hwnd
			return 0 // stop
		}
		return 1
	})
)

// processTree returns pid and all its descendants.
func processTree(pid uint32) map[uint32]struct{} {
	out := map[uint32]struct{}{pid: {}}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	parents := map[uint32][]uint32{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		parents[e.ParentProcessID] = append(parents[e.ParentProcessID], e.ProcessID)
	}
	queue := []uint32{pid}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, c := range parents[p] {
			if _, seen := out[c]; !seen {
				out[c] = struct{}{}
				queue = append(queue, c)
			}
		}
	}
	return out
}

func (winSystem) Find(pid int, title *regexp.Regexp) (Window, bool) {
	if pid <= 0 || title == nil {
		return 0, false
	}
	pids := processTree(uint32(pid))
	searchMu.Lock()
	defer searchMu.Unlock()
	searchPIDs, searchRe, searchHit = pids, title, 0
	_ = windows.EnumWindows(enumFind, nil) // returns an error when the callback stops early
	hit := searchHit
	searchPIDs, searchRe, searchHit = nil, nil, 0
	return Window(hit), hit != 0
}

func (winSystem) Hide(w Window) error {
	hwnd := uintptr(w)
	switch hideMode {
	case hideBehind:
		r, _, err := procSetWindowPos.Call(hwnd, hwndBottom, 0, 0, 0, 0, swpNoSize|swpNoMove|swpNoActivate)
		if r == 0 {
			return err
		}
	default:
		style, _, _ := procGetWindowLongPtrW.Call(hwnd, negIndex(gwlExStyle))
		style = style&^wsExAppWindow | wsExToolWindow
		_, _, _ = procSetWindowLongPtrW.Call(hwnd, negIndex(gwlExStyle), style)
		pos := int32(offscreenPos)
		r, _, err := procSetWindowPos.Call(hwnd, 0, uintptr(pos), uintptr(pos), 0, 0,
			swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
		if r == 0 {
			return err
		}
	}
	return nil
}

// negIndex converts a negative GetWindowLongPtr index to a uintptr.
func negIndex(i int) uintptr { return uintptr(i) }

func (winSystem) Alive(w Window) bool {
	r, _, _ := procIsWindow.Call(uintptr(w))
	return r != 0
}
