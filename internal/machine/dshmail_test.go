package machine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
)

// A message from an agent to m_…/dsh_… goes into the DSH session the
// owner took over through the plugin, prefaced for the model with who wrote
// it (a plugin with "from"), under the message's own request id, and the
// machine keeps who wrote it, for the session's answer. Not taken over, it
// is refused; while DSH Desktop does not run it waits (unavailable: the post
// office keeps it); files, a message for information only, another
// machine's DSH session, refused.
func TestDSHLetters(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	h := newHarness(t, Limits{})
	writeDSHSession(t, home, "owners", "/home/alex/app", 1)
	ctx := context.Background()
	muse := &Principal{Kind: PrincipalClient, ClientID: "c_muse", Grant: &identityv1.Grant{ClientId: "c_muse", ClientLabel: "Muse", TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_ALL, ProjectIds: []string{"*"}}}
	to := dshAddress(h.n.machineID, "owners")
	if h.n.dshOf(to) != "owners" || h.n.dshOf("dsh_otherone_owners") != "" || h.n.dshOf("s_x") != "" {
		t.Fatalf("dshOf(%s) = %q", to, h.n.dshOf(to))
	}
	letter := func(id, text string) *inboxv1.Message {
		return &inboxv1.Message{MessageId: id, From: &inboxv1.Address{Inbox: "c_muse", Session: "conv1"}, To: &inboxv1.Address{Inbox: h.n.machineID, Session: to}, Text: text, TraceId: "tr_1", Hop: 1}
	}
	deliver := func(m *inboxv1.Message, atts ...*sessionv1.Attachment) (string, error) {
		_, d, err := h.n.deliverLetter(ctx, muse, m, "Muse", atts)
		return d, err
	}

	// No plugin running: the gate comes first, then it waits.
	if _, err := deliver(letter("msg_01aaa", "hi")); codeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "has not taken over") {
		t.Fatalf("not taken over: %v", err)
	}
	if _, err := h.n.SetDSHTakeover(&machinev1.SetDSHTakeoverRequest{SessionId: "owners", On: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := deliver(letter("msg_01aaa", "hi")); codeOf(err) != connect.CodeUnavailable {
		t.Fatalf("DSH Desktop not running: %v, want unavailable (kept at the post office)", err)
	}
	h.n.mu.Lock()
	pending := h.n.dshPending
	h.n.mu.Unlock()
	if !pending {
		t.Fatal("not noted as waiting for DSH Desktop")
	}

	b := startFakeBridge(t, home)
	b.features = append(b.features, "from", "outbox")
	d, err := deliver(letter("msg_01aaa", "how far?"))
	if err != nil || d != "handed" {
		t.Fatalf("delivered %q, %v", d, err)
	}
	b.mu.Lock()
	got := b.prompts[len(b.prompts)-1]
	b.mu.Unlock()
	if got["requestId"] != "aide-01aaa" || got["text"] != "how far?" || got["messageId"] != "msg_01aaa" || fmtJSON(got["from"]) != `{"address":"c_muse/conv1","name":"Muse"}` {
		t.Fatalf("the plugin got %v", got)
	}
	var rec dshSent
	raw, _ := h.n.st.Get(dshSentKey("aide-01aaa"))
	if json.Unmarshal(raw, &rec); rec.From != "c_muse" || rec.Name != "Muse" || rec.Address != "c_muse/conv1" || rec.MessageID != "msg_01aaa" {
		t.Fatalf("kept %+v", rec)
	}
	if last, _ := h.n.st.Get(dshLastKey("owners")); string(last) != "aide-01aaa" {
		t.Fatalf("last %q", last)
	}
	// A plugin without "from" gets the text alone.
	b.features = []string{"options", "selections"}
	if _, err := deliver(letter("msg_01bbb", "plain")); err != nil {
		t.Fatal(err)
	}
	if got := b.prompts[len(b.prompts)-1]; got["from"] != nil || got["text"] != "plain" {
		t.Fatalf("to an older plugin: %v", got)
	}

	// Refused.
	for what, m := range map[string]*inboxv1.Message{
		"fyi":       func() *inboxv1.Message { m := letter("msg_01ccc", "x"); m.Fyi = true; return m }(),
		"another's": func() *inboxv1.Message { m := letter("msg_01ddd", "x"); m.To.Session = "dsh_otherone_owners"; return m }(),
		"too long":  letter("msg_01eee", strings.Repeat("x", maxDSHMessage+1)),
	} {
		if _, err := deliver(m); err == nil || codeOf(err) == connect.CodeUnavailable {
			t.Errorf("%s: %v", what, err)
		}
	}
	if _, err := deliver(letter("msg_01fff", "x"), &sessionv1.Attachment{Name: "a.txt"}); codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("with a file: %v", err)
	}
	// An agent that can't read the session doesn't reach it.
	own := &Principal{Kind: PrincipalClient, ClientID: "c_own", Grant: &identityv1.Grant{ClientId: "c_own", TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_OWN, ProjectIds: []string{"*"}}}
	if _, _, err := h.n.deliverLetter(ctx, own, letter("msg_01ggg", "x"), "Own", nil); codeOf(err) != connect.CodeNotFound {
		t.Errorf("an agent that can't read it: %v", err)
	}
	// DSH busy: it waits, and is not sent twice (DSH drops the same id).
	b.refuse = "busy"
	if _, err := deliver(letter("msg_01hhh", "x")); codeOf(err) != connect.CodeUnavailable {
		t.Errorf("DSH busy: %v", err)
	}
	b.refuse = ""

	// The session's answers: only once taken over, only to an agent this
	// machine knows, by default to the last that wrote.
	send := func(s dshnative.Send) string {
		_, _, refused := h.n.dshSend(s)
		if refused == nil {
			return ""
		}
		return refused.Message
	}
	for _, c := range []struct {
		s    dshnative.Send
		want string
	}{
		{dshnative.Send{SessionID: "owners", Text: "to a session", To: h.n.machineID + "/s_x"}, "not to sessions"},
		{dshnative.Send{SessionID: "owners", Text: "to another DSH session", To: "dsh_k3j9_x"}, "not to sessions"},
		{dshnative.Send{SessionID: "owners", Text: "to a stranger", To: "c_stranger"}, "not an agent"},
		{dshnative.Send{SessionID: "owners", Text: " "}, "empty"},
		{dshnative.Send{SessionID: "nosuch", Text: "x"}, "no session"},
	} {
		if got := send(c.s); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want %q", c.s.Text, got, c.want)
		}
	}
	// More than 30 in an hour: held for the owner; the hour before is forgotten.
	h.n.mu.Lock()
	h.n.dshOut = map[string][]time.Time{"owners": make([]time.Time, dshOutPerHour)}
	for i := range h.n.dshOut["owners"] {
		h.n.dshOut["owners"][i] = time.Now().Add(-time.Duration(i) * time.Minute)
	}
	h.n.mu.Unlock()
	if got := send(dshnative.Send{SessionID: "owners", Text: "x", To: "c_stranger"}); !strings.Contains(got, "held for the owner") {
		t.Errorf("the 31st in an hour: %q", got)
	}
	if n := h.n.dshSentLastHour("owners", time.Now().Add(61*time.Minute)); n != 0 {
		t.Errorf("%d counted an hour on, want none", n)
	}
	if _, err := h.n.SetDSHTakeover(&machinev1.SetDSHTakeoverRequest{SessionId: "owners", On: false}); err != nil {
		t.Fatal(err)
	}
	if got := send(dshnative.Send{SessionID: "owners", Text: "x"}); !strings.Contains(got, "not taken over") {
		t.Errorf("takeover off: %q", got)
	}
}

func fmtJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
