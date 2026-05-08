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

	"github.com/ncruces/zenity"
)

var (
	shell32          = syscall.NewLazyDLL("shell32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	shellExecuteW    = shell32.NewProc("ShellExecuteW")
	openProcess      = kernel32.NewProc("OpenProcess")
	waitSingleObject = kernel32.NewProc("WaitForSingleObject")
	closeHandle      = kernel32.NewProc("CloseHandle")
)

const (
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

func selectFile(title string, initialDir string, _ string) (string, bool, error) {
	path, err := zenity.SelectFile(
		zenity.Title(title),
		zenity.Filename(initialDir),
		zenity.FileFilters{
			{Name: "Batch files", Patterns: []string{"*.bat"}},
			{Name: "All files", Patterns: []string{"*"}},
		},
	)
	if errors.Is(err, zenity.ErrCanceled) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return path, true, nil
}

func messageBox(title string, body string, icon uintptr) (uintptr, uintptr, error) {
	options := []zenity.Option{zenity.Title(title)}
	if icon&messageQuestion != 0 {
		if icon&messageYesNoCancel == messageYesNoCancel {
			err := zenity.Question(body,
				append(options,
					zenity.OKLabel("Yes"),
					zenity.ExtraButton("No"),
					zenity.CancelLabel("Cancel"),
					zenity.DefaultCancel(),
				)...,
			)
			switch {
			case err == nil:
				return dialogYes, 0, nil
			case errors.Is(err, zenity.ErrExtraButton):
				return dialogNo, 0, nil
			case errors.Is(err, zenity.ErrCanceled):
				return 0, 0, nil
			default:
				return 0, 0, err
			}
		}

		err := zenity.Question(body,
			append(options,
				zenity.OKLabel("Yes"),
				zenity.CancelLabel("No"),
				zenity.DefaultCancel(),
			)...,
		)
		if err == nil {
			return dialogYes, 0, nil
		}
		if errors.Is(err, zenity.ErrCanceled) {
			return dialogNo, 0, nil
		}
		return 0, 0, err
	}

	if icon&messageError != 0 {
		return 0, 0, zenity.Error(body, options...)
	}
	return 0, 0, zenity.Info(body, options...)
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
