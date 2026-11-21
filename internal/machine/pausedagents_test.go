package machine

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// The owner pauses an agent on the machine: its status says so, no new
// session starts with it (and the refusal says why), while a session under
// way goes on taking messages; the pause outlives a restart; resumed, new
// sessions start at once. Paused until a time, it resumes by itself then and
// the status keeps when. A time already past, or an agent the machine doesn't
// know, is refused.
func TestPauseAgent(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	n := h.n
	ctx := context.Background()
	set := func(paused bool, resumeAt *timestamppb.Timestamp, agent ...string) error {
		name := "fake"
		if len(agent) > 0 {
			name = agent[0]
		}
		_, err := n.execAdmin(ctx, &identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetAgentPaused{SetAgentPaused: &identityv1.SetAgentPaused{Agent: name, Paused: paused, ResumeAt: resumeAt}}})
		return err
	}
	status := func() *machinev1.AgentStatus {
		t.Helper()
		for _, a := range n.Status().Agents {
			if a.Name == "fake" {
				return a
			}
		}
		t.Fatal("fake not listed")
		return nil
	}
	startErr := func() error {
		sess, err := h.s.Start(ctx, h.p, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "fake", Prompt: "hello"})
		if err == nil {
			// Its turn done before the test goes on (or ends): its worktree
			// written, nothing left writing under the test's folders.
			h.wait(sess.SessionId, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
		}
		return err
	}

	under := h.start("one under way")
	h.wait(under, sessionv1.SessionStatus_SESSION_STATUS_IDLE)

	if err := set(true, nil); err != nil {
		t.Fatal(err)
	}
	if p := status().Pause; p == nil || p.Since == nil || p.ResumeAt != nil {
		t.Fatalf("pause = %v, want since and no resume_at", p)
	}
	if err := startErr(); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("start while paused: %v, want failed_precondition", err)
	}
	h.send(under, "still here") // a session under way runs on
	h.wait(under, sessionv1.SessionStatus_SESSION_STATUS_IDLE)

	n.stopResumeTimers()
	n.loadPausedAgents() // a restart reads what was kept
	if status().Pause == nil {
		t.Fatal("resumed by a restart")
	}

	if err := set(false, nil); err != nil {
		t.Fatal(err)
	}
	if status().Pause != nil {
		t.Fatal("still paused once resumed")
	}
	if err := startErr(); err != nil {
		t.Fatalf("start once resumed: %v", err)
	}

	if err := set(true, timestamppb.New(time.Now().Add(-time.Minute))); err == nil {
		t.Fatal("paused until a time already past")
	}
	until := time.Now().Add(300 * time.Millisecond)
	if err := set(true, timestamppb.New(until)); err != nil {
		t.Fatal(err)
	}
	if p := status().Pause; p == nil || p.ResumeAt.AsTime().Sub(until).Abs() > time.Millisecond {
		t.Fatalf("pause = %v, want resume_at %v", p, until)
	}
	if err := startErr(); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("start while paused until a time: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for status().Pause != nil {
		if time.Now().After(deadline) {
			t.Fatal("not resumed by itself")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if a := status(); a.AutoResumedAt == nil || a.AutoResumedAt.AsTime().Before(until.Add(-time.Second)) {
		t.Fatalf("auto_resumed_at = %v, want about %v", a.AutoResumedAt, until)
	}
	if err := startErr(); err != nil {
		t.Fatalf("start once resumed by itself: %v", err)
	}

	if err := set(true, nil, "no-such-agent"); err == nil {
		t.Fatal("paused an agent the machine doesn't know")
	}
}
