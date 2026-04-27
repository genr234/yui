//go:build windows

package singleinstance

import (
	"syscall"
	"unsafe"
)

const errorAlreadyExists syscall.Errno = 183

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	createMutexW = kernel32.NewProc("CreateMutexW")
	releaseMutex = kernel32.NewProc("ReleaseMutex")
	closeHandle  = kernel32.NewProc("CloseHandle")
)

type Lock struct {
	handle uintptr
}

func Acquire(name string) (*Lock, bool, error) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, false, err
	}

	handle, _, callErr := createMutexW.Call(
		0,
		1,
		uintptr(unsafe.Pointer(namePtr)),
	)
	if handle == 0 {
		return nil, false, callErr
	}
	if callErr == errorAlreadyExists {
		closeHandle.Call(handle)
		return nil, true, nil
	}

	return &Lock{handle: handle}, false, nil
}

func (l *Lock) Release() {
	if l == nil || l.handle == 0 {
		return
	}
	releaseMutex.Call(l.handle)
	closeHandle.Call(l.handle)
	l.handle = 0
}
