package e2e

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	"github.com/samir2015dzh/aide/gen/go/aide/local/v1/localv1connect"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// TestMachineOnlyMac: a Mac logs in as a machine only (a work Mac), with
// the account and password, no one approving. Agents run sessions on it;
// its own app sees only this Mac's projects and sessions, knows the agents
// by name, adds projects, answers and stops sessions as one of the owner's
// devices would. Removed by the owner, it says so and leaves.
func TestMachineOnlyMac(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	env := []string{"AIDE_GEMINI_BIN=" + filepath.Join(binaries(t), "fakeacp"), "FAKE_ACP_STATE=" + filepath.Join(t.TempDir(), "fakeacp")}
	w := newOwnerMac(t, env, &localv1.GrantSpec{Agents: []string{"gemini"}}) // all machines, as the app's default
	ctx := context.Background()
	home, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}

	// The work Mac: the app's aided, nobody's yet. A wrong password
	// makes it nothing.
	work, _ := w.otherMac("work-mac", env)
	if _, err := work.LogIn(ctx, &localv1.LogInRequest{RelayUrl: home.RelayUrl, Account: testAccount, Password: "not the password", Label: "Work Mac", MachineOnly: true}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a wrong password: %v", err)
	}
	st := w.logInMachineOnly(work, home, "Work Mac")
	if st.Initialized || st.LocalMachineName != "work-mac" || st.DeviceKeyId != "" {
		t.Fatalf("work Mac status: %v", st)
	}
	eventually(t, 30*time.Second, "the owner lists the work Mac online", func() bool {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		for _, m := range res.GetMachines() {
			if m.MachineId == st.LocalMachineId {
				return err == nil && m.Online && m.MachineName == "work-mac" && m.DeviceKeyId == ""
			}
		}
		return false
	})

	// Its own app: this Mac only, a project added here, nothing to approve.
	repo := gitRepo(t, map[string]string{"README.md": "# billing\n"})
	if _, err := work.AddProject(ctx, &localv1.AddProjectRequest{MachineId: st.LocalMachineId, ProjectId: "billing", Path: repo}); err != nil {
		t.Fatalf("add a project on the work Mac: %v", err)
	}
	ms, err := work.ListMachines(ctx, &localv1.ListMachinesRequest{})
	if err != nil || len(ms.Machines) != 1 || !ms.Machines[0].Local || !ms.Machines[0].Online || ms.Machines[0].Status == nil {
		t.Fatalf("the work Mac's machines: %v %v", ms, err)
	}
	if ps, err := work.ListProjects(ctx, &localv1.ListProjectsRequest{}); err != nil || len(ps.Projects) != 1 || ps.Projects[0].ProjectId != "billing" {
		t.Fatalf("the work Mac's projects: %v %v", ps, err)
	}
	if _, err := work.ListDevices(ctx, &localv1.ListDevicesRequest{}); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("a machine only lists the owner's devices: %v", err)
	}
	watch, err := work.WatchSessions(ctx, &localv1.WatchSessionsRequest{})
	if err != nil || !watch.Receive() || !watch.Msg().MachinesChanged {
		t.Fatalf("watch sessions: %v", err)
	}
	defer watch.Close()

	// Who may start sessions here, before any came: the owner's agent, granted
	// all machines, as its signed grant says, scoped to this Mac alone.
	var bot *localv1.Client
	eventually(t, 30*time.Second, "the work Mac lists the grant that covers it", func() bool {
		cs, err := work.ListClients(ctx, &localv1.ListClientsRequest{})
		if err != nil || len(cs.Clients) != 1 {
			return false
		}
		bot = cs.Clients[0]
		return true
	})
	if bot.Label != "bot" || bot.Revoked || bot.ExpiresAt == nil || bot.LastSeen != nil || !slices.Equal(bot.Spec.GetMachineIds(), []string{st.LocalMachineId}) {
		t.Fatalf("the grant the work Mac lists: %v", bot)
	}

	// The agent, granted all machines, runs a session there.
	eventually(t, time.Minute, "the agent sees the work Mac's project", func() bool {
		r := w.cli(30*time.Second, "project", "list")
		return r.code == 0 && strings.Contains(r.stdout, "work-mac/billing")
	})
	r := w.cli(2*time.Minute, "session", "start", "--project", "work-mac/billing", "--agent", "gemini", "--wait", "-o", "json", "--prompt", "ask 选哪个颜色|红,蓝")
	s := firstSession(r.stdout)
	if s.SessionID == "" {
		t.Fatalf("start on the work Mac: %+v", r)
	}
	var q *sessionv1.Question
	eventually(t, time.Minute, "the work Mac's app sees the session and its question", func() bool {
		res, err := work.ListSessions(ctx, &localv1.ListSessionsRequest{})
		for _, x := range res.GetSessions() {
			if x.SessionId == s.SessionID {
				q = x.Progress.GetPendingQuestion()
			}
		}
		return err == nil && q != nil
	})
	if !watch.Receive() || len(watch.Msg().Sessions) == 0 {
		t.Fatal("the watch did not report the session")
	}
	// It knows the agent by the name its grant gave it, and when it came.
	if cs, err := work.ListClients(ctx, &localv1.ListClientsRequest{}); err != nil || len(cs.Clients) != 1 || cs.Clients[0].Label != "bot" || cs.Clients[0].LastSeen == nil {
		t.Fatalf("the agents the work Mac knows: %v %v", cs, err)
	}
	// Its own sessions are its to settle, as on a trusted device: it answers…
	if _, err := work.AnswerQuestion(ctx, &localv1.AnswerQuestionRequest{SessionId: s.SessionID, QuestionId: q.QuestionId,
		Answers: []*sessionv1.Answer{{ItemId: q.Items[0].Id, Selected: []string{"红"}}}}); err != nil {
		t.Fatalf("answer on the work Mac: %v", err)
	}
	eventually(t, time.Minute, "the turn finishes with the answer", func() bool {
		g := w.get(s.SessionID)
		return g.Status == "idle" && strings.Contains(g.Result, "answer: v-红")
	})
	// …and stops.
	long := firstSession(w.cli(2*time.Minute, "session", "start", "--project", "work-mac/billing", "--agent", "gemini", "-o", "json", "--prompt", "shell sleep 1800").stdout)
	eventually(t, time.Minute, "the long session runs", func() bool { return w.get(long.SessionID).Status == "running" })
	if _, err := work.InterruptSession(ctx, &localv1.InterruptSessionRequest{SessionId: long.SessionID}); err != nil {
		t.Fatalf("stop the session on the work Mac: %v", err)
	}
	eventually(t, time.Minute, "the session stopped", func() bool { return w.get(long.SessionID).Status == "idle" })

	// The owner narrows the grant to its own Mac: the work Mac no longer
	// lists it as one that may start sessions here (only by name, for the
	// sessions it started); widened again, it is back.
	own, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	scope := func(machines ...string) {
		t.Helper()
		oc, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(oc.Clients, func(c *localv1.Client) bool { return c.GrantId == bot.GrantId })
		spec := proto.Clone(oc.Clients[i].Spec).(*localv1.GrantSpec)
		spec.MachineIds = machines
		if _, err := w.app.client.UpdateClient(ctx, &localv1.UpdateClientRequest{GrantId: bot.GrantId, Spec: spec}); err != nil {
			t.Fatalf("change the grant's machines to %v: %v", machines, err)
		}
	}
	covered := func() *bool {
		cs, err := work.ListClients(ctx, &localv1.ListClientsRequest{})
		if err != nil || len(cs.Clients) != 1 {
			return nil
		}
		v := !cs.Clients[0].Revoked
		return &v
	}
	scope(own.LocalMachineId)
	eventually(t, 30*time.Second, "the narrowed grant no longer covers the work Mac", func() bool { c := covered(); return c != nil && !*c })
	scope("*")
	eventually(t, 30*time.Second, "the widened grant covers it again", func() bool { c := covered(); return c != nil && *c })

	// The owner removes it: the work Mac says so, and leaves.
	if _, err := w.app.client.RemoveMachine(ctx, &localv1.RemoveMachineRequest{MachineId: st.LocalMachineId}); err != nil {
		t.Fatal(err)
	}
	eventually(t, time.Minute, "the work Mac learns it was removed", func() bool {
		st, err := work.GetStatus(ctx, &localv1.GetStatusRequest{})
		return err == nil && st.MachineOnly && st.Removed
	})
	if _, err := work.Leave(ctx, &localv1.LeaveRequest{}); err != nil {
		t.Fatal(err)
	}
	if st, err := work.GetStatus(ctx, &localv1.GetStatusRequest{}); err != nil || st.MachineOnly || st.Initialized || st.Removed {
		t.Fatalf("after leaving: %v %v", st, err)
	}
	listed := func(id string) *localv1.Machine {
		res, _ := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		for _, m := range res.GetMachines() {
			if m.MachineId == id {
				return m
			}
		}
		return nil
	}

	// It logs in to the same owner again, under a new id (a removed one
	// never comes back).
	again := w.logInMachineOnly(work, home, "Work Mac")
	if again.LocalMachineId == st.LocalMachineId {
		t.Fatal("it joined again under the id that was removed")
	}
	eventually(t, 30*time.Second, "it is back online", func() bool { m := listed(again.LocalMachineId); return m != nil && m.Online })
	// It leaves by itself, and so leaves the owner's machines too.
	if _, err := work.Leave(ctx, &localv1.LeaveRequest{}); err != nil {
		t.Fatal(err)
	}
	// The owner's Mac hears of it at its next poll of the relay.
	eventually(t, 15*time.Second, "a Mac that left gone from the owner's machines", func() bool { return listed(again.LocalMachineId) == nil })

	// A Mac called like one the owner has gets the next free name.
	typed, typedSock := w.otherMac("typed-mac", env)
	typedSt := w.logInMachineOnly(typed, home, "Owner Mac")
	eventually(t, 30*time.Second, "it is named owner-mac-2", func() bool {
		m := listed(typedSt.LocalMachineId)
		return m != nil && m.MachineName == "owner-mac-2"
	})

	// It becomes one of the owner's devices too, logging in as any Mac
	// does; its machine stays.
	if _, err := typed.LogIn(ctx, &localv1.LogInRequest{RelayUrl: home.RelayUrl, Account: testAccount, Password: testPassword, Label: "typed-mac", Platform: "macos"}); err != nil {
		t.Fatal(err)
	}
	_ = typedSock
	up, err := typed.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil || !up.Initialized || up.MachineOnly || !up.LocalMachineEnabled || up.LocalMachineId != typedSt.LocalMachineId {
		t.Fatalf("after becoming one of the owner's devices: %v %v", up, err)
	}
	if ms, err := typed.ListMachines(ctx, &localv1.ListMachinesRequest{}); err != nil || len(ms.Machines) != 2 {
		t.Fatalf("one of the owner's devices lists the owner's machines: %v %v", ms, err)
	}
	// Its machine is now that device's own: removing the device removes it too.
	eventually(t, 30*time.Second, "its machine is the device's own", func() bool {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		for _, m := range res.GetMachines() {
			if m.MachineId == typedSt.LocalMachineId {
				return err == nil && m.DeviceKeyId == up.DeviceKeyId && m.Online
			}
		}
		return false
	})
	if _, err := w.app.client.RemoveDevice(ctx, &localv1.RemoveDeviceRequest{KeyId: up.DeviceKeyId}); err != nil {
		t.Fatal(err)
	}
	ms, err = w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
	if err != nil || slices.ContainsFunc(ms.Machines, func(m *localv1.Machine) bool { return m.MachineId == typedSt.LocalMachineId }) {
		t.Fatalf("the removed device's machine is still listed: %v %v", ms, err)
	}
}

