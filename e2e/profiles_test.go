package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
)

// TestAccountsOnOneDevice: one Mac belongs to two owners, each on its own
// server, with a profile for each (docs/accounts.md §6). It switches
// between them, each with its own machine; it signs out and back in
// without another device; and, signed out, it is signed out for good by
// another device of that owner, learns so when it signs in, and leaves.
func TestAccountsOnOneDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	bin := binaries(t)
	relayBin, daemonBin := filepath.Join(bin, "hermesh-relay"), filepath.Join(bin, "hermeshd")
	tmp := t.TempDir()
	sockDir, _ := os.MkdirTemp(shortTemp, "hm")
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	env := []string{"HERMESH_FAKE_PROVIDERS=1"}
	ctx := context.Background()
	var logs []*proc
	t.Cleanup(func() {
		if t.Failed() {
			for _, p := range logs {
				t.Logf("==== %s output ====\n%s", p.name, p.output())
			}
		}
	})

	startRelay := func(name string) (url, claim string) {
		port := freePort(t)
		data := filepath.Join(tmp, name)
		p := start(t, name, nil, relayBin, "serve", "--data-dir", data, "--listen", fmt.Sprintf("127.0.0.1:%d", port))
		logs = append(logs, p)
		eventually(t, 20*time.Second, name+" up", func() bool { return strings.Contains(p.output(), "relay listening") })
		return fmt.Sprintf("http://127.0.0.1:%d", port), strings.TrimSpace(run(t, nil, 10*time.Second, relayBin, "setup-code", "--data-dir", data).stdout)
	}
	relayA, claimA := startRelay("relay-a")
	relayB, claimB := startRelay("relay-b")

	macData, sock := filepath.Join(tmp, "mac"), filepath.Join(sockDir, "m.sock")
	startMac := func() *proc {
		p := start(t, "mac", env, daemonBin, "run", "--owner", "--data-dir", macData, "--socket", sock)
		logs = append(logs, p)
		return p
	}
	macProc := startMac()
	app := newTestApp(t, sock)
	status := func(a *testApp) *localv1.GetStatusResponse {
		st, err := a.client.GetStatus(ctx, &localv1.GetStatusRequest{})
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	profiles := func() []*localv1.Profile {
		res, err := app.client.ListProfiles(ctx, &localv1.ListProfilesRequest{})
		if err != nil {
			t.Fatal(err)
		}
		return res.Profiles
	}
	machineOnline := func(a *testApp, name string) bool {
		res, err := a.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		return err == nil && slices.ContainsFunc(res.Machines, func(m *localv1.Machine) bool { return m.MachineName == name && m.Online })
	}

	// Account A, with this Mac as its machine.
	setupA, err := app.client.Setup(ctx, &localv1.SetupRequest{RelayUrl: relayA, SetupCode: claimA, Account: "a@example.com", Password: testPassword, Label: "mac",
		Platform: "macos", LocalMachineName: "mac-a"})
	if err != nil {
		t.Fatalf("setup A: %v\n%s", err, macProc.output())
	}
	eventually(t, 30*time.Second, "mac-a online", func() bool { return machineOnline(app, "mac-a") })
	deviceA := status(app).DeviceKeyId

	// Account B, added: a new profile with its own key and its own machine.
	added, err := app.client.AddProfile(ctx, &localv1.AddProfileRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if st := status(app); st.Initialized || st.LocalMachineEnabled {
		t.Fatalf("an added profile is not empty: %+v", st)
	}
	setupB, err := app.client.Setup(ctx, &localv1.SetupRequest{RelayUrl: relayB, SetupCode: claimB, Account: "b@example.com", Password: testPassword, Label: "mac",
		Platform: "macos", LocalMachineName: "mac-b"})
	if err != nil {
		t.Fatalf("setup B: %v\n%s", err, macProc.output())
	}
	if st := status(app); st.OwnerId != setupB.OwnerId || st.DeviceKeyId == "" || st.DeviceKeyId == deviceA || st.Account != "b@example.com" {
		t.Fatalf("status of B: %+v", st)
	}
	eventually(t, 30*time.Second, "mac-b online", func() bool { return machineOnline(app, "mac-b") })
	if ms, _ := app.client.ListMachines(ctx, &localv1.ListMachinesRequest{}); len(ms.GetMachines()) != 1 {
		t.Fatalf("B sees machines that are not its own: %v", ms)
	}
	ps := profiles()
	if len(ps) != 2 || ps[0].OwnerId != setupA.OwnerId || ps[0].Current || ps[0].Account != "a@example.com" ||
		ps[1].Id != added.Id || ps[1].OwnerId != setupB.OwnerId || !ps[1].Current {
		t.Fatalf("profiles: %v", ps)
	}
	idA, idB := ps[0].Id, ps[1].Id

	// Logging in to an owner this Mac already has is refused; the empty
	// profile that asked is not listed once another is current.
	if _, err := app.client.AddProfile(ctx, &localv1.AddProfileRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.client.LogIn(ctx, &localv1.LogInRequest{RelayUrl: relayA, Account: "a@example.com", Password: testPassword, Label: "mac"}); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("logging in to A again: %v", err)
	}

	// Switching: A is back with its own machine; B's stops.
	if _, err := app.client.SwitchProfile(ctx, &localv1.SwitchProfileRequest{Id: idA}); err != nil {
		t.Fatal(err)
	}
	if len(profiles()) != 2 {
		t.Fatalf("the empty profile is listed: %v", profiles())
	}
	if st := status(app); st.OwnerId != setupA.OwnerId {
		t.Fatalf("status after switching to A: %+v", st)
	}
	eventually(t, 30*time.Second, "mac-a online again", func() bool { return machineOnline(app, "mac-a") && status(app).LocalMachineName == "mac-a" })

	// A second Mac logs in to A.
	sock2 := filepath.Join(sockDir, "m2.sock")
	mac2 := start(t, "mac2", env, daemonBin, "run", "--owner", "--data-dir", filepath.Join(tmp, "mac2"), "--socket", sock2)
	logs = append(logs, mac2)
	app2 := newTestApp(t, sock2)
	if _, err := app2.client.LogIn(ctx, &localv1.LogInRequest{RelayUrl: relayA, Account: "a@example.com", Password: testPassword, Label: "mac2", Platform: "macos"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "mac2 sees mac-a online", func() bool { return machineOnline(app2, "mac-a") })

	// Signing out keeps the keys but stops everything: no relay connection,
	// no machine, no calls that need the owner.
	if _, err := app.client.SignOut(ctx, &localv1.SignOutRequest{}); err != nil {
		t.Fatal(err)
	}
	if st := status(app); !st.SignedOut || !st.Initialized || st.LocalMachineEnabled || st.RelayConnected {
		t.Fatalf("status after signing out: %+v", st)
	}
	if _, err := app.client.ListMachines(ctx, &localv1.ListMachinesRequest{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("listing machines signed out: %v", err)
	}
	eventually(t, 30*time.Second, "mac2 sees mac-a offline", func() bool { return !machineOnline(app2, "mac-a") })
	// Signed out survives a restart.
	macProc.stop()
	macProc = startMac()
	app.cancel()
	app = newTestApp(t, sock)
	if st := status(app); !st.SignedOut || st.OwnerId != setupA.OwnerId {
		t.Fatalf("status after a restart: %+v", st)
	}
	// Signing in again needs no other device (a self-hosted relay: no
	// browser sign-in either).
	if _, err := app.client.SignIn(ctx, &localv1.SignInRequest{}); err != nil {
		t.Fatal(err)
	}
	if st := status(app); st.SignedOut || st.Removed {
		t.Fatalf("status after signing in: %+v", st)
	}
	eventually(t, 30*time.Second, "mac2 sees mac-a online after signing in", func() bool { return machineOnline(app2, "mac-a") })

	// Signed out, the Mac is signed out for good from mac2: signing in, it
	// learns so, and leaves A. B is untouched.
	if _, err := app.client.SignOut(ctx, &localv1.SignOutRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := app2.client.RemoveDevice(ctx, &localv1.RemoveDeviceRequest{KeyId: deviceA}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.client.SignIn(ctx, &localv1.SignInRequest{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "the Mac learns it was signed out", func() bool { return status(app).Removed })
	if _, err := app.client.Leave(ctx, &localv1.LeaveRequest{}); err != nil {
		t.Fatal(err)
	}
	if ps := profiles(); len(ps) != 2 || ps[0].Id != idA || ps[0].OwnerId != "" || !ps[0].Current || ps[1].Id != idB {
		t.Fatalf("profiles after leaving A: %v", ps)
	}
	if _, err := app.client.SwitchProfile(ctx, &localv1.SwitchProfileRequest{Id: idB}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "mac-b online after switching back", func() bool { return machineOnline(app, "mac-b") })

	// The current profile survives a restart.
	macProc.stop()
	startMac()
	app.cancel()
	app = newTestApp(t, sock)
	if st := status(app); st.OwnerId != setupB.OwnerId || st.SignedOut {
		t.Fatalf("status after a restart: %+v", st)
	}
	eventually(t, 30*time.Second, "mac-b online after a restart", func() bool { return machineOnline(app, "mac-b") })
}

// TestOwnerFromBeforeProfiles: a Mac set up before profiles (owner.db and
// keys at the top of the owner directory, its machine beside it) keeps its
// owner and its machine: hermeshd moves the owner into a profile, and the
// machine, whose worktrees name its path, stays where it is.
func TestOwnerFromBeforeProfiles(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	bin := binaries(t)
	relayBin, daemonBin := filepath.Join(bin, "hermesh-relay"), filepath.Join(bin, "hermeshd")
	tmp := t.TempDir()
	sockDir, _ := os.MkdirTemp(shortTemp, "hm")
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	ctx := context.Background()
	var logs []*proc
	t.Cleanup(func() {
		if t.Failed() {
			for _, p := range logs {
				t.Logf("==== %s output ====\n%s", p.name, p.output())
			}
		}
	})
	port := freePort(t)
	relayData := filepath.Join(tmp, "relay")
	relay := start(t, "relay", nil, relayBin, "serve", "--data-dir", relayData, "--listen", fmt.Sprintf("127.0.0.1:%d", port))
	logs = append(logs, relay)
	eventually(t, 20*time.Second, "relay up", func() bool { return strings.Contains(relay.output(), "relay listening") })
	relayURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	claim := strings.TrimSpace(run(t, nil, 10*time.Second, relayBin, "setup-code", "--data-dir", relayData).stdout)

	data, sock := filepath.Join(tmp, "mac"), filepath.Join(sockDir, "m.sock")
	startMac := func() *proc {
		p := start(t, "mac", []string{"HERMESH_FAKE_PROVIDERS=1"}, daemonBin, "run", "--owner", "--data-dir", data, "--socket", sock)
		logs = append(logs, p)
		return p
	}
	mac := startMac()
	app := newTestApp(t, sock)
	setup, err := app.client.Setup(ctx, &localv1.SetupRequest{RelayUrl: relayURL, SetupCode: claim, Account: testAccount, Password: testPassword, Label: "mac", LocalMachineName: "mac"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil || before.LocalMachineId == "" {
		t.Fatalf("status: %v %v", before, err)
	}
	mac.stop()
	app.cancel()

	// The layout from before profiles.
	ownerDir := filepath.Join(data, "owner")
	cur, _ := os.ReadFile(filepath.Join(ownerDir, "current"))
	profile := filepath.Join(ownerDir, "profiles", string(cur))
	for from, to := range map[string]string{"owner.db": "owner/owner.db", "owner.db-wal": "owner/owner.db-wal", "owner.db-shm": "owner/owner.db-shm",
		"keys": "owner/keys", "machine": "machine"} {
		if err := os.Rename(filepath.Join(profile, from), filepath.Join(data, to)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	os.RemoveAll(filepath.Join(ownerDir, "profiles"))
	os.Remove(filepath.Join(ownerDir, "current"))

	startMac()
	app = newTestApp(t, sock)
	st, err := app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil || st.OwnerId != setup.OwnerId || st.DeviceKeyId != before.DeviceKeyId || st.SignedOut {
		t.Fatalf("status after moving: %v %v", st, err)
	}
	eventually(t, 30*time.Second, "its machine online, under the same id", func() bool {
		res, err := app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		return err == nil && slices.ContainsFunc(res.Machines, func(m *localv1.Machine) bool { return m.MachineId == before.LocalMachineId && m.Online })
	})
	if _, err := os.Stat(filepath.Join(ownerDir, "owner.db")); !os.IsNotExist(err) {
		t.Fatalf("owner.db left at the top: %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "machine", "hermeshd.db")); err != nil {
		t.Fatalf("the machine moved: %v", err)
	}
	res, err := app.client.ListProfiles(ctx, &localv1.ListProfilesRequest{})
	if err != nil || len(res.Profiles) != 1 || res.Profiles[0].OwnerId != setup.OwnerId || !res.Profiles[0].Current {
		t.Fatalf("profiles: %v %v", res, err)
	}
}
