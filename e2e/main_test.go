package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/samir2015dzh/aide/internal/provider/codex/codextest"
)

// Real Codex runs (AIDE_REAL_CLI, AIDE_CHAOS) must not write threads
// or trusted projects into the user's own Codex state.
func TestMain(m *testing.M) {
	cleanup, err := codextest.IsolateHome()
	if err != nil {
		fmt.Fprintln(os.Stderr, "isolating CODEX_HOME:", err)
		os.Exit(1)
	}
	// These tests' agents stand for agents on other computers, though they
	// run on this one beside the machines: they reach them through the
	// relay. TestLocalAgent turns the local way on for its own.
	os.Setenv("AIDE_LOCAL", "off")
	code := m.Run()
	cleanup()
	os.Exit(code)
}
