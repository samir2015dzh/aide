package e2e

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/gen/go/aide/relay/v1/relayv1connect"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// TestMailable (工单 #161): the owner's list of agents says whether a message can go to each, as the post office
// knows it. An agent that joined and never registered an inbox is listed not mailable — the apps' roster then says it
// can't take messages and the chat box says so before anything is sent; one logged in with Aide's command line, which
// registers its inbox, is mailable, and the owner hears of it without asking again.
func TestMailable(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	ctx := context.Background()
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	mailable := func(label string) *bool {
		cs, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
		if err != nil {
			return nil
		}
		for _, c := range cs.Clients {
			if c.Label == label {
				return c.Mailable
			}
		}
		return nil
	}

	// Joined at the relay, no inbox: listed, not mailable.
	k, _ := identity.GenerateKey()
	j := &relayv1.Joiner{Kind: relayv1.JoinKind_JOIN_KIND_AGENT, PublicKey: identity.MarshalPublicKey(k.Public()), Label: "No inbox"}
	if _, err := relayv1connect.NewRelayPublicServiceClient(http.DefaultClient, w.server).LogIn(ctx, &relayv1.LogInRequest{Account: testAccount, Password: testPassword, Joiner: j}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "the agent with no inbox listed not mailable", func() bool {
		m := mailable("No inbox")
		return m != nil && !*m
	})

	// Logged in with the command line, which registers its inbox: mailable.
	w.loginAgent([]string{"AIDE_HOME=" + filepath.Join(w.tmp, "agent-inbox")}, "With inbox", nil)
	eventually(t, 30*time.Second, "the agent with its inbox listed mailable", func() bool {
		m := mailable("With inbox")
		return m != nil && *m
	})
	if m := mailable("No inbox"); m == nil || *m {
		t.Fatalf("the agent with no inbox: mailable %v", m)
	}
}
