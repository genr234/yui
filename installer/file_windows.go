//go:build windows

package main

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
	ofnFileMustExist   = 0x00001000
	ofnPathMustExist   = 0x00000800
	ofnExplorer        = 0x00080000
	messageOK          = 0x00000000
	messageYesNoCancel = 0x00000003
	messageError       = 0x00000010
	messageQuestion    = 0x00000020
	messageInfo        = 0x00000040
	dialogYes          = 6
	dialogNo           = 7
)

func selectFile(title string, initialDir string, filter string) (string, bool, error) {
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

func messageBox(title string, body string, icon uintptr) (uintptr, uintptr, error) {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0, 0, err
	}
	bodyPtr, err := syscall.UTF16PtrFromString(body)
	if err != nil {
		return 0, 0, err
	}

	ret, errno, callErr := messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(bodyPtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		icon,
	)
	return ret, errno, callErr
}

func defaultInitialDir() string {
	return `C:\`
}

func utf16DoubleNull(s string) []uint16 {
	encoded := syscall.StringToUTF16(s)
	if len(encoded) == 0 || encoded[len(encoded)-1] != 0 {
		encoded = append(encoded, 0)
	}
	return encoded
}
