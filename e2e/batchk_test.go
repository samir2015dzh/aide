package e2e

import (
	"path/filepath"
	"testing"
)

// addressLoginClaude is a stand-in Claude Code for 工单 #118 (fakeclaude),
// keeping its sign-in in dir: its `auth login` prints the address to open
// and takes the code back. The env is what aided needs to run it.
func addressLoginClaude(t *testing.T) (env []string, dir string) {
	dir = t.TempDir()
	return []string{"AIDE_CLAUDE_BIN=" + filepath.Join(binaries(t), "fakeclaude"+exe), "FAKE_CLAUDE_DIR=" + dir}, dir
}
