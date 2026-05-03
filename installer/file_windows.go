//go:build windows

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	comdlg32         = syscall.NewLazyDLL("comdlg32.dll")
	shell32          = syscall.NewLazyDLL("shell32.dll")
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	getOpenFileNameW = comdlg32.NewProc("GetOpenFileNameW")
	messageBoxW      = user32.NewProc("MessageBoxW")
	shellExecuteW    = shell32.NewProc("ShellExecuteW")
	openProcess      = kernel32.NewProc("OpenProcess")
	waitSingleObject = kernel32.NewProc("WaitForSingleObject")
	closeHandle      = kernel32.NewProc("CloseHandle")
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
	messageYesNo       = 0x00000004
	messageYesNoCancel = 0x00000003
	messageError       = 0x00000010
	messageQuestion    = 0x00000020
	messageInfo        = 0x00000040
	dialogYes          = 6
	dialogNo           = 7
	synchronize        = 0x00100000
	waitTimeout        = 0x00000102
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

func relaunchElevatedIfNeeded(mode installMode, target string) (bool, error) {
	if hasElevationMarker() {
		return false, nil
	}

	if err := checkTargetWritable(target); err == nil {
		return false, nil
	} else if !isPermissionError(err) {
		log.Printf("target write preflight failed without permission error: %v", err)
		return false, nil
	}

	args := []string{"--elevated"}
	if autoUpdateMode {
		args = append(args, "--auto-update")
		if parentPID > 0 {
			args = append(args, "--parent-pid", fmt.Sprintf("%d", parentPID))
		}
	}
	if mode == modeRestore {
		args = append(args, "--restore")
	}
	args = append(args, target)

	log.Printf("target requires elevation; relaunching installer with UAC")
	if err := shellExecuteRunas(args); err != nil {
		return false, err
	}

	return true, nil
}

func checkTargetWritable(target string) error {
	target = filepath.Clean(target)
	targetDir := filepath.Dir(target)
	probePath := filepath.Join(targetDir, fmt.Sprintf(".yui-write-test-%d.tmp", os.Getpid()))

	if err := os.WriteFile(probePath, []byte("test"), 0644); err != nil {
		return err
	}
	_ = os.Remove(probePath)

	file, err := os.OpenFile(target, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return file.Close()
}

func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return isPermissionError(pathErr.Err)
	}
	return false
}

func shellExecuteRunas(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find installer executable: %w", err)
	}

	verbPtr, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	exePtr, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	paramsPtr, err := syscall.UTF16PtrFromString(joinWindowsArgs(args))
	if err != nil {
		return err
	}
	dirPtr, err := syscall.UTF16PtrFromString(filepath.Dir(exe))
	if err != nil {
		return err
	}

	ret, _, callErr := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(exePtr)),
		uintptr(unsafe.Pointer(paramsPtr)),
		uintptr(unsafe.Pointer(dirPtr)),
		1,
	)
	if ret <= 32 {
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return fmt.Errorf("ShellExecute runas failed with code %d", ret)
	}

	return nil
}

func waitForParentExit(pid int, timeout time.Duration) {
	if pid <= 0 {
		return
	}

	handle, _, _ := openProcess.Call(synchronize, 0, uintptr(uint32(pid)))
	if handle == 0 {
		log.Printf("auto update parent process %d was not found", pid)
		return
	}
	defer closeHandle.Call(handle)

	millis := uint32(timeout / time.Millisecond)
	result, _, _ := waitSingleObject.Call(handle, uintptr(millis))
	if result == waitTimeout {
		log.Printf("auto update parent process %d did not exit before timeout", pid)
	}
}

func joinWindowsArgs(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, quoteWindowsArg(arg))
	}
	return strings.Join(quoted, " ")
}

func quoteWindowsArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}

	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for _, r := range arg {
		switch r {
		case '\\':
			backslashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, backslashes*2+1))
			b.WriteRune(r)
			backslashes = 0
		default:
			if backslashes > 0 {
				b.WriteString(strings.Repeat(`\`, backslashes))
				backslashes = 0
			}
			b.WriteRune(r)
		}
	}
	if backslashes > 0 {
		b.WriteString(strings.Repeat(`\`, backslashes*2))
	}
	b.WriteByte('"')
	return b.String()
}

func utf16DoubleNull(s string) []uint16 {
	encoded := syscall.StringToUTF16(s)
	if len(encoded) == 0 || encoded[len(encoded)-1] != 0 {
		encoded = append(encoded, 0)
	}
	return encoded
}
