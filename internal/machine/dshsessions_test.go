package machine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// writeDSHSession writes an uncompressed DSH session log with n turns of
// two events each (seq 0 to 2n-1).
func writeDSHSession(t *testing.T, home, id, cwd string, turns int) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "--"+strings.Trim(strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(cwd), "-")+"--", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lines := []any{map[string]any{"type": "session", "version": 4, "id": id, "createdAt": 1, "cwd": cwd, "isSeeded": false, "delegationDepth": 0}}
	for i := range turns {
		lines = append(lines, map[string]any{"type": "turn/start", "seq": 2 * i, "time": 2, "data": map[string]any{"turn": i + 1}},
			map[string]any{"type": "turn/end", "seq": 2*i + 1, "time": 3, "data": map[string]any{"turn": i + 1, "reason": map[string]any{"kind": "completed"}}})
	}
	var b []byte
	for _, l := range lines {
		j, _ := json.Marshal(l)
		b = append(append(b, j...), '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "session.v4.jsonl"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The DSH sessions listed are the owner's own: not those of this machine's
// sessions, by the id Aide recorded or by their worktree.
func TestDSHSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	h := newHarness(t, Limits{})
	id := h.start("hello")
	ref := h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE).ProviderSessionRef
	writeDSHSession(t, home, ref, "/somewhere", 1)
	writeDSHSession(t, home, "earlier-start", h.n.sessions.ws.Path(id), 1)
	writeDSHSession(t, home, "owners", "/home/alex/app", 30)

	list, err := h.n.DSHSessions(context.Background())
	if err != nil || len(list.Sessions) != 1 || list.Sessions[0].SessionId != "owners" {
		t.Fatalf("listed: %v %v", list, err)
	}

	page := func(req *machinev1.ReadDSHSessionRequest) string {
		req.SessionId = "owners"
		res, err := h.n.ReadDSHSession(req)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(res.Events[0].Seq, "-", res.Events[len(res.Events)-1].Seq, " ", res.HasMoreBefore, " ", res.HasMoreAfter)
	}
	for _, c := range []struct {
		req  *machinev1.ReadDSHSessionRequest
		want string
	}{
		{&machinev1.ReadDSHSessionRequest{}, "11-60 true false"},
		{&machinev1.ReadDSHSessionRequest{Limit: 100}, "1-60 false false"},
		{&machinev1.ReadDSHSessionRequest{AfterSeq: 5, Limit: 10}, "6-15 true true"},
		{&machinev1.ReadDSHSessionRequest{AfterSeq: 50}, "51-60 true false"},
		{&machinev1.ReadDSHSessionRequest{BeforeSeq: 11, Limit: 4}, "7-10 true true"},
		{&machinev1.ReadDSHSessionRequest{BeforeSeq: 3}, "1-2 false true"},
	} {
		if got := page(c.req); got != c.want {
			t.Errorf("%v: %s, want %s", c.req, got, c.want)
		}
	}
	if _, err := h.n.ReadDSHSession(&machinev1.ReadDSHSessionRequest{SessionId: "nope"}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("no such session: %v", err)
	}
	if _, err := h.n.ReadDSHSession(&machinev1.ReadDSHSessionRequest{SessionId: "owners", AfterSeq: 1, BeforeSeq: 9}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("both: %v", err)
	}
}

// An agent sees the DSH sessions as it sees sessions it did not start:
// with a grant that sees all of them, in the projects it covers (every
// folder with all projects); none with one that sees its own. Never those
// Aide started.
func TestDSHSessionsForAgents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	h := newHarness(t, Limits{})
	id := h.start("hello")
	ref := h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE).ProviderSessionRef
	demo, err := h.n.st.Project("demo")
	if err != nil {
		t.Fatal(err)
	}
	writeDSHSession(t, home, ref, demo.Path, 1)
	writeDSHSession(t, home, "in-demo", filepath.Join(demo.Path, "web"), 1)
	writeDSHSession(t, home, "elsewhere", "/home/alex/app", 1)

	svc := &machineService{n: h.n}
	for _, c := range []struct {
		visibility identityv1.TaskVisibility
		projects   []string
		want       string
	}{
		{identityv1.TaskVisibility_TASK_VISIBILITY_OWN, []string{"*"}, ""},
		{identityv1.TaskVisibility_TASK_VISIBILITY_ALL, []string{"*"}, "elsewhere in-demo"},
		{identityv1.TaskVisibility_TASK_VISIBILITY_ALL, []string{"demo"}, "in-demo"},
		{identityv1.TaskVisibility_TASK_VISIBILITY_ALL, []string{"other"}, ""},
	} {
		ctx := context.WithValue(context.Background(), principalKey{}, &Principal{Kind: PrincipalClient, ClientID: "c_agent",
			Grant: &identityv1.Grant{ClientId: "c_agent", MachineIds: []string{"*"}, ProjectIds: c.projects, Agents: []string{"*"}, TaskVisibility: c.visibility}})
		list, err := svc.ListDSHSessions(ctx, &machinev1.ListDSHSessionsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, s := range list.Sessions {
			got = append(got, s.SessionId)
		}
		slices.Sort(got)
		if strings.Join(got, " ") != c.want {
			t.Errorf("%v %v: listed %q, want %q", c.visibility, c.projects, got, c.want)
		}
		for _, id := range []string{ref, "in-demo", "elsewhere"} {
			_, err := svc.ReadDSHSession(ctx, &machinev1.ReadDSHSessionRequest{SessionId: id})
			if seen := slices.Contains(got, id); seen != (err == nil) || !seen && connect.CodeOf(err) != connect.CodeNotFound {
				t.Errorf("%v %v: read %s: %v", c.visibility, c.projects, id, err)
			}
		}
	}
}
