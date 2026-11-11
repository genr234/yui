//go:build !windows

package hotspot

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("hotspot: only on Windows")

func Start(context.Context, string, string) (string, *Original, error) {
	return "", nil, errUnsupported
}
func Stop(context.Context, *Original) (string, error) { return "off", nil }
func Supported() bool                                 { return false }
