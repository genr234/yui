//go:build windows

package native

import (
	"log"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// CloseLeftovers ends programs still running from runtimesDir. At controller
// start no session exists, so any such process was left behind by a previous
// run (an update or crash restarts the controller but not the program), and
// it would otherwise keep covering the screen with nobody able to close it.
// It returns how many it ended.
func CloseLeftovers(runtimesDir string) int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		log.Printf("native: leftover scan failed: %v", err)
		return 0
	}
	defer windows.CloseHandle(snap)

	self := uint32(os.Getpid())
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	ended := 0
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if entry.ProcessID == self || entry.ProcessID == 0 {
			continue
		}
		h, openErr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, entry.ProcessID)
		if openErr != nil {
			continue
		}
		buf := make([]uint16, windows.MAX_LONG_PATH)
		size := uint32(len(buf))
		if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) == nil {
			path := windows.UTF16ToString(buf[:size])
			if underDir(path, runtimesDir) {
				if termErr := windows.TerminateProcess(h, 1); termErr == nil {
					log.Printf("native: ended leftover program %s (pid %d)", path, entry.ProcessID)
					ended++
				} else {
					log.Printf("native: could not end leftover %s: %v", path, termErr)
				}
			}
		}
		windows.CloseHandle(h)
	}
	return ended
}
