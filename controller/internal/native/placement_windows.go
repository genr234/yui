//go:build windows

package native

import (
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetWindowLongPtrW            = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW            = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos                 = user32.NewProc("SetWindowPos")
	procGetWindowRect                = user32.NewProc("GetWindowRect")
	procGetSystemMetrics             = user32.NewProc("GetSystemMetrics")
	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
)

const (
	smCXScreen = 0
	smCYScreen = 1

	gwlStyle   = -16
	gwlExStyle = -20

	wsPopup       = 0x80000000
	wsVisible     = 0x10000000
	wsBorder      = 0x00800000
	wsCaption     = 0x00C00000 // WS_BORDER | WS_DLGFRAME
	wsSysMenu     = 0x00080000
	wsThickFrame  = 0x00040000
	wsMinimizeBox = 0x00020000
	wsMaximizeBox = 0x00010000

	wsExDlgModalFrame = 0x00000001
	wsExWindowEdge    = 0x00000100
	wsExClientEdge    = 0x00000200
	wsExStaticEdge    = 0x00020000

	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
	swpShowWindow   = 0x0040

	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is (HANDLE)-4.
	dpiPerMonitorAwareV2 = ^uintptr(3)

	placePoll = 250 * time.Millisecond
)

const styleStrip = wsCaption | wsBorder | wsSysMenu | wsThickFrame | wsMinimizeBox | wsMaximizeBox
const exStyleStrip = wsExDlgModalFrame | wsExWindowEdge | wsExClientEdge | wsExStaticEdge

type winRect struct{ Left, Top, Right, Bottom int32 }

func getLong(hwnd windows.HWND, index int) uintptr {
	v, _, _ := procGetWindowLongPtrW.Call(uintptr(hwnd), uintptr(index))
	return v
}

func setLong(hwnd windows.HWND, index int, v uintptr) {
	procSetWindowLongPtrW.Call(uintptr(hwnd), uintptr(index), v)
}

func screenSize() Screen {
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	return Screen{W: int(int32(w)), H: int(int32(h))}
}

// largestWindow picks the program's biggest visible top-level window: the
// game's render window rather than a helper.
func largestWindow(pid int) windows.HWND {
	var best windows.HWND
	bestArea := int64(-1)
	for _, hwnd := range topWindows(pid) {
		var r winRect
		procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
		area := int64(r.Right-r.Left) * int64(r.Bottom-r.Top)
		if area > bestArea {
			best, bestArea = hwnd, area
		}
	}
	return best
}

// Place polls the program's window every 250 ms: it strips the frame and
// moves the window to the placement rect whenever the program (or Windows)
// has changed either, so a game that resizes itself or re-enters fullscreen
// is put back. It stops when stop closes.
func (SystemPlacer) Place(pid int, spec WindowSpec, stop <-chan struct{}, report func(Rect, Screen)) {
	// Pixels must be physical whatever the controller's DPI awareness: make
	// this thread per-monitor aware (Windows 10 1607+; ignored when missing).
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if procSetThreadDpiAwarenessContext.Find() == nil {
		if prev, _, _ := procSetThreadDpiAwarenessContext.Call(dpiPerMonitorAwareV2); prev != 0 {
			defer procSetThreadDpiAwarenessContext.Call(prev)
		}
	}

	var last Rect
	var lastScreen Screen
	ticker := time.NewTicker(placePoll)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		hwnd := largestWindow(pid)
		if hwnd == 0 {
			continue
		}
		if iconic, _, _ := procIsIconic.Call(uintptr(hwnd)); iconic != 0 {
			continue
		}
		screen := screenSize()
		want := PlaceRect(screen, float64(spec.Aspect))
		if want.W <= 0 {
			continue
		}

		style := getLong(hwnd, gwlStyle)
		ex := getLong(hwnd, gwlExStyle)
		wantStyle := (style &^ styleStrip) | wsPopup | wsVisible
		wantEx := ex &^ exStyleStrip
		framed := wantStyle != style || wantEx != ex
		if framed {
			setLong(hwnd, gwlStyle, wantStyle)
			setLong(hwnd, gwlExStyle, wantEx)
		}
		var cur winRect
		procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&cur)))
		moved := int(cur.Left) != want.X || int(cur.Top) != want.Y ||
			int(cur.Right-cur.Left) != want.W || int(cur.Bottom-cur.Top) != want.H
		if framed || moved {
			flags := uintptr(swpNoZOrder | swpNoActivate | swpShowWindow)
			if framed {
				flags |= swpFrameChanged
			}
			procSetWindowPos.Call(uintptr(hwnd), 0, uintptr(want.X), uintptr(want.Y), uintptr(want.W), uintptr(want.H), flags)
		}
		if want != last || screen != lastScreen {
			last, lastScreen = want, screen
			report(want, screen)
		}
	}
}
