package machine

import (
	"context"
	"slices"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// firstWatch is the first message of a Watch with req.
func (h *harness) firstWatch(req *machinev1.WatchRequest) *machinev1.WatchResponse {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan *machinev1.WatchResponse, 1)
	go h.n.Watch(ctx, &Principal{Kind: PrincipalOwner}, req, func(m *machinev1.WatchResponse) error {
		select {
		case got <- m:
		default:
		}
		return nil
	})
	select {
	case m := <-got:
		return m
	case <-time.After(5 * time.Second):
		h.t.Fatal("no first message")
		return nil
	}
}

func ids(list []*sessionv1.Session) []string {
	var out []string
	for _, s := range list {
		out = append(out, s.SessionId)
	}
	slices.Sort(out)
	return out
}

// A watcher that reconnects gets only what changed since its token, a
// deletion included, and nothing when nothing changed; after this machine
// started again its token is refused, and the fresh start says what was
// deleted since the watcher's last message.
func TestWatchResumes(t *testing.T) {
	h := newHarness(t, Limits{MaxConcurrent: 4})
	first := h.start("say one")
	h.wait(first, sessionv1.SessionStatus_SESSION_STATUS_IDLE)

	fresh := h.firstWatch(&machinev1.WatchRequest{View: sessionv1.SessionView_SESSION_VIEW_BASIC})
	if !fresh.Reset_ || !slices.Equal(ids(fresh.Sessions), []string{first}) || fresh.Status == nil || len(fresh.Projects) != 1 || len(fresh.ResumeToken) != 16 {
		t.Fatalf("fresh start: %v", fresh)
	}
	if fresh.Sessions[0].Result != "" {
		t.Error("BASIC carries the reply")
	}

	second := h.start("say two")
	h.wait(second, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	if err := h.s.Delete(context.Background(), &Principal{Kind: PrincipalOwner}, first); err != nil {
		t.Fatal(err)
	}
	resumed := h.firstWatch(&machinev1.WatchRequest{ResumeToken: fresh.ResumeToken, HeldAt: fresh.SentAt})
	if resumed.Reset_ || !slices.Equal(ids(resumed.Sessions), []string{second}) || !slices.Equal(resumed.DeletedSessionIds, []string{first}) {
		t.Fatalf("resumed: reset %v, sessions %v, deleted %v", resumed.Reset_, ids(resumed.Sessions), resumed.DeletedSessionIds)
	}
	again := h.firstWatch(&machinev1.WatchRequest{ResumeToken: resumed.ResumeToken})
	if again.Reset_ || len(again.Sessions) != 0 || len(again.DeletedSessionIds) != 0 {
		t.Fatalf("resumed with nothing changed: %v", again)
	}

	h.s.changeLog = newChangeLog() // as after aided started again
	restarted := h.firstWatch(&machinev1.WatchRequest{ResumeToken: again.ResumeToken, HeldAt: fresh.SentAt})
	if !restarted.Reset_ || !slices.Equal(ids(restarted.Sessions), []string{second}) || !slices.Equal(restarted.DeletedSessionIds, []string{first}) {
		t.Fatalf("after a restart: reset %v, sessions %v, deleted %v", restarted.Reset_, ids(restarted.Sessions), restarted.DeletedSessionIds)
	}
}

// ReportStatus carries only the machine's name and version (工单 #143): a
// session's run, hiding an agent and a health check do not send it again,
// a rename does; the owner sees the hidden agent in Watch's status all the
// same, its one source for the machine's state.
func TestOnlyARenameKicksRelayStatus(t *testing.T) {
	h := newHarness(t, Limits{MaxConcurrent: 2})
	drained := func() bool {
		select {
		case <-h.n.statusKick:
			return true
		default:
			return false
		}
	}
	drained() // one left from opening the machine, if any

	sid := h.start("hello")
	h.wait(sid, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	if drained() {
		t.Fatal("a session's run kicked ReportStatus")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	statuses := make(chan *machinev1.MachineStatus, 16)
	go h.n.Watch(ctx, &Principal{Kind: PrincipalOwner}, &machinev1.WatchRequest{}, func(m *machinev1.WatchResponse) error {
		if m.Status != nil {
			statuses <- m.Status
		}
		return nil
	})
	hidden := func(st *machinev1.MachineStatus) bool {
		for _, a := range st.Agents {
			if a.Name == "fake" {
				return a.Hidden
			}
		}
		return false
	}
	select {
	case st := <-statuses:
		if hidden(st) {
			t.Fatal("fake hidden before it was hidden")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no first status")
	}
	if _, err := h.n.execAdmin(context.Background(), &identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetAgentHidden{SetAgentHidden: &identityv1.SetAgentHidden{Agent: "fake", Hidden: true}}}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for seen := false; !seen; {
		select {
		case st := <-statuses:
			seen = hidden(st)
		case <-deadline:
			t.Fatal("Watch did not send the hidden agent")
		}
	}
	h.n.refreshHealth()
	if drained() {
		t.Fatal("hiding an agent or a health check kicked ReportStatus")
	}

	if _, err := h.n.execAdmin(context.Background(), &identityv1.AdminCommand{Command: &identityv1.AdminCommand_SetName{SetName: &identityv1.SetName{Name: "renamed"}}}); err != nil {
		t.Fatal(err)
	}
	if !drained() {
		t.Fatal("a rename did not kick ReportStatus")
	}
}
