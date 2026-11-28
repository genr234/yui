//go:build windows

package clips

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// captureSource is the screen grab: the primary monitor through Desktop
// Duplication, copied to system memory.
func captureSource(fps int) string {
	return "ddagrab=output_idx=0:framerate=" + itoa(fps) + ":draw_mouse=0,hwdownload,format=bgra"
}
