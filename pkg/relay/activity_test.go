package relay

import (
	"context"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// An MCP connection (its key held by a server) is online by its last call, within ActiveWindow, and then offline with
// when that was; an agent with a connection of its own is online only while connected, its last connection said
// once it left (工单 #94).
func TestOnlineByLastCall(t *testing.T) {
	s, pub := newTestServer(t)
	ctx := context.Background()
	code, _ := s.SetupCode()
	_, dj := joiner(relayv1.JoinKind_JOIN_KIND_DEVICE)
	if _, err := pub.SetUp(ctx, &relayv1.SetUpRequest{SetupCode: code, Account: "me", Password: "correct horse", Joiner: dj}); err != nil {
		t.Fatal(err)
	}
	owner := s.SingleOwner()
	key, _ := identity.GenerateKey()
	sg, _, err := s.IssueGrant("me", "correct horse", identity.MarshalPublicKey(key.Public()), "Muse", "https://gateway.example")
	if err != nil {
		t.Fatal(err)
	}
	mcp := &identityv1.Grant{}
	identity.Unverified(sg, mcp)
	_, agent := joiner(relayv1.JoinKind_JOIN_KIND_AGENT)
	res, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "correct horse", Joiner: agent})
	if err != nil {
		t.Fatal(err)
	}
	own := &identityv1.Grant{}
	identity.Unverified(res.Credential, own)

	entry := func(clientID string) *relayv1.ClientEntry {
		t.Helper()
		entries, err := s.ClientEntries(owner)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			g := &identityv1.Grant{}
			if identity.Unverified(e.Grant, g) == nil && g.ClientId == clientID {
				return e
			}
		}
		t.Fatalf("%s is not listed", clientID)
		return nil
	}
	presence := func(clientID string) *relayv1.Presence {
		t.Helper()
		tn, _ := s.tenantFor(owner)
		for _, p := range s.presence(tn) {
			if p.Inbox == clientID {
				return p
			}
		}
		return nil
	}

	// Never called: offline, with nothing to say when.
	if e := entry(mcp.ClientId); e.Online || e.Recent || e.LastSeen != nil {
		t.Fatalf("before any call: %v", e)
	}
	// A call five minutes ago, as a client calling every five minutes: online by it, said so, for the owner and the
	// gateway's roster alike.
	called := time.Now().Add(-5 * time.Minute)
	s.ClientActive(owner, mcp.ClientId, called)
	if e := entry(mcp.ClientId); !e.Online || !e.Recent || !e.LastSeen.AsTime().Equal(called) {
		t.Fatalf("a call 5 minutes ago: %v", e)
	}
	if p := presence(mcp.ClientId); p == nil || !p.Online || !p.Recent {
		t.Fatalf("the roster's presence: %v", p)
	}
	// An earlier call doesn't take it back.
	s.ClientActive(owner, mcp.ClientId, called.Add(-time.Minute))
	if e := entry(mcp.ClientId); !e.LastSeen.AsTime().Equal(called) {
		t.Fatalf("an earlier call moved it: %v", e)
	}

	// Its last call past the window: offline, with when it called.
	tn, _ := s.tenantFor(owner)
	old := time.Now().Add(-ActiveWindow - time.Minute)
	s.mu.Lock()
	tn.lastSeen[mcp.ClientId] = old
	s.mu.Unlock()
	if e := entry(mcp.ClientId); e.Online || e.Recent || !e.LastSeen.AsTime().Equal(old) {
		t.Fatalf("its call past the window: %v", e)
	}
	if p := presence(mcp.ClientId); p == nil || p.Online || p.Recent {
		t.Fatalf("the roster's presence past the window: %v", p)
	}

	// An agent with a connection of its own is not online by what it did: gone a minute ago, it is offline.
	left := time.Now().Add(-time.Minute)
	s.mu.Lock()
	tn.lastSeen[own.ClientId] = left
	s.mu.Unlock()
	if e := entry(own.ClientId); e.Online || e.Recent || !e.LastSeen.AsTime().Equal(left) {
		t.Fatalf("an agent of its own, gone: %v", e)
	}
}

// An agent is mailable once its inbox is registered (工单 #161): before that, online or not, the owner's list says a
// message cannot go to it.
func TestMailable(t *testing.T) {
	s, pub := newTestServer(t)
	ctx := context.Background()
	code, _ := s.SetupCode()
	_, dj := joiner(relayv1.JoinKind_JOIN_KIND_DEVICE)
	if _, err := pub.SetUp(ctx, &relayv1.SetUpRequest{SetupCode: code, Account: "me", Password: "correct horse", Joiner: dj}); err != nil {
		t.Fatal(err)
	}
	owner := s.SingleOwner()
	key, _ := identity.GenerateKey()
	sg, _, err := s.IssueGrant("me", "correct horse", identity.MarshalPublicKey(key.Public()), "Bot", "https://gateway.example")
	if err != nil {
		t.Fatal(err)
	}
	g := &identityv1.Grant{}
	identity.Unverified(sg, g)
	mailable := func() *bool {
		t.Helper()
		entries, err := s.ClientEntries(owner)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			eg := &identityv1.Grant{}
			if identity.Unverified(e.Grant, eg) == nil && eg.ClientId == g.ClientId {
				return e.Mailable
			}
		}
		t.Fatal("not listed")
		return nil
	}
	s.ClientActive(owner, g.ClientId, time.Now())
	if m := mailable(); m == nil || *m {
		t.Fatalf("online with no inbox: mailable %v", m)
	}
	if err := s.messageStore().PutRegistration(owner, g.ClientId, &identityv1.Signed{Body: []byte("reg")}); err != nil {
		t.Fatal(err)
	}
	if m := mailable(); m == nil || !*m {
		t.Fatalf("with its inbox registered: mailable %v", m)
	}
}
