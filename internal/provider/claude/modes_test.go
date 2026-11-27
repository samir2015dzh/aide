package claude

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

// TestModes (工单 #180 C): Claude Code offers the three presets, none chosen
// being full access as its sessions always were here; the one chosen is what
// Aide's gate enforces on its tool calls, and SetMode changes it from the
// next call on.
func TestModes(t *testing.T) {
	scripted(t)
	if ids := modeIDs(provider.PresetModes()); !slices.Equal(ids, []string{"read-only", "workspace-write", "danger-full-access"}) || DefaultMode != "danger-full-access" {
		t.Fatalf("modes %v, default %s", ids, DefaultMode)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	out := filepath.Join(dir, "decisions")
	hook := func(id, tool, input string) string {
		return `echo '{"type":"control_request","request_id":"` + id + `","request":{"subtype":"hook_callback","callback_id":"gate","input":{"tool_name":"` + tool + `","tool_input":` + input + `,"tool_use_id":"` + id + `"}}}'` + "\nread r; echo \"$r\" >> " + out + "\n"
	}
	script := "#!/bin/sh\nread init\nread prompt\n" +
		hook("a", "Bash", `{"command":"ls"}`) + hook("b", "Write", `{"file_path":"`+filepath.Join(dir, "x.txt")+`"}`) + hook("c", "Read", `{"file_path":"`+filepath.Join(dir, "x.txt")+`"}`) +
		"sleep 1\n" + hook("d", "Bash", `{"command":"ls"}`) +
		`echo '{"type":"result","subtype":"success","result":"ok","usage":{"input_tokens":1,"output_tokens":1}}'` + "\nsleep 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(mode, then string) []string {
		t.Helper()
		os.Remove(out)
		s, err := (&Provider{Binary: bin}).Start(context.Background(), provider.StartSpec{WorkDir: dir, Prompt: "hi", Mode: mode, Env: os.Environ()})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if then != "" {
			time.Sleep(300 * time.Millisecond) // after a–c, before d
			if err := s.(provider.ModeSetter).SetMode(context.Background(), then); err != nil {
				t.Fatal(err)
			}
		}
		for {
			select {
			case e := <-s.Events():
				if e.Kind == provider.EventTurnCompleted {
					b, _ := os.ReadFile(out)
					var ds []string
					for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
						ds = append(ds, map[bool]string{true: "deny", false: "allow"}[strings.Contains(l, `"deny"`)])
					}
					return ds
				}
			case <-time.After(10 * time.Second):
				t.Fatal("timed out")
			}
		}
	}
	for _, c := range []struct {
		mode, then string
		want       []string // Bash, Write in the workspace, Read, Bash after SetMode
	}{
		{"", "", []string{"allow", "allow", "allow", "allow"}},
		{"danger-full-access", "", []string{"allow", "allow", "allow", "allow"}},
		{"workspace-write", "", []string{"deny", "allow", "allow", "deny"}},
		{"read-only", "", []string{"deny", "deny", "allow", "deny"}},
		{"read-only", "danger-full-access", []string{"deny", "deny", "allow", "allow"}},
	} {
		if got := run(c.mode, c.then); !slices.Equal(got, c.want) {
			t.Errorf("mode %q then %q: %v, want %v", c.mode, c.then, got, c.want)
		}
	}
	if _, err := (&Provider{Binary: bin}).Start(context.Background(), provider.StartSpec{WorkDir: dir, Prompt: "hi", Mode: "plan"}); err == nil {
		t.Fatal("a mode it does not offer must be refused")
	}
}

func modeIDs(ms []provider.Mode) (ids []string) {
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	return ids
}
