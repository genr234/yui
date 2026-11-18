//go:build !windows

package native

import (
	"os"
	"regexp"
	"syscall"
)

func (SystemFocus) Capture() FocusToken                         { return 0 }
func (SystemFocus) Give(int, []*regexp.Regexp, <-chan struct{}) {}
func (SystemFocus) Restore(FocusToken)                          {}

// terminate asks the process to exit (SIGTERM). It reports false when no
// graceful request could be delivered, so the caller kills right away.
func terminate(p *os.Process) bool {
	return p.Signal(syscall.SIGTERM) == nil
}
