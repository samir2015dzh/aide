package resident

import "unsafe"

var procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")

// memoryStatusEx is MEMORYSTATUSEX (sysinfoapi.h).
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func readMemory() (Memory, bool) {
	st := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&st))); r == 0 {
		return Memory{}, false
	}
	return Memory{Total: st.TotalPhys, Available: st.AvailPhys}, true
}