// logInMachineOnly logs mac in to the owner of home as a machine only,
// named label, with the account and password.
func (w *ownerMac) logInMachineOnly(mac localv1connect.LocalServiceClient, home *localv1.GetStatusResponse, label string) *localv1.GetStatusResponse {
	t := w.t
	t.Helper()
	ctx := context.Background()
	if _, err := mac.LogIn(ctx, &localv1.LogInRequest{RelayUrl: home.RelayUrl, Account: testAccount, Password: testPassword, Label: label, MachineOnly: true}); err != nil {
		t.Fatal(err)
	}
	st, err := mac.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil || !st.MachineOnly || st.OwnerId != home.OwnerId {
		t.Fatalf("status once logged in: %v %v", st, err)
	}
	return st
}

// otherMac starts another Mac's aided (as the app runs it) that belongs
// to nobody yet, and returns its local API and socket.
func (w *ownerMac) otherMac(name string, env []string) (localv1connect.LocalServiceClient, string) {
	w.t.Helper()
	sockDir, _ := os.MkdirTemp(shortTemp, "hm")
	w.t.Cleanup(func() { os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "o.sock")
	p := start(w.t, "owner "+name, env, filepath.Join(w.bin, "aided"), "run", "--owner", "--data-dir", filepath.Join(w.tmp, name), "--socket", sock)
	w.logs = append(w.logs, p)
	return localClient(w.t, sock), sock
}

// A machine offline while the relay restarts keeps its name and when it was
// last online: the new relay hears of it only once the machine is back, and
// until then the owner shows it as it last had them, never by its id.
func TestMachineNameAfterRelayRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	env := []string{"AIDE_GEMINI_BIN=" + filepath.Join(binaries(t), "fakeacp"), "FAKE_ACP_STATE=" + filepath.Join(t.TempDir(), "fakeacp")}
	w := newOwnerMac(t, env, &localv1.GrantSpec{Agents: []string{"gemini"}})
	ctx := context.Background()
	home, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	work, _ := w.otherMac("work-mac", env)
	daemon := w.logs[len(w.logs)-1] // the one otherMac started
	id := w.logInMachineOnly(work, home, "work-mac").LocalMachineId
	listed := func() *localv1.Machine {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil {
			return nil
		}
		for _, m := range res.Machines {
			if m.MachineId == id {
				return m
			}
		}
		return nil
	}
	eventually(t, 30*time.Second, "work-mac listed online by its name", func() bool {
		m := listed()
		return m != nil && m.Online && m.MachineName == "work-mac"
	})

	daemon.stop()
	var gone *localv1.Machine
	eventually(t, 30*time.Second, "work-mac offline", func() bool {
		gone = listed()
		return gone != nil && !gone.Online && gone.LastSeen != nil
	})
	w.restartRelay()
	eventually(t, 60*time.Second, "this Mac back on the new relay", func() bool {
		r := w.cli(30*time.Second, "project", "list")
		return r.code == 0 && strings.Contains(r.stdout, "owner-mac/demo")
	})
	// The new relay lists work-mac without a name for as long as it is away.
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(500 * time.Millisecond) {
		if m := listed(); m == nil || m.Online || m.MachineName != "work-mac" || !proto.Equal(m.LastSeen, gone.LastSeen) {
			t.Fatalf("work-mac after the relay restarted: %v (before: %v)", m, gone)
		}
	}

	// Online when the relay goes, and not back after: it was online until
	// about then, and stays so as the list comes again.
	w.otherMac("work-mac", env) // the same Mac started again
	daemon = w.logs[len(w.logs)-1]
	eventually(t, 60*time.Second, "work-mac back online", func() bool {
		m := listed()
		return m != nil && m.Online && m.MachineName == "work-mac"
	})
	w.relay.stop()
	went := time.Now()
	daemon.stop()
	w.startRelay()
	eventually(t, 60*time.Second, "this Mac back on the new relay", func() bool {
		r := w.cli(30*time.Second, "project", "list")
		return r.code == 0 && strings.Contains(r.stdout, "owner-mac/demo")
	})
	var after *localv1.Machine
	eventually(t, 30*time.Second, "work-mac listed offline by the new relay", func() bool {
		after = listed()
		return after != nil && !after.Online
	})
	if after.MachineName != "work-mac" || after.LastSeen == nil || after.LastSeen.AsTime().Before(went.Add(-time.Second)) {
		t.Fatalf("work-mac, online when the relay went: %v (the relay went at %v)", after, went)
	}
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(500 * time.Millisecond) {
		if m := listed(); m == nil || m.Online || !proto.Equal(m.LastSeen, after.LastSeen) {
			t.Fatalf("work-mac's last online moved: %v (was %v)", m, after)
		}
	}
}

// TestLogInAgainAfterAWrongPassword: a device that runs a machine, refused
// for a wrong password, logs in with the right one at once: the machine
// opened for the first try was let go of (its data directory not left in
// use).
func TestLogInAgainAfterAWrongPassword(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, nil, &localv1.GrantSpec{})
	ctx := context.Background()
	home, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := w.otherMac("mac2", nil)
	req := &localv1.LogInRequest{RelayUrl: home.RelayUrl, Account: testAccount, Password: "not the password", Label: "mac2", Platform: "macos", LocalMachineName: "mac2"}
	if _, err := c.LogIn(ctx, req); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a wrong password: %v", err)
	}
	req.Password = testPassword
	if _, err := c.LogIn(ctx, req); err != nil {
		t.Fatalf("the right password after a wrong one: %v", err)
	}
	eventually(t, time.Minute, "mac2's machine online", func() bool {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		return err == nil && slices.ContainsFunc(res.Machines, func(m *localv1.Machine) bool { return m.MachineName == "mac2" && m.Online })
	})
}
