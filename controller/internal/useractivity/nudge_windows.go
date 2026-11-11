//go:build windows

package useractivity

import (
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procSendInput            = user32.NewProc("SendInput")
	procSystemParametersInfo = user32.NewProc("SystemParametersInfoW")
	procFindWindowW          = user32.NewProc("FindWindowW")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procSetThreadExecState   = kernel32.NewProc("SetThreadExecutionState")
)

const (
	inputMouse      = 0
	mouseEventfMove = 0x0001

	spiGetScreenSaverRunning = 0x0072
	wmClose                  = 0x0010

	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
	esContinuous      = 0x80000000
)

// mouseInput and input mirror MOUSEINPUT and INPUT. The uintptr field gives
// mouseInput pointer alignment, so mi lands at offset 8 on 64-bit and 4 on
// 32-bit Windows, as in the C layout.
type mouseInput struct {
	dx, dy    int32
	mouseData uint32
	flags     uint32
	time      uint32
	extraInfo uintptr
}

type input struct {
	typ uint32
	mi  mouseInput
}

type osPlatform struct{}

// Nudge sends a mouse move of zero distance. That is real input to Windows:
// it resets the idle timer (screensaver, display sleep, GetLastInputInfo)
// and reaches low-level mouse hooks, yet the cursor does not move, so the
// page under it gets no mousemove (a video player would otherwise flash
// its controls). A screensaver that is already running is then closed,
// since zero movement is below what screensavers react to.
func (osPlatform) Nudge() error {
	inputs := [1]input{
		{typ: inputMouse, mi: mouseInput{flags: mouseEventfMove}},
	}
	var errs []error
	n, _, err := procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	if int(n) != len(inputs) {
		errs = append(errs, fmt.Errorf("SendInput sent %d of %d: %v", n, len(inputs), err))
	}
	if err := closeScreensaver(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// awake runs SetThreadExecutionState on one OS thread for as long as the
// display must stay on: the state belongs to the calling thread, and is
// cleared by Windows when that thread or the controller exits.
var awake struct {
	mu   sync.Mutex
	stop chan struct{}
}

func (osPlatform) SetAwake(on bool) error {
	awake.mu.Lock()
	defer awake.mu.Unlock()
	if on == (awake.stop != nil) {
		return nil
	}
	if !on {
		close(awake.stop)
		awake.stop = nil
		resumeIdleTools()
		return nil
	}
	stop := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		// Stay on this thread until stop; never unlocked, so Windows
		// retires the thread (and its execution state) when we return.
		runtime.LockOSThread()
		r, _, err := procSetThreadExecState.Call(uintptr(esContinuous | esSystemRequired | esDisplayRequired))
		if r == 0 {
			result <- fmt.Errorf("SetThreadExecutionState: %v", err)
			return
		}
		result <- nil
		<-stop
		procSetThreadExecState.Call(uintptr(esContinuous))
	}()
	if err := <-result; err != nil {
		return err
	}
	awake.stop = stop
	pauseIdleTools()
	return nil
}

// closeScreensaver ends a running screensaver: first the standard way (post
// WM_CLOSE to its window), then by ending any *.scr process of this user,
// since custom screensavers (kiosk 1 runs a video one) may not use the
// standard window class.
func closeScreensaver() error {
	var running uint32
	procSystemParametersInfo.Call(spiGetScreenSaverRunning, 0, uintptr(unsafe.Pointer(&running)), 0)

	if class, err := syscall.UTF16PtrFromString("WindowsScreenSaverClass"); err == nil {
		if hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(class)), 0); hwnd != 0 {
			procPostMessageW.Call(hwnd, wmClose, 0, 0)
		}
	}
	ended, err := endScreensaverProcesses()
	if ended > 0 || running != 0 {
		log.Printf("user activity: closed the screensaver (running=%v, processes ended=%d)", running != 0, ended)
	}
	return err
}

func endScreensaverProcesses() (int, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, fmt.Errorf("process scan: %w", err)
	}
	defer windows.CloseHandle(snap)

	var mySession uint32
	_ = windows.ProcessIdToSessionId(uint32(os.Getpid()), &mySession)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	ended := 0
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if !strings.HasSuffix(strings.ToLower(name), ".scr") {
			continue
		}
		var session uint32
		if windows.ProcessIdToSessionId(entry.ProcessID, &session) != nil || session != mySession {
			continue
		}
		h, openErr := windows.OpenProcess(windows.PROCESS_TERMINATE, false, entry.ProcessID)
		if openErr != nil {
			continue
		}
		if windows.TerminateProcess(h, 0) == nil {
			ended++
		}
		windows.CloseHandle(h)
	}
	return ended, nil
}
