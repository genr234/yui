package native

import (
	"path/filepath"
	"strings"
)

// underDir reports whether path is inside dir (case-insensitive, as on
// Windows file systems).
func underDir(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(dir)), strings.ToLower(filepath.Clean(path)))
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..")
}
