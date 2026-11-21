package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	"github.com/samir2015dzh/aide/internal/connaddr"
)

// ownerMac is an owner Mac (the owner daemon, also running its local machine
// with project "demo") behind a real relay, and one paired agent CLI.
type ownerMac struct {
	t          *testing.T
	bin, tmp   string
	aided      string // the owner's aided when not bin's, from its next start
	repo       string
	daemonEnv  []string
	daemonArgs []string
	cliEnv     []string
	owner      *proc
	app        *testApp
	sock       string
	logs       []*proc
	relay      *proc
	relayArgs  []string
	relayEnv   []string
	relayDebug string // where the relay serves /metrics
	ownerDebug string // where the owner's aided serves /metrics
	vpsDebug   string // where vps-1 (farOwnerWithVPS) serves /metrics
	repoHead   string // the owner's HEAD, which sessions must never move
	addr       string // the owner's server, as a connection address (any login takes it)
	server     string // the owner's server
	// nproc, when set, is the owner daemon's limit on the user's processes
	// (ulimit -u): it then can't start programs once they reach it.
	nproc int
}

// newOwnerMac starts it; daemonEnv and daemonArgs configure the owner
// daemon, grant is the agent's scope (nil or empty: what logging in gives).
func newOwnerMac(t *testing.T, daemonEnv []string, grant *localv1.GrantSpec, daemonArgs ...string) *ownerMac {
	t.Helper()
	return newOwnerMacAt(t, 0, daemonEnv, grant, daemonArgs...)
}

// newFarOwnerMac is newOwnerMac with the relay oneWay away from everyone
// (delayProxy).
func newFarOwnerMac(t *testing.T, oneWay time.Duration, daemonEnv []string, grant *localv1.GrantSpec) *ownerMac {
	t.Helper()
	return newOwnerMacAt(t, oneWay, daemonEnv, grant)
}

func newOwnerMacAt(t *testing.T, oneWay time.Duration, daemonEnv []string, grant *localv1.GrantSpec, daemonArgs ...string) *ownerMac {
	t.Helper()
	w := &ownerMac{t: t, bin: binaries(t), tmp: t.TempDir(), daemonArgs: daemonArgs}
	w.ownerDebug = fmt.Sprintf("127.0.0.1:%d", freePort(t))
	w.daemonEnv = append(slices.Clone(daemonEnv), "AIDE_DEBUG_ADDR="+w.ownerDebug)
	sockDir, _ := os.MkdirTemp(shortTemp, "hm")
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	w.sock = filepath.Join(sockDir, "o.sock")
	t.Cleanup(func() {
		if t.Failed() {
			for _, p := range w.logs {
				t.Logf("==== %s output ====\n%s", p.name, p.output())
			}
		}
	})
	ctx := context.Background()
	relayBin := filepath.Join(w.bin, "aide-relay")
	relayData := filepath.Join(w.tmp, "relay")
	// freePort's port is free when it answers, not when the relay binds it:
	// another test can take it in between ("bind: address already in use").
	// Nothing knows the relay's address yet, so it starts again on another.
	var port int
	for try := 1; ; try++ {
		port = freePort(t)
		w.relayDebug = fmt.Sprintf("127.0.0.1:%d", freePort(t))
		w.relayArgs = []string{"serve", "--data-dir", relayData, "--listen", fmt.Sprintf("127.0.0.1:%d", port), "--debug-addr", w.relayDebug}
		if w.runRelay() {
			break
		}
		if try == 3 {
			t.Fatalf("relay did not start:\n%s", w.relay.output())
		}
		t.Logf("relay did not start on 127.0.0.1:%d, again on another port:\n%s", port, w.relay.output())
	}
	setupCode := strings.TrimSpace(run(t, nil, 10*time.Second, relayBin, "setup-code", "--data-dir", relayData).stdout)

	w.startOwner()
	w.app = newTestApp(t, w.sock)
	relayURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if oneWay > 0 {
		relayURL = "http://" + delayProxy(t, relayURL[len("http://"):], oneWay)
	}
	setup, err := w.app.client.Setup(ctx, &localv1.SetupRequest{RelayUrl: relayURL, SetupCode: setupCode, Account: testAccount, Password: testPassword, Label: "test-mac"})
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, w.owner.output())
	}
	w.server, w.addr = relayURL, connaddr.Format(relayURL, setup.OwnerId)
	localMachine, err := w.app.client.SetLocalMachine(ctx, &localv1.SetLocalMachineRequest{Enabled: true, MachineName: "owner-mac"})
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "machine online", func() bool {
		res, err := w.app.client.ListMachines(ctx, &localv1.ListMachinesRequest{})
		return err == nil && len(res.Machines) == 1 && res.Machines[0].Online && res.Machines[0].Status != nil
	})
	w.repo = gitRepo(t, map[string]string{"README.md": "# demo\n"})
	w.repoHead = gitOut(t, w.repo, "rev-parse", "HEAD")
	if _, err := w.app.client.AddProject(ctx, &localv1.AddProjectRequest{MachineId: localMachine.MachineId, ProjectId: "demo", Path: w.repo}); err != nil {
		t.Fatal(err)
	}
	w.cliEnv = []string{"AIDE_HOME=" + filepath.Join(w.tmp, "agent")}
	w.loginAgent(w.cliEnv, "bot", grant)
	return w
}

