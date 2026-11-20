package machine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/acp"
)

var (
	fakeACPOnce sync.Once
	fakeACPBin  string
	fakeACPErr  error
)

// fakeACP builds the scripted ACP agent once for these tests.
func fakeACP(t *testing.T) string {
	t.Helper()
	fakeACPOnce.Do(func() {
		dir, err := os.MkdirTemp("", "fakeacp")
		if err != nil {
			fakeACPErr = err
			return
		}
		fakeACPBin = filepath.Join(dir, "fakeacp")
		if runtime.GOOS == "windows" {
			fakeACPBin += ".exe"
		}
		if out, err := exec.Command("go", "build", "-o", fakeACPBin, "../provider/acp/fakeacp").CombinedOutput(); err != nil {
			fakeACPErr = err
			t.Log(string(out))
		}
	})
	if fakeACPErr != nil {
		t.Fatal(fakeACPErr)
	}
	return fakeACPBin
}

func waitAgent(t *testing.T, n *Machine, name string, ok func(*machinev1.AgentStatus) bool) *machinev1.AgentStatus {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		a := agent(n.Status(), name)
		if a != nil && ok(a) {
			return a
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: %+v", name, a)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A custom agent is kept in agents.json, its secret's value apart in the
// credential store's file; it is listed and checked at once, runs with its
// variables, comes back when the machine opens again, and goes when removed.
func TestCustomAgents(t *testing.T) {
	bin := fakeACP(t)
	dir := t.TempDir()
	n, err := Open(Config{DataDir: dir, Providers: map[string]provider.Provider{}})
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	def := &machinev1.CustomAgent{Name: "mine", Command: bin, Args: []string{"acp"}, Env: []*machinev1.AgentEnv{
		{Name: "FAKE_ACP_STATE", Value: state}, {Name: "MINE_API_KEY", Value: "s3cret"}, {Name: "MINE_PIN", Value: "1234", Secret: true}}}
	if err := n.SaveCustomAgent(def); err != nil {
		t.Fatal(err)
	}
	a := waitAgent(t, n, "mine", func(a *machinev1.AgentStatus) bool { return !a.Checking })
	if !a.Custom || !a.Available || a.Version != "1.2.3" || a.Binary != bin {
		t.Fatalf("status: %+v", a)
	}
	b, _ := os.ReadFile(filepath.Join(dir, customAgentsFile))
	// The folder as JSON writes it: on Windows its backslashes doubled.
	quoted, _ := json.Marshal(state)
	if strings.Contains(string(b), "s3cret") || strings.Contains(string(b), "1234") || !strings.Contains(string(b), strings.Trim(string(quoted), `"`)) {
		t.Fatalf("agents.json:\n%s", b)
	}
	sealed, _ := os.ReadFile(filepath.Join(dir, customSecretsFile))
	if runtime.GOOS == "windows" && strings.Contains(string(sealed), "s3cret") {
		t.Fatalf("a secret in the clear: %s", sealed)
	}
	if st, err := os.Stat(filepath.Join(dir, customSecretsFile)); err != nil || runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatalf("secrets file: %v %v", st, err)
	}
	listed := n.CustomAgents().Agents
	if len(listed) != 1 || len(listed[0].Env) != 3 || listed[0].Env[0].Value != state ||
		listed[0].Env[1].Value != "" || !listed[0].Env[1].Secret || listed[0].Env[2].Value != "" || !listed[0].Env[2].Secret {
		t.Fatalf("listed: %v", listed)
	}

	// Saved again with the secrets left empty, they keep their values.
	def.Env[1].Value, def.Env[2].Value = "", ""
	def.Args = []string{"acp", "--again"}
	if err := n.SaveCustomAgent(def); err != nil {
		t.Fatal(err)
	}
	n.Close()

	n, err = Open(Config{DataDir: dir, Providers: map[string]provider.Provider{}})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	p, _ := n.provider("mine").(*acp.Provider)
	if p == nil || strings.Join(p.Args, " ") != "acp --again" || strings.Join(p.Env, " ") != "FAKE_ACP_STATE="+state+" MINE_API_KEY=s3cret MINE_PIN=1234" {
		t.Fatalf("after opening again: %+v", p)
	}
	n.CheckAgents(true, 0)
	waitAgent(t, n, "mine", func(a *machinev1.AgentStatus) bool { return a.Available })
	res, err := n.TestCustomAgent(context.Background(), &machinev1.CustomAgent{Name: "mine", Command: bin, Env: []*machinev1.AgentEnv{{Name: "MINE_API_KEY"}}})
	if err != nil || !res.Ok || res.AgentTitle != "Fake ACP" || res.AgentVersion != "1.2.3" {
		t.Fatalf("test: %+v %v", res, err)
	}

	if err := n.RemoveCustomAgent("mine"); err != nil {
		t.Fatal(err)
	}
	if agent(n.Status(), "mine") != nil || n.provider("mine") != nil || len(n.CustomAgents().Agents) != 0 {
		t.Fatalf("still there: %v", n.Status().Agents)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, customSecretsFile)); strings.Contains(string(b), "mine") {
		t.Fatalf("its secrets stayed: %s", b)
	}
}

