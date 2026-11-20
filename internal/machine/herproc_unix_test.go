//go:build !windows

package machine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/samir2015dzh/aide/internal/paths"
)

// herPID is the her a machine of dir runs, as her's lock records it.
func herPID(t *testing.T, dir string) int {
	var rec struct {
		PID int `json:"pid"`
	}
	b, _ := os.ReadFile(filepath.Join(filepath.Dir(paths.Her(dir)), "her.lock"))
	json.Unmarshal(b, &rec)
	return rec.PID
}

// running says whether the process pid is still there.
func running(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.Signal(0)) == nil
}
