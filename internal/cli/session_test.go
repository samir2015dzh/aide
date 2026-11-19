package cli

import (
	"testing"

	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
)

// A failure kind or status newer than this build (a newer machine's) reads
// "unknown", one of the documented names, never its number.
func TestNamesFromANewerMachine(t *testing.T) {
	if got := failureName(sessionv1.FailureKind_FAILURE_KIND_BASE_REF_UNAVAILABLE); got != "base_ref_unavailable" {
		t.Fatalf("known kind: %q", got)
	}
	if got := failureName(sessionv1.FailureKind(999)); got != "unknown" {
		t.Fatalf("newer kind: %q", got)
	}
	if got := statusName(sessionv1.SessionStatus(999)); got != "unknown" {
		t.Fatalf("newer status: %q", got)
	}
}

// A session in a GitHub repository runs on the machine named, else on the
// only one online; with several, the caller must choose.
func TestRepoMachine(t *testing.T) {
	a, b := machineRef{ID: "m_a", Name: "a", Online: true}, machineRef{ID: "m_b", Name: "b"}
	if m, err := repoMachine([]machineRef{a, b}, ""); err != nil || m.ID != "m_a" {
		t.Fatalf("the only one online: %+v, %v", m, err)
	}
	if m, err := repoMachine([]machineRef{a, b}, "b"); err != nil || m.ID != "m_b" {
		t.Fatalf("named: %+v, %v", m, err)
	}
	if m, err := repoMachine([]machineRef{b}, ""); err != nil || m.ID != "m_b" {
		t.Fatalf("the only one, offline: %+v, %v", m, err)
	}
	b.Online = true
	if _, err := repoMachine([]machineRef{a, b}, ""); exitCode(err) != exitUsage {
		t.Fatalf("several online: %v", err)
	}
	if _, err := repoMachine(nil, ""); exitCode(err) != exitUsage {
		t.Fatalf("none: %v", err)
	}
}
