//go:build windows

package useractivity

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	user32        = syscall.NewLazyDLL("user32.dll")
	procSendInput = user32.NewProc("SendInput")
)

const (
	inputMouse      = 0
	mouseEventfMove = 0x0001
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

// nudge moves the mouse one pixel and straight back. That resets the OS idle
// timer (screensaver, GetLastInputInfo) and reaches low-level mouse hooks as
// movement, while the cursor ends where it was.
func nudge() error {
	inputs := [2]input{
		{typ: inputMouse, mi: mouseInput{dx: 1, flags: mouseEventfMove}},
		{typ: inputMouse, mi: mouseInput{dx: -1, flags: mouseEventfMove}},
	}
	n, _, err := procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	if int(n) != len(inputs) {
		return fmt.Errorf("SendInput sent %d of %d: %v", n, len(inputs), err)
	}
	return nil
}
