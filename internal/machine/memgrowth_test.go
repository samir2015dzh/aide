package machine

import (
	"fmt"
	"testing"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// Work order #40: the change log keeps at most changeLogKept entries; a
// token from before what it let go starts the watcher again, one after it
// resumes, and nothing changed after the floor is missed.
func TestChangeLogBounded(t *testing.T) {
	c := newChangeLog()
	early := c.token(0)
	for i := 0; i < 3*changeLogKept; i++ {
		c.session(fmt.Sprintf("s%d", i))
	}
	if n := len(c.sessions) + len(c.projects); n > changeLogKept {
		t.Fatalf("%d entries kept", n)
	}
	if _, ok := c.resume(early); ok {
		t.Fatal("a token from before the floor resumed")
	}
	if _, _, _, behind := c.since(0); !behind {
		t.Fatal("since(0) is not behind")
	}
	at := c.n
	if n, ok := c.resume(c.token(at)); !ok || n != at {
		t.Fatalf("a current token: %d %v", n, ok)
	}
	c.session("late")
	got, _, _, behind := c.since(at)
	if behind || len(got) != 1 || got[0] != "late" {
		t.Fatalf("since the last token: %v %v", got, behind)
	}
}

// Work order #40: what the machine keeps per session goes with it.
func TestForgetSession(t *testing.T) {
	n := openTestMachine(t)
	n.tools.path = "/tmp/x.sock"
	if len(n.mcpServers("s1")) != 1 || len(n.tools.byID) != 1 {
		t.Fatal("no token made")
	}
	n.mu.Lock()
	n.lastChain = map[string]chainAt{"s1": {"t", 1}, "s2": {"u", 1}}
	n.handedTurns = map[string][]string{"s1": {"m"}}
	n.mu.Unlock()
	n.wsReqs.sessions = map[string]map[string]*sessionv1.WorkspaceRequest{"s1": {}}
	n.forgetSession("s1")
	if len(n.tools.byID) != 0 || len(n.tools.tokens) != 0 || len(n.lastChain) != 1 || len(n.handedTurns) != 0 || len(n.wsReqs.sessions) != 0 {
		t.Fatalf("left: %v %v %v %v %v", n.tools.byID, n.tools.tokens, n.lastChain, n.handedTurns, n.wsReqs.sessions)
	}
}
