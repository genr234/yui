//go:build !windows

package main

func selectFile(title string, initialDir string, filter string) (string, bool, error) {
	return "", false, nil
}

const (
	messageOK          uintptr = 0
	messageYesNo       uintptr = 0
	messageYesNoCancel uintptr = 0
	messageError       uintptr = 0
	messageQuestion    uintptr = 0
	messageInfo        uintptr = 0
	dialogYes          uintptr = 6
	dialogNo           uintptr = 7
)

func messageBox(title string, body string, icon uintptr) (uintptr, uintptr, error) {
	return 0, 0, nil
}

func defaultInitialDir() string {
	return "."
}

func relaunchElevatedIfNeeded(mode installMode, target string) (bool, error) {
	return false, nil
}
