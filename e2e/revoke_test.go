package e2e

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// TestRevokedAgentsLeaveTheList: a revoked personal agent disappears from
// the owner's list and is still refused. Removing a machine revokes the
// agents that could use only that machine, and drops it from the scope of
// the others. Both hold after the relay restarts.
func TestRevokedAgentsLeaveTheList(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{}) // "bot": every machine
	ctx := context.Background()
	ms, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
	if err != nil || len(ms.Machines) != 1 {
		t.Fatalf("machines: %v %v", ms, err)
	}
	mac := ms.Machines[0].MachineId
	other := "m_" + strings.Repeat("b", 24) // a machine that is not removed
	env := func(name string) []string { return []string{"AIDE_HOME=" + filepath.Join(w.tmp, name)} }
	pair := func(name string, machines ...string) {
		t.Helper()
		w.loginAgent(env(name), name, &localv1.GrantSpec{MachineIds: machines})
	}
	pair("only-mac", mac)
	pair("mac-and-other", mac, other)

	listed := func() map[string]*localv1.Client {
		t.Helper()
		res, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]*localv1.Client{}
		for _, c := range res.Clients {
			m[c.Label] = c
		}
		return m
	}
	refused := func(name string, e []string) {
		t.Helper()
		eventually(t, 30*time.Second, name+" refused as revoked", func() bool {
			r := run(t, e, 30*time.Second, filepath.Join(w.bin, "aide"), "project", "list")
			return r.code == 3 && strings.Contains(r.stderr, "aide auth login")
		})
	}
	if l := listed(); len(l) != 3 {
		t.Fatalf("listed before: %v", l)
	}

	// Revoking an agent forgets it; the revocation still refuses it.
	if _, err := w.app.client.RevokeClient(ctx, &localv1.RevokeClientRequest{GrantId: listed()["bot"].GrantId}); err != nil {
		t.Fatal(err)
	}
	if l := listed(); l["bot"] != nil || len(l) != 2 {
		t.Fatalf("listed after revoking bot: %v", l)
	}
	refused("bot", w.cliEnv)

	// Removing the Mac revokes the agent that could use only the Mac, and
	// drops the Mac from the other's scope.
	if _, err := w.app.client.RemoveMachine(ctx, &localv1.RemoveMachineRequest{MachineId: mac}); err != nil {
		t.Fatal(err)
	}
	check := func(when string) {
		t.Helper()
		l := listed()
		if l["only-mac"] != nil || l["mac-and-other"] == nil || len(l) != 1 {
			t.Fatalf("%s: listed %v", when, l)
		}
		if got := l["mac-and-other"].Spec.MachineIds; !slices.Equal(got, []string{other}) {
			t.Fatalf("%s: mac-and-other may use %v, want only %s", when, got, other)
		}
	}
	check("after removing the Mac")
	refused("only-mac", env("only-mac"))

	w.restartRelay()
	eventually(t, 30*time.Second, "owner back on the relay", func() bool {
		_, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
		return err == nil
	})
	check("after the relay restarted")
	refused("bot", w.cliEnv)
}
