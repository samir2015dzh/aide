package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestHeldForTheOwner (docs/agent-messages.md §15.5): as the owner ruled, a
// message from an agent let in less than 7 days ago, to a session another
// started, waits on its machine for the owner: listed in the app, released
// to the session, or discarded. And the owner's information boundary,
// written on their device and published by it, refuses a message that
// matches it before it goes, naming the rule's category only.
func TestHeldForTheOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	// The owner's ruling as it stands: 7 days (TestMain makes no agent new).
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1", "AIDE_TEST_NEW_AGENT_DAYS="}, &localv1.GrantSpec{})
	ctx := context.Background()
	s, r := w.startSession(30*time.Second, "--prompt", "say mine")
	if s.SessionID == "" {
		t.Fatalf("start: %+v", r)
	}
	w.waitStatus(s.SessionID, "idle")
	// The bot's own session takes its words as they are, new as it is.
	var own struct {
		Message struct{ Delivered string }
	}
	if r := w.cli(30*time.Second, "message", "send", "--to", s.SessionID, "--message", "say still mine", "-o", "json"); r.code != 0 || json.Unmarshal([]byte(r.stdout), &own) != nil || own.Message.Delivered != "turn" {
		t.Fatalf("to its own session: %+v", r)
	}
	w.waitStatus(s.SessionID, "idle")

	helper := w.addAgent("helper", &localv1.GrantSpec{TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_ALL})
	send := func(text string) result {
		return run(t, append(append([]string{}, w.cliEnv...), helper...), time.Minute, filepath.Join(w.bin, "aide"), "message", "send", "-o", "json", "--to", s.SessionID, "--message", text)
	}
	held := func() []*localv1.HeldMessageEntry {
		l, err := w.app.client.ListHeldMessages(ctx, &localv1.ListHeldMessagesRequest{})
		if err != nil {
			t.Fatal(err)
		}
		return l.Messages
	}
	var sent struct {
		Message struct{ ID, Delivered string }
	}
	r = send("say released")
	if json.Unmarshal([]byte(r.stdout), &sent); r.code != 0 || sent.Message.Delivered != "held" {
		t.Fatalf("a new agent to another's session: %+v", r)
	}
	h := held()
	if len(h) != 1 || h[0].Message.MessageId != sent.Message.ID || h[0].Message.Reason != "new_agent" || h[0].Message.FromName != "helper" || h[0].Message.SessionId != s.SessionID {
		t.Fatalf("held: %v", h)
	}
	if w.get(s.SessionID).Result == "released" {
		t.Fatal("reached the session while held")
	}
	if _, err := w.app.client.ReleaseHeldMessage(ctx, &localv1.ReleaseHeldMessageRequest{MachineId: h[0].MachineId, MessageId: sent.Message.ID, Release: true}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "the released message's turn", func() bool { return w.get(s.SessionID).Result == "released" })
	w.waitStatus(s.SessionID, "idle")

	r = send("say discarded")
	json.Unmarshal([]byte(r.stdout), &sent)
	if h = held(); len(h) != 1 {
		t.Fatalf("held: %v", h)
	}
	if _, err := w.app.client.ReleaseHeldMessage(ctx, &localv1.ReleaseHeldMessageRequest{MachineId: h[0].MachineId, MessageId: sent.Message.ID}); err != nil {
		t.Fatal(err)
	}
	if h = held(); len(h) != 0 {
		t.Fatalf("held once discarded: %v", h)
	}
	time.Sleep(time.Second)
	if res := w.get(s.SessionID).Result; res != "released" {
		t.Fatalf("a discarded message reached the session: %q", res)
	}

	// The owner's information boundary: written on their device (placeholder
	// rule), published when the owner's agents change, refused before it goes.
	cur, _ := os.ReadFile(filepath.Join(w.tmp, "owner-mac", "owner", "current"))
	rules := `{"rules": [{"category": "example", "patterns": ["placeholder-secret"]}]}`
	if err := os.WriteFile(filepath.Join(w.tmp, "owner-mac", "owner", "profiles", string(cur), "boundary.json"), []byte(rules), 0o600); err != nil {
		t.Fatal(err)
	}
	w.addAgent("third", &localv1.GrantSpec{})
	var refused result
	eventually(t, time.Minute, "the boundary published", func() bool {
		refused = w.cli(30*time.Second, "message", "send", "--to", s.SessionID, "--message", "say placeholder-secret", "-o", "json")
		return refused.code != 0
	})
	var doc struct {
		Error struct{ Kind, Message string }
	}
	if json.Unmarshal([]byte(refused.stdout), &doc); doc.Error.Kind != "refused" || !strings.Contains(doc.Error.Message, "boundary (example)") || strings.Contains(doc.Error.Message, "placeholder-secret") {
		t.Fatalf("refused: %+v", refused)
	}
	if r := w.cli(30*time.Second, "message", "send", "--to", s.SessionID, "--message", "say fine", "-o", "json"); r.code != 0 {
		t.Fatalf("a message the boundary lets through: %+v", r)
	}
}

// heldMessage has a new agent write to a session the fixture's agent
// started: held for the owner, for held.spec.ts to release.
func heldMessage(t *testing.T, f desktopFixture) {
	t.Helper()
	w := f.owner
	s := firstSession(w.cli(2*time.Minute, "session", "start", "--project", "demo", "-o", "json", "--agent", "gemini", "--title", "等放行", "--prompt", "say 先这样", "--wait").stdout)
	if s.SessionID == "" {
		t.Fatal("no session to write to")
	}
	helper := w.addAgent("helper", &localv1.GrantSpec{TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_ALL, Agents: []string{"gemini"}})
	r := run(t, append(append([]string{}, w.cliEnv...), helper...), time.Minute, filepath.Join(w.bin, "aide"), "message", "send", "-o", "json", "--to", s.SessionID, "--message", "say 放行了")
	if !strings.Contains(r.stdout, `"held"`) {
		t.Fatalf("not held: %+v", r)
	}
}
