package resident

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const supported = true

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procSetProcessInformation = kernel32.NewProc("SetProcessInformation")
)

// SetProcessInformation's ProcessPowerThrottling (processthreadsapi.h):
// since Windows 10 1709. EXECUTION_SPEED is EcoQoS on Windows 11, where
// Task Manager shows it as efficiency mode.
const (
	processPowerThrottling   = 4
	throttlingVersion        = 1
	throttlingExecutionSpeed = 0x1
)

type powerThrottlingState struct {
	Version, ControlMask, StateMask uint32
}

// throttleHandle turns execution speed throttling on, or back to the
// system's own choice, and sets the priority class to match.
func throttleHandle(h windows.Handle, on bool) error {
	st := powerThrottlingState{Version: throttlingVersion}
	class := uint32(windows.NORMAL_PRIORITY_CLASS)
	if on {
		st.ControlMask, st.StateMask = throttlingExecutionSpeed, throttlingExecutionSpeed
		class = windows.BELOW_NORMAL_PRIORITY_CLASS
	}
	var errs []error
	if err := procSetProcessInformation.Find(); err != nil {
		errs = append(errs, err) // before 1709: priority only
	} else if r, _, err := procSetProcessInformation.Call(uintptr(h), processPowerThrottling, uintptr(unsafe.Pointer(&st)), unsafe.Sizeof(st)); r == 0 {
		errs = append(errs, fmt.Errorf("SetProcessInformation: %w", err))
	}
	if err := windows.SetPriorityClass(h, class); err != nil {
		errs = append(errs, fmt.Errorf("SetPriorityClass: %w", err))
	}
	return errors.Join(errs...)
}

func throttleSelf(on bool) error {
	return throttleHandle(windows.CurrentProcess(), on)
}

// throttle throttles the process pid or not; and, with tree, the processes
// of its job too (what it started while it ran below normal took that).
func throttle(pid int, on, tree bool) error {
	pids := []int{pid}
	if tree && groupPids != nil {
		pids = append(pids, groupPids(pid)...)
	}
	var errs []error
	for i, p := range pids {
		if i > 0 && p == pid {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p))
		if err != nil {
			if i == 0 {
				errs = append(errs, fmt.Errorf("process %d: %w", p, err))
			}
			continue // gone meanwhile
		}
		if err := throttleHandle(h, on); err != nil && i == 0 {
			errs = append(errs, err)
		}
		windows.CloseHandle(h)
	}
	return errors.Join(errs...)
}

// groupPids lists the processes in the job of the tree whose first
// process is pid (internal/provider runs each tree in a job of its own).
var groupPids func(pid int) []int

// SetGroupPids tells the saver how to find a tree's processes.
func SetGroupPids(f func(pid int) []int) { groupPids = f }

// priority is this process's priority class, as Task Manager names it.
func priority() string {
	c, err := windows.GetPriorityClass(windows.CurrentProcess())
	if err != nil {
		return "unknown"
	}
	return priorityName(c)
}

func priorityName(c uint32) string {
	switch c {
	case windows.IDLE_PRIORITY_CLASS:
		return "low"
	case windows.BELOW_NORMAL_PRIORITY_CLASS:
		return "below normal"
	case windows.NORMAL_PRIORITY_CLASS:
		return "normal"
	case windows.ABOVE_NORMAL_PRIORITY_CLASS:
		return "above normal"
	case windows.HIGH_PRIORITY_CLASS:
		return "high"
	case windows.REALTIME_PRIORITY_CLASS:
		return "realtime"
	}
	return fmt.Sprintf("%#x", c)
}

// SaveAlways throttles this process for good: the service's supervisor,
// which only waits on the daemon (and starts its daemon at normal
// priority, internal/service).
func SaveAlways() error { return throttleSelf(true) }
