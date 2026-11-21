package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// TestAgentPause: the owner pauses Claude Code on this Mac from the app, as
// when its plan's usage is used up. The app's list says it is paused; an
// agent starting a session with it is refused, with why; resumed, sessions
// start again at once. Paused until a time (a usage reset), it resumes by
// itself then, and the app shows when it did.
func TestAgentPause(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()
	status := func() (string, *machinev1.AgentStatus) {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(res.Machines) == 0 {
			return "", nil
		}
		for _, a := range res.Machines[0].Status.GetAgents() {
			if a.Name == "claude" {
				return res.Machines[0].MachineId, a
			}
		}
		return res.Machines[0].MachineId, nil
	}
	// Agents are checked when a page showing them asks, as the app's would.
	if _, err := w.app.client.CheckAgents(ctx, &localv1.CheckAgentsRequest{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "claude checked", func() bool { _, a := status(); return a != nil && !a.Checking })
	machineID, _ := status()
	pause := func(paused bool, until time.Time) {
		t.Helper()
		req := &localv1.SetAgentPausedRequest{MachineId: machineID, Agent: "claude", Paused: paused}
		if !until.IsZero() {
			req.ResumeAt = timestamppb.New(until)
		}
		if _, err := w.app.client.SetAgentPaused(ctx, req); err != nil {
			t.Fatal(err)
		}
	}

	pause(true, time.Time{})
	eventually(t, 10*time.Second, "the app lists claude paused", func() bool { _, a := status(); return a.GetPause() != nil && a.GetPause().ResumeAt == nil })
	if s, r := w.startSession(30*time.Second, "--prompt", "say hi"); r.code == 0 || s.SessionID != "" || !strings.Contains(r.stdout+r.stderr, "claude is paused on this machine by its owner") {
		t.Fatalf("start while paused: %+v", r)
	}

	pause(false, time.Time{})
	eventually(t, 10*time.Second, "the app lists claude resumed", func() bool { _, a := status(); return a.GetPause() == nil })
	if s, r := w.startSession(30*time.Second, "--prompt", "say hi"); s.SessionID == "" {
		t.Fatalf("start once resumed: %+v", r)
	}

	until := time.Now().Add(1500 * time.Millisecond)
	pause(true, until)
	if _, r := w.startSession(30*time.Second, "--prompt", "say hi"); r.code == 0 || !strings.Contains(r.stdout+r.stderr, "paused on this machine by its owner until") {
		t.Fatalf("start while paused until a time: %+v", r)
	}
	eventually(t, 20*time.Second, "claude resumed by itself, and the app says when", func() bool {
		_, a := status()
		return a.GetPause() == nil && a.GetAutoResumedAt() != nil && !a.GetAutoResumedAt().AsTime().Before(until.Add(-time.Second))
	})
	if s, r := w.startSession(30*time.Second, "--prompt", "say hi"); s.SessionID == "" {
		t.Fatalf("start once resumed by itself: %+v", r)
	}
}