// What can't be saved says why, and changes nothing.
func TestCustomAgentRefused(t *testing.T) {
	dir := t.TempDir()
	n, err := Open(Config{DataDir: dir, Providers: map[string]provider.Provider{"claude": &probe{name: "claude"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	for _, c := range []struct {
		def  *machinev1.CustomAgent
		want string
	}{
		{&machinev1.CustomAgent{Name: "deepseek", Command: "x"}, "built in"},
		{&machinev1.CustomAgent{Name: "claude", Command: "x"}, "built in"},
		{&machinev1.CustomAgent{Name: "My Agent", Command: "x"}, "not one Aide can use"},
		{&machinev1.CustomAgent{Name: "mine", Command: " "}, "give the command"},
		{&machinev1.CustomAgent{Name: "mine", Command: "x", Dir: filepath.Join(dir, "nowhere")}, "not a folder"},
		{&machinev1.CustomAgent{Name: "mine", Command: "x", Env: []*machinev1.AgentEnv{{Name: "1A", Value: "x"}}}, "not a variable name"},
		{&machinev1.CustomAgent{Name: "mine", Command: "x", Env: []*machinev1.AgentEnv{{Name: "A", Value: "x"}, {Name: "A", Value: "y"}}}, "given twice"},
		{&machinev1.CustomAgent{Name: "mine", Command: "x", Env: []*machinev1.AgentEnv{{Name: "OPENAI_API_KEY"}}}, "give a value for OPENAI_API_KEY"},
	} {
		err := n.SaveCustomAgent(c.def)
		if connect.CodeOf(err) != connect.CodeInvalidArgument || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v, want %q", c.def, err, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, customAgentsFile)); err == nil {
		t.Fatal("agents.json written")
	}
	if err := n.RemoveCustomAgent("mine"); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("remove: %v", err)
	}
	// A command that does not answer as an ACP agent fails its test with why.
	res, err := n.TestCustomAgent(context.Background(), &machinev1.CustomAgent{Name: "mine", Command: "aide-no-such-agent"})
	if err != nil || res.Ok || !strings.Contains(res.Reason, "aide-no-such-agent not found") {
		t.Fatalf("test: %+v %v", res, err)
	}
}

func TestLooksSecret(t *testing.T) {
	for name, want := range map[string]bool{"OPENAI_API_KEY": true, "GITHUB_TOKEN": true, "DB_PASSWORD": true, "client_secret": true,
		"ANTHROPIC_AUTH_TOKEN": true, "FAKE_ACP_STATE": false, "HTTPS_PROXY": false, "NODE_OPTIONS": false, "PWD": false} {
		if looksSecret(name) != want {
			t.Errorf("%s: %v", name, !want)
		}
	}
}
