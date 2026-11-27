package machine

import (
	"context"
	"errors"
	"testing"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// testTurn is an ACP-like agent (its check cannot tell its sign-in) whose
// test turn answers, or fails as not logged in.
type testTurn struct{ signedOut bool }

func (testTurn) Name() string { return "acplike" }
func (testTurn) Discover(context.Context) provider.Info {
	return provider.Info{Name: "acplike", Available: true, Version: "1.0.0", LoginUnconfirmed: true}
}
func (t testTurn) Start(context.Context, provider.StartSpec) (provider.Session, error) {
	if t.signedOut {
		return nil, provider.Fail(sessionv1.FailureKind_FAILURE_KIND_AUTH, "authenticate: not logged in")
	}
	events := make(chan provider.Event, 2)
	events <- provider.Event{Kind: provider.EventAgentMessage, Text: "4"}
	events <- provider.Event{Kind: provider.EventTurnCompleted}
	return &answered{events: events, done: make(chan struct{})}, nil
}

type answered struct {
	provider.Session
	events chan provider.Event
	done   chan struct{}
}

func (a *answered) Events() <-chan provider.Event { return a.events }
func (a *answered) Done() <-chan struct{}         { return a.done }
func (a *answered) Err() error                    { return errors.New("closed") }
func (a *answered) Close() error                  { return nil }

// TestRunTestShowsSignIn (工单 #180): Run Test is a turn too, so what it
// shows of an ACP agent's sign-in stands as a session's does — one that
// answered makes it signed in, one that failed "not logged in" signed out —
// not "unconfirmed" until a real task runs.
func TestRunTestShowsSignIn(t *testing.T) {
	for _, c := range []struct {
		signedOut bool
		in        bool
	}{{false, true}, {true, false}} {
		n, err := Open(Config{DataDir: t.TempDir(), Providers: map[string]provider.Provider{"acplike": testTurn{signedOut: c.signedOut}}})
		if err != nil {
			t.Fatal(err)
		}
		<-n.CheckAgents(true, 0)
		if _, err := n.TestAgent(context.Background(), "acplike"); err != nil {
			t.Fatal(err)
		}
		<-n.CheckAgents(true, 0) // and through the next check
		n.mu.Lock()
		i := n.infos["acplike"]
		n.mu.Unlock()
		n.Close()
		if i.LoggedIn != c.in || i.LoginUnconfirmed {
			t.Fatalf("signed out %v: after Run Test %+v", c.signedOut, i)
		}
	}
}
