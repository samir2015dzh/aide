//go:build !windows

package unlock

import "os"

// readFile: elsewhere a file read can be replaced or removed meanwhile.
func readFile(path string) ([]byte, error) { return os.ReadFile(path) }

func retry(f func() error) error { return f() }
