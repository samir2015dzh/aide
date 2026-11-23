package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fixtureRun(t *testing.T, name string) func(context.Context, string, []string) ([]byte, error) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "cliusage", name))
	if err != nil {
		t.Fatal(err)
	}
	return func(context.Context, string, []string) ([]byte, error) { return b, nil }
}

// Claude Code's /usage, asked without a terminal (工单 #86): the plan's rows
// from its structured twin, named as the usage endpoint names its windows,
// a row of a kind unknown here left out, unknown fields no harm; its text
// read when a version writes no twin; told apart from a version that took
// /usage as a prompt, from an answer without a plan, and from not running.
func TestCLIUsage(t *testing.T) {
	at := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	read := func(run func(context.Context, string, []string) ([]byte, error)) Usage {
		return (&CLIUsage{Run: run, Now: func() time.Time { return at }}).Read(context.Background(), "claude")
	}

	u := read(fixtureRun(t, "structured.jsonl"))
	if u.Unavailable != "" || !u.FetchedAt.Equal(at) {
		t.Fatalf("structured: %+v", u)
	}
	want := map[string]float64{"five_hour": 20.4, "seven_day": 59, "seven_day_sonnet": 3}
	if len(u.Windows) != len(want) || u.Windows[0].Key != "five_hour" || u.Windows[1].Key != "seven_day" {
		t.Fatalf("structured windows = %+v", u.Windows)
	}
	for _, w := range u.Windows {
		if want[w.Key] != w.Utilization || w.ResetsAt.IsZero() {
			t.Fatalf("structured window %+v", w)
		}
	}
	if got := u.Windows[0].ResetsAt; !got.Equal(time.Date(2026, 11, 20, 20, 39, 59, 735515000, time.UTC)) {
		t.Fatalf("five_hour resets_at = %v", got)
	}

	u = read(fixtureRun(t, "text.jsonl"))
	if u.Unavailable != "" || len(u.Windows) != 3 || u.Windows[0].Key != "five_hour" || u.Windows[0].Utilization != 7 || u.Windows[1].Utilization != 41 || u.Windows[2].Key != "seven_day_opus" {
		t.Fatalf("text: %+v", u)
	}

	if u = read(fixtureRun(t, "noplan.jsonl")); u.Unavailable != CLINoReading || len(u.Windows) != 0 {
		t.Fatalf("no plan: %+v", u)
	}
	// A model's answer that happens to look like a usage row is not one.
	if u = read(fixtureRun(t, "prompt.jsonl")); u.Unavailable != CLIUnsupported || len(u.Windows) != 0 {
		t.Fatalf("prompt: %+v", u)
	}
	u = read(func(context.Context, string, []string) ([]byte, error) { return nil, errors.New("exit status 1") })
	if u.Unavailable != CLIFailed || !strings.Contains(u.Detail, "exit status 1") {
		t.Fatalf("failed: %+v", u)
	}
}

// End to end with a stand-in Claude Code: the real command is run, with the
// arguments that ask /usage without a terminal and keep no session.
func TestCLIUsageCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in is a shell script")
	}
	dir := t.TempDir()
	fixture, err := filepath.Abs(filepath.Join("testdata", "cliusage", "structured.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "claude")
	script := "#!/bin/sh\n[ \"$*\" = \"" + strings.Join(CLIUsageArgs, " ") + "\" ] || { echo \"unexpected: $*\" >&2; exit 2; }\ncat '" + fixture + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	u := (&CLIUsage{}).Read(context.Background(), bin)
	if u.Unavailable != "" || len(u.Windows) != 3 {
		t.Fatalf("stand-in Claude Code: %+v", u)
	}
}
