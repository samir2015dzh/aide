package cli

import (
	"testing"

	"github.com/samir2015dzh/aide/internal/peer"
)

// TestCheckName: an agent uses a machine picked by name only if the
// machine presents that name on the end-to-end connection, so a relay that
// swaps the names of an owner's machines cannot misdirect work.
func TestCheckName(t *testing.T) {
	listed := machineRef{ID: "m_a", Name: "prod"}
	if err := checkName(listed, &peer.Machine{MachineID: "m_a", Name: "prod"}); err != nil {
		t.Fatalf("same name: %v", err)
	}
	if err := checkName(listed, &peer.Machine{MachineID: "m_a", Name: "staging"}); err == nil {
		t.Fatal("a machine calling itself staging was used as prod")
	}
	// Listed without a name (by id): nothing to hold it to.
	if err := checkName(machineRef{ID: "m_a", Name: "m_a"}, &peer.Machine{MachineID: "m_a", Name: "staging"}); err != nil {
		t.Fatalf("by id: %v", err)
	}
}

// A machine the relay knows no name for is called so in plain words, with
// the last four of its id, in the language of what a person reads; never
// its whole id (工单 #84). There is no name to hold it to either.
func TestUnnamedMachine(t *testing.T) {
	t.Setenv("AIDE_LANG", "zh-CN")
	if got := unnamedMachine("m_zxcz5wdiap4e56ys"); got != "未命名的机器 · 56ys" {
		t.Fatalf("zh: %q", got)
	}
	t.Setenv("AIDE_LANG", "en")
	if got := unnamedMachine("m_zxcz5wdiap4e56ys"); got != "Unnamed machine · 56ys" {
		t.Fatalf("en: %q", got)
	}
	ref := machineRef{ID: "m_zxcz5wdiap4e56ys", Name: unnamedMachine("m_zxcz5wdiap4e56ys")}
	if err := checkName(ref, &peer.Machine{MachineID: ref.ID, Name: "far-pc"}); err != nil {
		t.Fatalf("unnamed: %v", err)
	}
}
