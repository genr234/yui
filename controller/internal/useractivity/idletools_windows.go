//go:build windows

package useractivity

import (
	"log"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdll                = windows.NewLazySystemDLL("ntdll.dll")
	procNtSuspendProcess = ntdll.NewProc("NtSuspendProcess")
	procNtResumeProcess  = ntdll.NewProc("NtResumeProcess")
)

const processSuspendResume = 0x0800

// pausedTools are the processes this controller paused, so it resumes
// exactly those. Guarded by awake.mu (SetAwake is the only caller).
var pausedTools = map[uint32]string{}

// pauseIdleTools pauses the configured idle tools running in this session.
// Pausing, not ending: nothing changes for good, and they carry on where
// they were once resumed.
func pauseIdleTools() {
	for pid, name := range findIdleTools() {
		if _, done := pausedTools[pid]; done {
			continue
		}
		if err := suspendResume(pid, true); err != nil {
			log.Printf("user activity: pause %s (%d): %v", name, pid, err)
			continue
		}
		pausedTools[pid] = name
		log.Printf("user activity: paused idle tool %s (%d)", name, pid)
	}
}

func resumeIdleTools() {
	for pid, name := range pausedTools {
		if err := suspendResume(pid, false); err != nil {
			log.Printf("user activity: resume %s (%d): %v", name, pid, err)
		} else {
			log.Printf("user activity: resumed idle tool %s (%d)", name, pid)
		}
		delete(pausedTools, pid)
	}
}

// ResumeIdleTools resumes every configured idle tool. Run at start: a
// controller that crashed while holding a wake lock left them paused.
// Resuming one that isn't paused does nothing.
func ResumeIdleTools() {
	for pid := range findIdleTools() {
		_ = suspendResume(pid, false)
	}
}

func suspendResume(pid uint32, suspend bool) error {
	h, err := windows.OpenProcess(processSuspendResume, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	proc := procNtResumeProcess
	if suspend {
		proc = procNtSuspendProcess
	}
	if status, _, _ := proc.Call(uintptr(h)); status != 0 {
		return windows.NTStatus(status)
	}
	return nil
}

// findIdleTools lists the configured idle tools running in this session.
func findIdleTools() map[uint32]string {
	names := idleToolNames()
	found := map[uint32]string{}
	if len(names) == 0 {
		return found
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return found
	}
	defer windows.CloseHandle(snap)
	var mySession uint32
	_ = windows.ProcessIdToSessionId(uint32(os.Getpid()), &mySession)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		name := windows.UTF16ToString(entry.ExeFile[:])
		if !names[strings.ToLower(name)] {
			continue
		}
		var session uint32
		if windows.ProcessIdToSessionId(entry.ProcessID, &session) != nil || session != mySession {
			continue
		}
		found[entry.ProcessID] = name
	}
	return found
}
