package machine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// updatingAgent is a CLI that updates itself: its version is the file
// version in dir; its updater, a script, prints a line on each stream,
// writes the version it was given and exits with the code in dir/exit.
type updatingAgent struct {
	name, dir string
}

func (a *updatingAgent) Name() string { return a.name }
func (a *updatingAgent) Discover(context.Context) provider.Info {
	b, _ := os.ReadFile(filepath.Join(a.dir, "version"))
	return provider.Info{Name: a.name, Available: true, LoggedIn: true, Version: strings.TrimSpace(string(b)), Binary: a.script()}
}
func (a *updatingAgent) Start(context.Context, provider.StartSpec) (provider.Session, error) {
	return nil, nil
}
func (a *updatingAgent) LatestRelease(context.Context) (string, error) { return "", nil }
func (a *updatingAgent) UpdateCommand() (string, []string)             { return a.script(), []string{"update"} }
func (a *updatingAgent) script() string                                { return filepath.Join(a.dir, "agent") }

// TestAgentUpdate (工单 #178): a built-in agent whose CLI updates itself
// says the newest release against its version — available, current, or
// unknown when the channel does not answer or the versions do not compare,
// never "current" then — its channel asked as it is checked on purpose and
// otherwise at most every releaseEvery. UpdateAgent runs its own updater,
// passes its output through, says a failure by its exit code, and after
// one that succeeded reads the new version back.
func TestAgentUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script stands in for the CLI")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "version"), []byte("2.1.300\n"), 0o600)
	os.WriteFile(filepath.Join(dir, "exit"), []byte("0"), 0o600)
	os.WriteFile(filepath.Join(dir, "agent"), []byte(`#!/bin/sh
d=$(dirname "$0")
echo "Checking for updates…"
echo "warning: a slow mirror" >&2
code=$(cat "$d/exit")
[ "$code" = 0 ] && echo 2.1.330 > "$d/version" && echo "Updated to 2.1.330"
exit "$code"
`), 0o755)
	a := &updatingAgent{name: "codex", dir: dir}
	var latest atomic.Value
	latest.Store("2.1.330")
	var asked atomic.Int32
	defer func(f func(context.Context, provider.Updater) (string, error)) { latestRelease = f }(latestRelease)
	latestRelease = func(context.Context, provider.Updater) (string, error) {
		asked.Add(1)
		v := latest.Load().(string)
		if v == "offline" {
			return "", errors.New("npm registry: dial tcp: no route to host")
		}
		return v, nil
	}
	n, err := Open(Config{DataDir: t.TempDir(), Providers: map[string]provider.Provider{"codex": a}})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	update := func() *machinev1.AgentUpdate { return agentRow(n.Status(), "codex").GetUpdate() }

	<-n.CheckAgents(true, 0)
	if u := update(); u.GetState() != machinev1.AgentUpdateState_AGENT_UPDATE_STATE_AVAILABLE || u.Latest != "2.1.330" {
		t.Fatalf("a newer release out: %+v", u)
	}
	<-n.CheckAgents(true, 0) // passive, asked within releaseEvery: not asked again
	if asked.Load() != 1 {
		t.Fatalf("channel asked %d times", asked.Load())
	}
	latest.Store("offline")
	<-n.CheckAgentsOnPurpose()
	if u := update(); u.GetState() != machinev1.AgentUpdateState_AGENT_UPDATE_STATE_UNKNOWN || !strings.Contains(u.Detail, "no route to host") {
		t.Fatalf("the channel down: %+v", u)
	}
	latest.Store("<html>")
	<-n.CheckAgentsOnPurpose()
	if u := update(); u.GetState() != machinev1.AgentUpdateState_AGENT_UPDATE_STATE_UNKNOWN || !strings.Contains(u.Detail, "unrecognized release") {
		t.Fatalf("an unreadable release: %+v", u)
	}
	latest.Store("2.1.330")
	<-n.CheckAgentsOnPurpose()

	// A failed update: its exit code said, its output passed through, the version as it was.
	os.WriteFile(filepath.Join(dir, "exit"), []byte("3"), 0o600)
	res, err := n.UpdateAgent(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if res.Succeeded || !strings.HasSuffix(res.Message, "update exited with code 3") || !strings.Contains(res.Output, "Checking for updates…") || !strings.Contains(res.Output, "warning: a slow mirror") || res.Version != "" {
		t.Fatalf("a failed update: %+v", res)
	}
	if u := update(); u.GetState() != machinev1.AgentUpdateState_AGENT_UPDATE_STATE_AVAILABLE {
		t.Fatalf("after a failed update: %+v", u)
	}

	// One that succeeds: the new version read back, and nothing newer out.
	os.WriteFile(filepath.Join(dir, "exit"), []byte("0"), 0o600)
	res, err = n.UpdateAgent(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Succeeded || res.Version != "2.1.330" || !strings.Contains(res.Output, "Updated to 2.1.330") || res.Command != a.script()+" update" {
		t.Fatalf("an update: %+v", res)
	}
	if row := agentRow(n.Status(), "codex"); row.Version != "2.1.330" || row.GetUpdate().GetState() != machinev1.AgentUpdateState_AGENT_UPDATE_STATE_CURRENT {
		t.Fatalf("after the update: %s %+v", row.Version, row.GetUpdate())
	}

	// An agent with no updater of its own: none run, and no update check.
	if _, err := n.UpdateAgent(context.Background(), "deepseek"); err == nil {
		t.Fatal("updated an agent with no updater")
	}
}
