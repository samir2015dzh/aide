package resident

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procSetProcessWorkingSetSize = kernel32.NewProc("SetProcessWorkingSetSize")
	procGetProcessMemoryInfo     = kernel32.NewProc("K32GetProcessMemoryInfo")
)

// processMemoryCounters is PROCESS_MEMORY_COUNTERS (psapi.h).
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// trimWorkingSet asks Windows to take from pid's working set every page it
// can (SetProcessWorkingSetSize with -1, -1, as EmptyWorkingSet does): they
// go to the standby list, still in memory for the process to touch again at
// the cost of a soft fault, free for anything else that needs memory. It
// returns the working set before and after, in bytes.
func trimWorkingSet(pid int) (before, after uint64, err error) {
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		return 0, 0, err
	}
	defer windows.CloseHandle(h)
	before = workingSet(h)
	all := ^uintptr(0)
	if r, _, e := procSetProcessWorkingSetSize.Call(uintptr(h), all, all); r == 0 {
		return before, before, e
	}
	return before, workingSet(h), nil
}

func workingSet(h windows.Handle) uint64 {
	c := processMemoryCounters{cb: uint32(unsafe.Sizeof(processMemoryCounters{}))}
	if r, _, _ := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&c)), uintptr(c.cb)); r == 0 {
		return 0
	}
	return uint64(c.WorkingSetSize)
}

// descendants lists the processes under pid (its children, theirs, …), as a
// snapshot of the system's processes shows them now.
func descendants(pid int) []int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	children := map[uint32][]uint32{}
	e := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		children[e.ParentProcessID] = append(children[e.ParentProcessID], e.ProcessID)
	}
	var out []int
	seen := map[uint32]bool{uint32(pid): true}
	queue := []uint32{uint32(pid)}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, c := range children[p] {
			if !seen[c] {
				seen[c] = true
				out = append(out, int(c))
				queue = append(queue, c)
			}
		}
	}
	return out
}
