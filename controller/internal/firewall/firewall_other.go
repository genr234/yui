//go:build !windows

// Package firewall lets phones reach the controller's pad links through
// Windows Firewall; elsewhere there is nothing to do.
package firewall

import "context"

func Ensure(context.Context) (string, error) { return "not needed", nil }
