package machine

import (
	"context"
	"strings"
	"testing"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// userTexts are the user messages the session's timeline records.
func (h *harness) userTexts(id string) []string {
	var out []string
	for _, e := range h.events(id) {
		if m := e.GetUserMessage(); m != nil {
			out = append(out, m.Text)
		}
	}
	return out
}

func TestSessionReferenceInjectsContext(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	old := h.start("say the answer is 42")
	h.wait(old, idle)

	id := h.start("go on from #" + old + " please")
	h.wait(id, idle)
	got := h.rec.spec(-1).Prompt
	for _, want := range []string{"go on from #" + old + " please", "[Aide] Referenced session #" + old, "Title: say the answer is 42", "Final result:\nthe answer is 42", "[Aide] End of session #" + old} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, got)
		}
	}
	if u := h.userTexts(id); len(u) != 1 || u[0] != got {
		t.Fatalf("timeline records %q, the agent got %q", u, got)
	}

	// Into a running session too.
	h.send(id, "and #"+old)
	h.wait(id, idle)
	if u := h.userTexts(id); !strings.HasPrefix(u[len(u)-1], "and #"+old+"\n\n[Aide] Referenced session #"+old) {
		t.Fatalf("sent message not expanded: %q", u)
	}
}

func TestSessionReferenceTruncates(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	old := h.start("say " + strings.Repeat("x", maxRefResultChars+100))
	h.wait(old, idle)
	got, err := h.s.expandPrompt(h.p, "#"+old)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[truncated: 4000 of 4100 characters shown]") || strings.Contains(got, strings.Repeat("x", maxRefResultChars+1)) {
		t.Fatalf("not truncated:\n%s", got)
	}
}

func TestSessionReferenceNeedsAccess(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	old := h.start("say secret")
	h.wait(old, idle)
	other := &Principal{Kind: PrincipalClient, ClientID: "c_other", Grant: &identityv1.Grant{GrantId: "g_other",
		MachineIds: []string{"*"}, ProjectIds: []string{"*"}, Agents: []string{"*"}}}
	for _, ref := range []string{"#" + old, "#s_nosuch_session"} {
		_, err := h.s.Start(context.Background(), other, &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "fake", Prompt: "look at " + ref})
		if connect.CodeOf(err) != connect.CodeNotFound || !strings.Contains(err.Error(), "referenced session "+ref) {
			t.Fatalf("%s: %v", ref, err)
		}
	}
	// With task visibility "all", the grant lets it see the session.
	other.Grant.TaskVisibility = identityv1.TaskVisibility_TASK_VISIBILITY_ALL
	if _, err := h.s.expandPrompt(other, "look at #"+old); err != nil {
		t.Fatal(err)
	}
}

func TestMacros(t *testing.T) {
	h := newHarness(t, DefaultLimits())
	for name, body := range macros {
		got, err := h.s.expandPrompt(h.p, "  !"+name+" fix the login bug")
		if err != nil || got != body+"\n\nfix the login bug" {
			t.Fatalf("!%s: %q %v", name, got, err)
		}
	}
	if _, err := h.s.expandPrompt(h.p, "!nope do it"); connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), "!implement, !plan") {
		t.Fatalf("unknown macro: %v", err)
	}
	// Not a macro or a reference: left as written.
	for _, s := range []string{"run it! now", "write to @alice", "see issue#s_x", "!!", "! plan"} {
		if got, err := h.s.expandPrompt(h.p, s); err != nil || got != s {
			t.Fatalf("%q: %q %v", s, got, err)
		}
	}
}
