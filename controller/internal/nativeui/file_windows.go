//go:build windows

package nativeui

import (
	"syscall"
	"unsafe"
)

var (
	comdlg32         = syscall.NewLazyDLL("comdlg32.dll")
	user32           = syscall.NewLazyDLL("user32.dll")
	getOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
	messageBoxW      = user32.NewProc("MessageBoxW")
)

type openFileName struct {
	structSize    uint32
	owner         uintptr
	instance      uintptr
	filter        *uint16
	customFilter  *uint16
	maxCustFilter uint32
	filterIndex   uint32
	file          *uint16
	maxFile       uint32
	fileTitle     *uint16
	maxFileTitle  uint32
	initialDir    *uint16
	title         *uint16
	flags         uint32
	fileOffset    uint16
	fileExtension uint16
	defExt        *uint16
	custData      uintptr
	hook          uintptr
	templateName  *uint16
	reservedPtr   uintptr
	reserved      uint32
	flagsEx       uint32
}

const (
	ofnFileMustExist     = 0x00001000
	ofnPathMustExist     = 0x00000800
	ofnExplorer          = 0x00080000
	msgOK                = 0x00000000
	msgYesNoCancel       = 0x00000003
	msgIconError         = 0x00000010
	msgIconQuestion      = 0x00000020
	msgIconInfo          = 0x00000040
	msgCancelTryContinue = 0x00000006
	choiceCancel         = 2
	choiceTryAgain       = 10
	choiceContinue       = 11
	choiceYes            = 6
	choiceNo             = 7
)

func SelectFile(title string, initialDir string, filter string) (string, bool, error) {
	buffer := make([]uint16, 32768)
	filterPtr := utf16DoubleNull(filter)
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return "", false, err
	}
	initialDirPtr, err := syscall.UTF16PtrFromString(initialDir)
	if err != nil {
		return "", false, err
	}

	ofn := openFileName{
		structSize: uint32(unsafe.Sizeof(openFileName{})),
		filter:     &filterPtr[0],
		file:       &buffer[0],
		maxFile:    uint32(len(buffer)),
		initialDir: initialDirPtr,
		title:      titlePtr,
		flags:      ofnExplorer | ofnFileMustExist | ofnPathMustExist,
	}

	ret, _, callErr := getOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ret == 0 {
		if callErr != syscall.Errno(0) {
			return "", false, callErr
		}
		return "", false, nil
	}

	return syscall.UTF16ToString(buffer), true, nil
}

func utf16DoubleNull(s string) []uint16 {
	encoded := syscall.StringToUTF16(s)
	if len(encoded) == 0 || encoded[len(encoded)-1] != 0 {
		encoded = append(encoded, 0)
	}
	return encoded
}

func ShowInfo(title string, body string) error {
	_, err := messageBox(title, body, msgOK|msgIconInfo)
	return err
}

func ShowError(title string, body string) error {
	_, err := messageBox(title, body, msgOK|msgIconError)
	return err
}

func AskYesNoCancel(title string, body string) (string, error) {
	result, err := messageBox(title, body, msgYesNoCancel|msgIconQuestion)
	if err != nil {
		return "", err
	}

	switch result {
	case choiceYes:
		return "yes", nil
	case choiceNo:
		return "no", nil
	default:
		return "cancel", nil
	}
}

func AskTryContinue(title string, body string) (string, error) {
	result, err := messageBox(title, body, msgCancelTryContinue|msgIconQuestion)
	if err != nil {
		return "", err
	}

	switch result {
	case choiceTryAgain:
		return "try", nil
	case choiceContinue:
		return "continue", nil
	default:
		return "cancel", nil
	}
}

func messageBox(title string, body string, flags uintptr) (uintptr, error) {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0, err
	}
	bodyPtr, err := syscall.UTF16PtrFromString(body)
	if err != nil {
		return 0, err
	}

	result, _, callErr := messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(bodyPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		flags,
	)
	if callErr != syscall.Errno(0) {
		return result, callErr
	}
	return result, nil
}
