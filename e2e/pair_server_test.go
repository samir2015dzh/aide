package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
)

// TestPairWithServerAddress: a machine pairs with the bare address of the
// owner's own server, which pins the one owner the server reports, as a
// connection address would; a server nobody claimed yet says so. --yes,
// which only meant something to a hosted center, is still accepted.
func TestPairWithServerAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	bin := binaries(t)
	daemon := filepath.Join(bin, "hermeshd")

	// A server nobody claimed yet.
	port := freePort(t)
	relay := start(t, "unclaimed relay", nil, filepath.Join(bin, "hermesh-relay"), "serve", "--data-dir", filepath.Join(t.TempDir(), "relay"),
		"--listen", fmt.Sprintf("127.0.0.1:%d", port))
	eventually(t, 20*time.Second, "relay up", func() bool { return strings.Contains(relay.output(), "relay listening") })
	r := run(t, nil, 30*time.Second, daemon, "pair", fmt.Sprintf("http://127.0.0.1:%d", port), "--no-service", "--data-dir", filepath.Join(t.TempDir(), "m"))
	if r.code == 0 || !strings.Contains(r.stdout+r.stderr, "is not set up yet") {
		t.Fatalf("pair with an unclaimed server: %+v", r)
	}

	// The owner's server, by its address alone.
	w := newOwnerMac(t, []string{"HERMESH_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()
	st, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(w.tmp, "vps-bare")
	pair := start(t, "pair vps-bare", []string{"HERMESH_FAKE_PROVIDERS=1"}, daemon, "pair", st.RelayUrl,
		"--name", "vps-bare", "--yes", "--no-service", "--data-dir", data)
	if _, err := w.app.client.ApproveMachinePairing(ctx, &localv1.ApproveMachinePairingRequest{Code: pair.pairingCode(t), MachineName: "vps-bare"}); err != nil {
		t.Fatal(err)
	}
	if c := pair.wait(t, 30*time.Second); c != 0 {
		t.Fatalf("pair: %s", pair.output())
	}
	if out := pair.output(); !strings.Contains(out, "Paired as") || !strings.Contains(out, "deprecated") {
		t.Fatalf("pair said: %s", out)
	}
	ms, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// "✓ Paired as “vps-bare” (m_…)": the owner's machine list has it.
	id := regexp.MustCompile(`\((m_[a-z0-9]+)\)`).FindStringSubmatch(pair.output())
	found := false
	for _, m := range ms.Machines {
		found = found || id != nil && m.MachineId == id[1]
	}
	if !found {
		t.Fatalf("vps-bare (%v) is not among the owner's machines: %v", id, ms.Machines)
	}
}
