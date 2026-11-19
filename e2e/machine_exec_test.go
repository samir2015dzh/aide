package e2e

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	machinev1 "github.com/hermeshai/hermesh/gen/go/hermesh/machine/v1"
)

// TestMachineExec walks the ops channel (WO #6, docs/machine-ops.md) with
// real binaries, through the relay as an agent away from the machine uses
// it: an agent the owner hasn't allowed is refused; the owner allows it in
// Access, as the app's switch does; then it runs a command and gets its
// output and exit code, a failing one fails, one that runs too long is
// ended; and the owner's ops log lists every call, the refusal too, but no
// output.
func TestMachineExec(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"HERMESH_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()
	bin := filepath.Join(w.bin, "hermesh")
	// Through the relay, not this computer's own endpoint.
	env := append(w.cliEnv, "HERMESH_LOCAL=off")
	cli := func(args ...string) result { return run(t, env, time.Minute, bin, args...) }
	toErr := "echo err >&2"
	if runtime.GOOS == "windows" {
		toErr = "[Console]::Error.WriteLine('err')"
	}

	// Not allowed yet: refused, and told how to get it.
	r := cli("machine", "exec", "owner-mac", "--", "echo", "secret-output")
	if r.code != 3 || !strings.Contains(r.stderr, "Run commands") || strings.Contains(r.stdout, "secret-output") {
		t.Fatalf("exec without the scope: %+v", r)
	}
	doc := jsonDoc(t, cli("machine", "exec", "owner-mac", "--command", "echo hi", "-o", "json"))
	if errOf(doc)["kind"] != "denied" {
		t.Fatalf("exec without the scope, json: %v", doc)
	}

	// The owner turns Run commands on for it: its grant, with that alone changed.
	clients, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{})
	if err != nil || len(clients.Clients) != 1 || clients.Clients[0].Spec.MachineExec {
		t.Fatalf("clients: %v %v", clients, err)
	}
	bot := clients.Clients[0]
	spec := proto.CloneOf(bot.Spec)
	spec.MachineExec = true
	if _, err := w.app.client.UpdateClient(ctx, &localv1.UpdateClientRequest{GrantId: bot.GrantId, Spec: spec}); err != nil {
		t.Fatal(err)
	}
	if cs, err := w.app.client.ListClients(ctx, &localv1.ListClientsRequest{}); err != nil || !cs.Clients[0].Spec.MachineExec ||
		!proto.Equal(cs.Clients[0].ExpiresAt, bot.ExpiresAt) {
		t.Fatalf("after allowing: %v %v", cs, err)
	}

	// Allowed: output and exit code, in the folder asked for.
	eventually(t, 30*time.Second, "exec allowed", func() bool {
		r = cli("machine", "exec", "owner-mac", "--workdir", w.repo, "--", "echo hello; "+toErr)
		return r.code == 0
	})
	// As the machine's shell wrote it: PowerShell ends lines with CRLF.
	if strings.ReplaceAll(r.stdout, "\r\n", "\n") != "hello\n" || !strings.Contains(r.stderr, "err") {
		t.Fatalf("exec: %+v", r)
	}
	doc = jsonDoc(t, cli("machine", "exec", "owner-mac", "--workdir", w.repo, "--command", "echo hello", "-o", "json"))
	ex, _ := doc["exec"].(map[string]any)
	if doc["ok"] != true || lf(ex["stdout"]) != "hello\n" || ex["exit_code"] != float64(0) || ex["machine"] != "owner-mac" || ex["timed_out"] != false ||
		ex["stdout_truncated"] != false || ex["started_at"] == nil || ex["ended_at"] == nil {
		t.Fatalf("exec json: %v", doc)
	}
	if real, _ := filepath.EvalSymlinks(w.repo); ex["workdir"] != w.repo && ex["workdir"] != real {
		t.Fatalf("ran in %v, want %s", ex["workdir"], w.repo)
	}

	// A failing command fails, with its own exit code in the output.
	r = cli("machine", "exec", "owner-mac", "-o", "json", "--", "exit 7")
	doc = jsonDoc(t, r)
	if ex, _ := doc["exec"].(map[string]any); r.code != 5 || errOf(doc)["kind"] != "command_failed" || ex["exit_code"] != float64(7) {
		t.Fatalf("exit 7: %v", doc)
	}

	// One that runs too long is ended, on time.
	began := time.Now()
	r = cli("machine", "exec", "owner-mac", "--timeout", "2s", "-o", "json", "--", "echo started; sleep 30")
	doc = jsonDoc(t, r)
	if ex, _ := doc["exec"].(map[string]any); r.code != 6 || ex["timed_out"] != true || ex["exit_code"] != float64(-1) || lf(ex["stdout"]) != "started\n" {
		t.Fatalf("timeout: %v", doc)
	}
	if took := time.Since(began); took > 20*time.Second {
		t.Fatalf("the timed-out command took %v", took)
	}

	// The owner's ops log: every call, newest first, by name; no output.
	log, err := w.app.client.ListExecLog(ctx, &localv1.ListExecLogRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var got []machinev1.ExecOutcome
	for _, e := range log.Entries {
		if e.MachineName != "owner-mac" || e.Record.ClientLabel != "bot" || e.Record.ClientId != bot.ClientId || e.Record.StartedAt == nil || e.Record.EndedAt == nil {
			t.Fatalf("entry: %v", e)
		}
		got = append(got, e.Record.Outcome)
	}
	exited, refused := machinev1.ExecOutcome_EXEC_OUTCOME_EXITED, machinev1.ExecOutcome_EXEC_OUTCOME_REFUSED
	want := []machinev1.ExecOutcome{machinev1.ExecOutcome_EXEC_OUTCOME_TIMED_OUT, exited, exited, exited, refused, refused}
	// While the machine had not seen the new grant yet, eventually's tries may have been refused too.
	if len(got) < len(want) || !equalOutcomes(got[:4], want[:4]) || !equalOutcomes(got[len(got)-2:], want[4:]) {
		t.Fatalf("ops log: %v, want %v", got, want)
	}
	if e := log.Entries[1].Record; e.ExitCode != 7 || e.Command != "exit 7" {
		t.Fatalf("the failing call: %v", e)
	}
	if e := log.Entries[len(log.Entries)-1].Record; e.Command != "echo secret-output" || !strings.Contains(e.Reason, "Run commands") {
		t.Fatalf("the refusal: %v", e)
	}

	// Turned off again: refused at once, as before.
	spec.MachineExec = false
	if _, err := w.app.client.UpdateClient(ctx, &localv1.UpdateClientRequest{GrantId: bot.GrantId, Spec: spec}); err != nil {
		t.Fatal(err)
	}
	eventually(t, 30*time.Second, "exec refused again", func() bool {
		return cli("machine", "exec", "owner-mac", "--", "echo", "hi").code == 3
	})
}

func errOf(doc map[string]any) map[string]any {
	e, _ := doc["error"].(map[string]any)
	return e
}

func equalOutcomes(a, b []machinev1.ExecOutcome) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// lf is a command's output with PowerShell's CRLF line ends as LF: machine
// exec passes the output on as the machine's shell wrote it.
func lf(v any) string {
	s, _ := v.(string)
	return strings.ReplaceAll(s, "\r\n", "\n")
}
