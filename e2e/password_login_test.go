package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	"github.com/hermeshai/hermesh/gen/go/hermesh/local/v1/localv1connect"
	"github.com/hermeshai/hermesh/internal/mobile"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// Logging in with an account and password (docs/password-login.md): the
// owner turns it on from the trusted device it has; a phone that never
// paired, a second computer and a new agent each log in with the account
// and password alone, with no other device approving and no codes, and see
// the owner's machines and projects; the agent that was paired by approval
// before stays as it was. Changing the password signs out every device and
// agent that logged in with the old one, and the old password no longer
// works.
func TestPasswordLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"HERMESH_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()
	home, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	server := strings.TrimPrefix(home.RelayUrl, "http://")
	// The agent paired by approval (Muse's CLI): its status before and after.
	ms := regexp.MustCompile(`\d+ ms`)
	statusOf := func(r result) string { return ms.ReplaceAllString(r.stdout, "N ms") }
	before := w.cli(30*time.Second, "auth", "status")
	if before.code != 0 {
		t.Fatalf("auth status before: %+v", before)
	}

	// Off: nothing to log in to.
	if got, err := w.app.client.GetLoginPassword(ctx, &localv1.GetLoginPasswordRequest{}); err != nil || got.Account != "" {
		t.Fatalf("password login before it is turned on: %v %v", got, err)
	}
	if _, err := w.app.client.SetLoginPassword(ctx, &localv1.SetLoginPasswordRequest{Account: "samir", Password: "short"}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a short password: %v", err)
	}
	if _, err := w.app.client.SetLoginPassword(ctx, &localv1.SetLoginPasswordRequest{Account: "Samir", Password: "correct horse battery"}); err != nil {
		t.Fatalf("turning password login on: %v", err)
	}
	got, err := w.app.client.GetLoginPassword(ctx, &localv1.GetLoginPasswordRequest{})
	if err != nil || got.Account != "samir" || len(got.Devices) != 0 {
		t.Fatalf("password login once on: %v %v", got, err)
	}

	// A phone that never paired: its core, the Android app's, no signer
	// attached at all, so nothing on it is signed with its own key and no
	// other device is asked.
	phone := newPhoneCore(t)
	phoneKey, _ := identity.GenerateKey()
	login := &localv1.PasswordLoginRequest{RelayUrl: server, Account: "samir", Password: "wrong password", RootPublicKey: identity.MarshalPublicKey(phoneKey.Public()),
		RootLabel: "Xiaomi 15 Pro", RootPlatform: "android"}
	if _, err := phone.PasswordLogin(ctx, login); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a wrong password: %v", err)
	}
	login.Password = "correct horse battery"
	if _, err := phone.PasswordLogin(ctx, login); err != nil {
		t.Fatalf("the phone logs in: %v", err)
	}
	st, err := phone.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil || !st.Initialized || st.OwnerId != home.OwnerId || st.Account != "samir" {
		t.Fatalf("the phone once logged in: %v %v", st, err)
	}
	eventually(t, 30*time.Second, "the phone sees the owner's machine and its project", func() bool {
		ms, err := phone.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(ms.Machines) != 1 || ms.Machines[0].MachineName != "owner-mac" || !ms.Machines[0].Online {
			return false
		}
		ps, err := phone.ListProjects(ctx, &localv1.ListProjectsRequest{})
		return err == nil && slices.ContainsFunc(ps.Projects, func(p *localv1.Project) bool { return p.ProjectId == "demo" })
	})
	if _, err := phone.ListSessions(ctx, &localv1.ListSessionsRequest{}); err != nil {
		t.Fatalf("the phone's sessions: %v", err)
	}
	eventually(t, 30*time.Second, "the phone among the Mac's trusted devices", func() bool {
		devices, err := w.app.client.ListDevices(ctx, &localv1.ListDevicesRequest{})
		return err == nil && slices.ContainsFunc(devices.Devices, func(d *localv1.Device) bool { return d.Label == "Xiaomi 15 Pro" && d.Platform == "android" })
	})

	// A second computer, by the app's hermeshd: logs in with the server's
	// address alone (no owner part), and runs its own machine.
	work, _ := w.otherMac("work-pc", nil)
	workKey, _ := identity.GenerateKey()
	if _, err := work.PasswordLogin(ctx, &localv1.PasswordLoginRequest{RelayUrl: server, Account: "SAMIR ", Password: "correct horse battery",
		RootPublicKey: identity.MarshalPublicKey(workKey.Public()), RootLabel: "work-pc", RootPlatform: "windows", LocalMachineName: "work-pc"}); err != nil {
		t.Fatalf("the second computer logs in: %v", err)
	}
	eventually(t, 30*time.Second, "both machines online, seen from the second computer", func() bool {
		ms, err := work.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil || len(ms.Machines) != 2 {
			return false
		}
		for _, m := range ms.Machines {
			if !m.Online {
				return false
			}
		}
		return true
	})
	got, err = w.app.client.GetLoginPassword(ctx, &localv1.GetLoginPasswordRequest{})
	slices.Sort(got.GetDevices())
	if err != nil || !slices.Equal(got.Devices, []string{"Xiaomi 15 Pro", "work-pc"}) {
		t.Fatalf("devices that logged in: %v %v", got, err)
	}

	// A new agent: `hermesh auth login --account`, the password from the
	// environment (or stdin, or asked for on a terminal).
	agentEnv := []string{"HERMESH_HOME=" + filepath.Join(w.tmp, "agent2"), "HERMESH_PASSWORD=correct horse battery"}
	agent := func(args ...string) result {
		return run(t, agentEnv, 60*time.Second, filepath.Join(w.bin, "hermesh"), args...)
	}
	if r := agent("auth", "login", "--account", "samir", "--name", "laptop-agent", server); r.code != 0 {
		t.Fatalf("the agent logs in: %+v", r)
	}
	eventually(t, 30*time.Second, "the agent sees the project", func() bool {
		r := agent("project", "list")
		return r.code == 0 && strings.Contains(r.stdout, "owner-mac/demo")
	})
	if after := w.cli(30*time.Second, "auth", "status"); after.code != 0 || statusOf(after) != statusOf(before) {
		t.Fatalf("the agent paired by approval changed:\nbefore:\n%s\nafter:\n%s", before.stdout, after.stdout)
	}

	// The password changes: the phone, the second computer (and its
	// machine) and the agent that logged in with the old one are out; the
	// Mac, its machine and the agent paired by approval stay.
	ch, err := w.app.client.SetLoginPassword(ctx, &localv1.SetLoginPasswordRequest{Account: "samir", Password: "a new password 2026"})
	if err != nil || ch.RemovedDevices != 2 {
		t.Fatalf("changing the password: %v %v", ch, err)
	}
	eventually(t, 60*time.Second, "the phone learns it was signed out", func() bool {
		st, err := phone.GetStatus(ctx, &localv1.GetStatusRequest{})
		return err == nil && st.Removed
	})
	eventually(t, 60*time.Second, "the agent that logged in is refused", func() bool {
		return agent("project", "list").code != 0
	})
	devices, err := w.app.client.ListDevices(ctx, &localv1.ListDevicesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devices.Devices {
		if d.Label == "Xiaomi 15 Pro" || d.Label == "work-pc" {
			t.Fatalf("still a trusted device after the change: %v", d)
		}
	}
	eventually(t, 30*time.Second, "the Mac's machine alone, online", func() bool {
		ms, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		return err == nil && len(ms.Machines) == 1 && ms.Machines[0].MachineName == "owner-mac" && ms.Machines[0].Online
	})
	if r := w.cli(30*time.Second, "project", "list"); r.code != 0 || !strings.Contains(r.stdout, "owner-mac/demo") {
		t.Fatalf("the agent paired by approval after the change: %+v", r)
	}
	if after := w.cli(30*time.Second, "auth", "status"); after.code != 0 || statusOf(after) != statusOf(before) {
		t.Fatalf("the agent paired by approval changed:\nbefore:\n%s\nafter:\n%s", before.stdout, after.stdout)
	}

	// The old password no longer works; the new one does (the agent pairs
	// again as a new client, its old grant revoked).
	agentEnv[1] = "HERMESH_PASSWORD=correct horse battery"
	if r := agent("auth", "login", "--account", "samir", server); r.code == 0 {
		t.Fatalf("the old password still logs in: %+v", r)
	}
	agentEnv[1] = "HERMESH_PASSWORD=a new password 2026"
	if r := agent("auth", "login", "--account", "samir", server); r.code != 0 {
		t.Fatalf("the new password: %+v", r)
	}
	eventually(t, 30*time.Second, "the agent is back", func() bool {
		return agent("project", "list").code == 0
	})

	// Off: nobody logs in with a password; what logged in with it is out.
	if _, err := w.app.client.SetLoginPassword(ctx, &localv1.SetLoginPasswordRequest{}); err != nil {
		t.Fatalf("turning password login off: %v", err)
	}
	if got, err := w.app.client.GetLoginPassword(ctx, &localv1.GetLoginPasswordRequest{}); err != nil || got.Account != "" {
		t.Fatalf("password login once off: %v %v", got, err)
	}
	eventually(t, 60*time.Second, "the agent that logged in is refused once it is off", func() bool {
		return agent("project", "list").code != 0
	})
	if r := agent("auth", "login", "--account", "samir", server); r.code == 0 {
		t.Fatalf("a password login once off: %+v", r)
	}
}

