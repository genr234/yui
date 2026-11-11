//go:build !windows

package hotspot

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("hotspot: only on Windows")

func Start(context.Context, string, string) (string, error) { return "", errUnsupported }
func Stop(context.Context) (string, error)                  { return "off", nil }
func Supported() bool                                       { return false }
