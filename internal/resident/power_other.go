//go:build !windows

package resident

// Power saving is Windows'; elsewhere the saver only says it does nothing.
const supported = false

func throttleSelf(on bool) error            { return nil }
func throttle(pid int, on, tree bool) error { return nil }
func priority() string                      { return "normal" }
func SetGroupPids(f func(pid int) []int)    {}
func SaveAlways() error                     { return nil }

func trimWorkingSet(pid int) (before, after uint64, err error) { return 0, 0, nil }
func descendants(pid int) []int                                { return nil }
