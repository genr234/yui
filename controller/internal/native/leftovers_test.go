package native

import (
	"path/filepath"
	"testing"
)

func TestUnderDir(t *testing.T) {
	root := filepath.Join("C:", "Yui", "runtimes")
	cases := map[string]bool{
		filepath.Join(root, "dolphin", "2609", "Dolphin-x64", "Dolphin.exe"): true,
		filepath.Join("C:", "Yui", "RUNTIMES", "x.exe"):                      true,
		filepath.Join("C:", "Yui", "runtimes-old", "x.exe"):                  false,
		filepath.Join("C:", "Windows", "explorer.exe"):                       false,
		root: false,
		"":   false,
	}
	for path, want := range cases {
		if got := underDir(path, root); got != want {
			t.Errorf("underDir(%q) = %v, want %v", path, got, want)
		}
	}
}
