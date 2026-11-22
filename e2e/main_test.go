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
	// Every agent here was let in a moment before: none is new, but in
	// TestHeldForTheOwner, which holds a new one's messages as the owner
	// ruled (docs/agent-messages.md §15.5).
	os.Setenv("AIDE_TEST_NEW_AGENT_DAYS", "0")
	// The CLI's words are read in English here, whatever this computer's
	// language (work order #38); TestCLIInChinese asks for Chinese.
	os.Setenv("AIDE_LANG", "en")
	code := m.Run()
	cleanup()
	os.Exit(code)
}