// loginAgent logs an agent in with the owner's account (env names its
// home), and gives it spec's scope when spec says more than the defaults.
// It returns the agent's grant id.
func (w *ownerMac) loginAgent(env []string, name string, spec *localv1.GrantSpec) string {
	w.t.Helper()
	if r := run(w.t, append(slices.Clone(env), loginEnv()...), 60*time.Second, filepath.Join(w.bin, "aide"), "auth", "login", w.server, "--name", name); r.code != 0 {
		w.t.Fatalf("login %s: %+v", name, r)
	}
	ctx := context.Background()
	var grantID string
	eventually(w.t, 30*time.Second, "the agent "+name+" listed", func() bool {
		cs, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
		for _, c := range cs.GetClients() {
			if err == nil && c.Label == name {
				grantID = c.GrantId
			}
		}
		return grantID != ""
	})
	if spec != nil && !proto.Equal(spec, &localv1.GrantSpec{}) {
		spec = proto.Clone(spec).(*localv1.GrantSpec)
		if spec.Label == "" {
			spec.Label = name
		}
		if _, err := w.app.client.UpdateClient(ctx, &localv1.UpdateClientRequest{GrantId: grantID, Spec: spec}); err != nil {
			w.t.Fatalf("the agent %s's scope: %v", name, err)
		}
	}
	return grantID
}

// loginMachine logs a machine in with the owner's account: aided login,
// with its data in dataDir and its name, no service; env and args add to
// it. It returns the machine's id.
func (w *ownerMac) loginMachine(name, dataDir string, env []string, args ...string) string {
	w.t.Helper()
	r := run(w.t, append(slices.Clone(env), loginEnv()...), 60*time.Second, filepath.Join(w.bin, "aided"),
		append([]string{"login", w.server, "--name", name, "--no-service", "--data-dir", dataDir}, args...)...)
	if r.code != 0 {
		w.t.Fatalf("aided login %s: %+v", name, r)
	}
	id := loggedInMachine(r.stdout)
	if id == "" {
		w.t.Fatalf("aided login %s said no machine id:\n%s", name, r.stdout)
	}
	return id
}

