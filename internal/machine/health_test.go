package machine

import (
	"runtime"
	"testing"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// A background update that went back is a problem in the status, naming
// the version it went back from (工单 #162).
func TestUpdateFailedIsAProblem(t *testing.T) {
	n := &Machine{cfg: Config{DataDir: t.TempDir(), UpdateFailed: "0.2.0"}}
	for _, p := range n.checkHealth() {
		if p.Kind == machinev1.MachineProblemKind_MACHINE_PROBLEM_KIND_UPDATE_FAILED && p.Version == "0.2.0" {
			return
		}
	}
	t.Error("no UPDATE_FAILED problem")
}

// The user's processes and their limit are read without starting one.
func TestUserProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows sets no limit on a user's processes")
	}
	pc, err := userProcesses()
	if err != nil {
		t.Fatal(err)
	}
	if pc.used == 0 {
		t.Fatalf("no processes counted: %+v", pc)
	}
	if pc.limit != 0 && pc.used > pc.limit*2 {
		t.Fatalf("more processes than the limit allows: %+v", pc)
	}
}

func TestNearLimit(t *testing.T) {
	for _, c := range []struct {
		used, limit uint64
		near        bool
	}{{2669, 2666, true}, {2400, 2666, true}, {2300, 2666, false}, {5, 0, false}} {
		if got := nearLimit(c.used, c.limit); got != c.near {
			t.Errorf("nearLimit(%d, %d) = %v", c.used, c.limit, got)
		}
	}
}
