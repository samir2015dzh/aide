//go:build !windows

package machine

import "path/filepath"

// realPath is path with every link in it followed.
func realPath(path string) (string, error) { return filepath.EvalSymlinks(path) }
