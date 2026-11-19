package machine

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// herPID is the her a machine of this test process runs: her.exe started by
// it. Her's lock can't tell, as on Unix: her holds the whole file locked
// (LockFileEx), so no other process reads it while she runs.
func herPID(t *testing.T, dir string) int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(snap)
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ParentProcessID == uint32(os.Getpid()) && windows.UTF16ToString(e.ExeFile[:]) == "her.exe" {
			return int(e.ProcessID)
		}
	}
	return 0
}

// running says whether the process pid is still there: Windows has no
// signal 0, so its handle is asked whether it has ended.
func running(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, 0)
	return err == nil && ev == uint32(windows.WAIT_TIMEOUT)
}
