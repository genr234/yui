//go:build !windows

package remotecontrol

import "image"

// CaptureDesktop is only implemented on Windows, where kiosks run.
func CaptureDesktop(int) (Desktop, error) {
	return Desktop{}, ErrDesktopUnsupported
}

func grabDesktop() (*image.RGBA, image.Point, error) {
	return nil, image.Point{}, ErrDesktopUnsupported
}