// newPhoneCore is the Android app's core (internal/mobile, which the
// gomobile binding wraps), in a fresh data directory, with no signer.
func newPhoneCore(t *testing.T) localv1connect.LocalServiceClient {
	t.Helper()
	// The core keeps its owner lock open as long as the process runs, as the
	// app's does: Windows can't remove its folder before then, so not
	// t.TempDir, whose cleanup would fail the test there.
	dir, err := os.MkdirTemp("", "phonecore")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	c, err := mobile.Start(dir, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Pause)
	hc := &http.Client{Transport: &bearer{token: c.Token(), next: http.DefaultTransport}}
	return localv1connect.NewLocalServiceClient(hc, fmt.Sprintf("http://127.0.0.1:%d", c.Port()))
}

// An agent logging in with no address goes to the owner's own server
// ($HERMESH_SERVER), whose one owner gets the request and approves it as
// any. With no address and no $HERMESH_SERVER it stops and asks for one:
// there is no default server and no official center to fall back to.
func TestLoginToOwnServer(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"HERMESH_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()
	home, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(w.bin, "hermesh")
	nowhere := []string{"HERMESH_HOME=" + filepath.Join(w.tmp, "nowhere-agent"), "HERMESH_SERVER=", "HERMESH_CENTER="}
	for _, args := range [][]string{{"auth", "login"}, {"auth", "login", "--account", "samir", "--password-stdin"}} {
		if r := run(t, nowhere, 30*time.Second, bin, args...); r.code != 2 || !strings.Contains(r.stderr, "which server?") {
			t.Fatalf("%v with no server: %+v", args, r)
		}
	}
	env := []string{"HERMESH_HOME=" + filepath.Join(w.tmp, "own-server-agent"), "HERMESH_SERVER=" + strings.TrimPrefix(home.RelayUrl, "http://"), "HERMESH_CENTER="}
	login := start(t, "login", env, bin, "auth", "login", "--name", "own-server-agent")
	if _, err := w.app.client.ApproveClientPairing(ctx, &localv1.ApproveClientPairingRequest{Code: login.pairingCode(t), Spec: &localv1.GrantSpec{}}); err != nil {
		t.Fatal(err)
	}
	if c := login.wait(t, 30*time.Second); c != 0 {
		t.Fatalf("login: %s", login.output())
	}
	if r := run(t, env, 30*time.Second, bin, "auth", "status"); r.code != 0 || !strings.Contains(r.stdout, home.OwnerId) {
		t.Fatalf("status: %+v", r)
	}
}
