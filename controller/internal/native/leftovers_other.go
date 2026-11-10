//go:build !windows

package native

// CloseLeftovers is a no-op off Windows, where kiosks don't run.
func CloseLeftovers(string) int { return 0 }
