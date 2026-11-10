//go:build !windows

package remotecontrol

import "errors"

// CaptureDesktop is only implemented on Windows, where kiosks run.
func CaptureDesktop(int) (Desktop, error) {
	return Desktop{}, errors.New("desktop capture is only supported on Windows")
}