// loggedInMachine is the machine id `aided login` printed ("✓ Logged in as
// “name” (m_…)"), "" before it did.
func loggedInMachine(out string) string {
	if m := regexp.MustCompile(`Logged in as .*\((m_[a-z0-9]+)\)`).FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

func (w *ownerMac) startRelay() {
	if !w.runRelay() {
		w.t.Fatalf("relay did not start:\n%s", w.relay.output())
	}
}

// runRelay starts the relay and waits until it is listening; false if it
// exits first.
func (w *ownerMac) runRelay() bool {
	w.relay = start(w.t, "relay", w.relayEnv, filepath.Join(w.bin, "aide-relay"), w.relayArgs...)
	w.logs = append(w.logs, w.relay)
	r := w.relay
	for end := time.Now().Add(20 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if strings.Contains(r.output(), "relay listening") {
			return true
		}
		select {
		case <-r.done:
			return false
		default:
		}
	}
	return false
}

// restartRelay stops the relay and starts it again on the same address.
func (w *ownerMac) restartRelay() {
	w.relay.stop()
	w.startRelay()
}

// worktree is where this Mac's machine, in the current profile, keeps a
// session's worktree.
func (w *ownerMac) worktree(id string) string {
	cur, _ := os.ReadFile(filepath.Join(w.tmp, "owner-mac", "owner", "current"))
	return filepath.Join(w.tmp, "owner-mac", "owner", "profiles", string(cur), "machine", "worktrees", id)
}

func (w *ownerMac) startOwner() {
	args := append([]string{"run", "--owner", "--data-dir", filepath.Join(w.tmp, "owner-mac"), "--socket", w.sock}, w.daemonArgs...)
	bin := filepath.Join(w.bin, "aided")
	if w.aided != "" {
		bin = w.aided
	}
	if w.nproc > 0 {
		args = append([]string{"-c", fmt.Sprintf(`ulimit -u %d && exec "$0" "$@"`, w.nproc), bin}, args...)
		bin = "/bin/sh"
	}
	w.owner = start(w.t, "owner", w.daemonEnv, bin, args...)
	w.logs = append(w.logs, w.owner)
}

// restartOwner stops the owner daemon (gracefully, or with kill -9) and
// starts it again, waiting until the machine is back online.
func (w *ownerMac) restartOwner(kill bool) {
	if kill {
		w.owner.kill()
	} else {
		w.owner.stop()
	}
	w.app.cancel()
	w.startOwner()
	w.app = newTestApp(w.t, w.sock)
	eventually(w.t, 30*time.Second, "machine back", func() bool {
		r := w.cli(30*time.Second, "project", "list")
		return r.code == 0 && strings.Contains(r.stdout, "owner-mac/demo")
	})
}

// signs is how often the owner's devices had the server sign something
// (OwnerControl.Sign), as the relay counts it.
func (w *ownerMac) signs() int {
	return metric(w.t, w.relayDebug, `aide_relay_owner_rpcs_total{method="Sign"}`)
}

func (w *ownerMac) cli(timeout time.Duration, args ...string) result {
	return run(w.t, w.cliEnv, timeout, filepath.Join(w.bin, "aide"), args...)
}

// sessionJSON is the part of the contract's session the tests read.
type sessionJSON struct {
	SessionID   string `json:"session_id"`
	Status      string `json:"status"`
	Title       string `json:"title"`
	Ref         string `json:"agent_session_id"`
	Result      string `json:"result"`
	LastFailure *struct {
		Kind      string `json:"kind"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
		Hint      string `json:"hint"`
	} `json:"last_failure"`
	// Its state and freshness (docs/session-states.md).
	State        string  `json:"state"`
	Link         string  `json:"link"`
	StateText    string  `json:"state_text"`
	Stale        bool    `json:"stale"`
	HeartbeatAge *int64  `json:"heartbeat_age_seconds"`
	LastEventAt  *string `json:"last_event_at"`
	StalledFor   int64   `json:"stalled_seconds"`
}

// startSession runs `aide session start --project demo -o json args...`.
func (w *ownerMac) startSession(timeout time.Duration, args ...string) (sessionJSON, result) {
	r := w.cli(timeout, append([]string{"session", "start", "--project", "demo", "-o", "json"}, args...)...)
	return firstSession(r.stdout), r
}

// firstSession decodes the session of a command's JSON document.
func firstSession(out string) sessionJSON {
	var doc struct{ Session sessionJSON }
	json.Unmarshal([]byte(out), &doc)
	return doc.Session
}

// unmarshalSession decodes the session of a command's JSON document into v.
func unmarshalSession(out string, v any) error {
	var doc struct{ Session json.RawMessage }
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		return err
	}
	return json.Unmarshal(doc.Session, v)
}

// send runs `aide session send id --wait -o json --message text`.
func (w *ownerMac) send(timeout time.Duration, id, text string) (sessionJSON, result) {
	r := w.cli(timeout, "session", "send", id, "--wait", "-o", "json", "--message", text)
	return firstSession(r.stdout), r
}

func (w *ownerMac) get(id string) sessionJSON {
	return firstSession(w.cli(30*time.Second, "session", "get", id, "-o", "json").stdout)
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
