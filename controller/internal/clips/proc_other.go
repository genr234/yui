//go:build !windows

package clips

import "os/exec"

func hideWindow(*exec.Cmd) {}

// captureSource is "" off Windows: there is no screen grab, so a Buffer
// without an explicit Source does not run.
func captureSource(int) string { return "" }
