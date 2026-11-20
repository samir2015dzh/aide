package cli

import (
	"testing"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// A DSH session's id leads to its machine as a session's does, gives DSH's
// own id back whole (underscores and all), and is never a session's.
func TestNativeIDs(t *testing.T) {
	work, other := machineRef{ID: "m_k3j9x2abcdefgh", Name: "work"}, machineRef{ID: "m_zz9x2abcdefgh", Name: "other"}
	id := nativeID(work.ID, "ses_01_abc")
	if id != "dsh_k3j9x2ab_ses_01_abc" || !isNative(id) || isNative("s_k3j9x2ab_01abc") {
		t.Fatalf("id %q", id)
	}
	if got := dshID(id); got != "ses_01_abc" {
		t.Fatalf("DSH's id: %q", got)
	}
	if m, err := resolveMachine([]machineRef{other, work}, "", id); err != nil || m.Name != "work" {
		t.Fatalf("machine: %+v %v", m, err)
	}
	if err := readOnly(id); errorKind(err) != "invalid_state" || exitCode(err) != exitUsage {
		t.Fatalf("read-only: %v", err)
	}
}

// A DSH session's state reads as the contract spells it; one the machine
// can't tell, or from a newer machine, is unknown.
func TestNativeState(t *testing.T) {
	for st, want := range map[machinev1.DSHSessionState]string{
		machinev1.DSHSessionState_DSH_SESSION_STATE_RUNNING:     "running",
		machinev1.DSHSessionState_DSH_SESSION_STATE_IDLE:        "idle",
		machinev1.DSHSessionState_DSH_SESSION_STATE_ENDED:       "ended",
		machinev1.DSHSessionState_DSH_SESSION_STATE_UNSPECIFIED: "unknown",
		machinev1.DSHSessionState(99):                           "unknown",
	} {
		if got := nativeState(st); got != want {
			t.Errorf("%v: %q, want %q", st, got, want)
		}
	}
}
