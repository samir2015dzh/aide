package machine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// Agent CLI updates (工单 #178, after paseo's release-check.ts and
// update-command.ts, Apache License 2.0). A built-in agent whose CLI
// updates itself (provider.Updater) has its release channel asked as it is
// checked — on purpose (Check Again), else at most every releaseEvery — and
// its status says the newest release against the version here, unknown
// when that cannot be told. UpdateAgent runs the CLI's own updater, its
// output passed through, and checks the agent again: Aide never replaces
// the program itself (Antigravity's ACP server keeps its registry install).

// releaseEvery is how often a release channel is asked by a check not on
// purpose: releases come days apart, and a check runs every few minutes.
const releaseEvery = 6 * time.Hour

// updateTimeout bounds an updater: one that downloads a new version over a
// slow line still ends in time.
const updateTimeout = 10 * time.Minute

// updateOutputMax is how much of an updater's output is kept: its end.
const updateOutputMax = 64 << 10

// releaseRead is what an agent's release channel said when last asked.
type releaseRead struct {
	latest string
	err    string
	at     time.Time
}

// latestRelease asks u's channel; tests stand in for it.
var latestRelease = func(ctx context.Context, u provider.Updater) (string, error) { return u.LatestRelease(ctx) }

// readRelease asks name's release channel, when its CLI updates itself and
// it was found, unless asked within releaseEvery by a check not on purpose;
// it reports whether it asked.
func (n *Machine) readRelease(name string, p provider.Provider, info provider.Info, onPurpose bool) bool {
	u, ok := p.(provider.Updater)
	if !ok || !info.Available {
		return false
	}
	n.mu.Lock()
	r, had := n.releases[name]
	n.mu.Unlock()
	if had && !onPurpose && time.Since(r.at) < releaseEvery {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	latest, err := latestRelease(ctx, u)
	r = releaseRead{latest: latest, at: time.Now()}
	if err != nil {
		r = releaseRead{err: err.Error(), at: r.at}
	}
	n.mu.Lock()
	n.releases[name] = r
	n.mu.Unlock()
	return true
}

// updateOfLocked is name's update check against version, nil when its
// channel was never asked. The caller holds n.mu.
func (n *Machine) updateOfLocked(name, version string) *machinev1.AgentUpdate {
	r, ok := n.releases[name]
	if !ok {
		return nil
	}
	u := &machinev1.AgentUpdate{Latest: r.latest, CheckedAt: timestamppb.New(r.at)}
	if r.err != "" {
		u.State, u.Detail = machinev1.AgentUpdateState_AGENT_UPDATE_STATE_UNKNOWN, r.err
		return u
	}
	switch c := provider.CompareRelease(version, r.latest); c.State {
	case provider.UpdateAvailable:
		u.State = machinev1.AgentUpdateState_AGENT_UPDATE_STATE_AVAILABLE
	case provider.UpdateCurrent:
		u.State = machinev1.AgentUpdateState_AGENT_UPDATE_STATE_CURRENT
	default:
		u.State, u.Detail = machinev1.AgentUpdateState_AGENT_UPDATE_STATE_UNKNOWN, c.Message
	}
	return u
}

// UpdateAgent runs name's CLI's own updater and, when it exits 0, checks
// the agents again on purpose, so the version it reads back is the new one
// and the status says whether more is out.
func (n *Machine) UpdateAgent(ctx context.Context, name string) (*machinev1.UpdateAgentResponse, error) {
	u, ok := n.provider(name).(provider.Updater)
	if !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("%s has no updater of its own for Aide to run", name))
	}
	bin, args := u.UpdateCommand()
	if bin == "" || !provider.Exists(bin) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("%s is not installed on this machine", name))
	}
	n.mu.Lock()
	if n.updating[name] {
		n.mu.Unlock()
		return nil, connect.NewError(connect.CodeAborted, fmt.Errorf("%s is being updated already", name))
	}
	n.updating[name] = true
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		delete(n.updating, name)
		n.mu.Unlock()
	}()

	line := strings.Join(append([]string{bin}, args...), " ")
	run, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()
	cmd := provider.Command(run, bin, args...)
	cmd.Env = provider.ProbeEnv("")
	var out bytes.Buffer // one writer for both: it keeps the order they were printed in
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	res := &machinev1.UpdateAgentResponse{Command: line, Output: tail(out.String(), updateOutputMax)}
	var exit *exec.ExitError
	switch {
	case err == nil:
		res.Succeeded = true
	case errors.Is(run.Err(), context.DeadlineExceeded):
		res.Message = fmt.Sprintf("%s stopped after %s", line, updateTimeout)
	case errors.As(err, &exit) && exit.ExitCode() >= 0:
		res.Message = fmt.Sprintf("%s exited with code %d", line, exit.ExitCode())
	default:
		res.Message = fmt.Sprintf("%s: %v", line, err)
	}
	n.audit.Log(AuditRecord{Kind: "agent", Subject: name, Action: "update_cli", Detail: strings.TrimSpace(line + " · " + map[bool]string{true: "succeeded", false: res.Message}[res.Succeeded])})
	if res.Succeeded {
		<-n.CheckAgentsOnPurpose()
		res.Version = n.providerInfo(name).Version
	}
	return res, nil
}

// tail is the end of s, at most max bytes, cut at a line.
func tail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[len(s)-max:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return "…\n" + s
}
