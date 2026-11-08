//go:build !windows

package probe

import (
	"context"
	"runtime"
)

// collect on non-Windows hosts reports only the OS; the kiosk itself runs
// Windows, so audio endpoints and GPUs are not probed here.
func collect(_ context.Context) Report {
	return Report{OS: runtime.GOOS}
}
