//go:build !windows

package nativeui

func SelectFile(title string, initialDir string, filter string) (string, bool, error) {
	return "", false, nil
}

func ShowInfo(title string, body string) error {
	return nil
}

func ShowError(title string, body string) error {
	return nil
}

func AskYesNoCancel(title string, body string) (string, error) {
	return "cancel", nil
}

func AskTryContinue(title string, body string) (string, error) {
	return "cancel", nil
}
