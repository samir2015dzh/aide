//go:build unix

package machine

import (
	"context"
	"encoding/base64"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

func opsMachine(t *testing.T) *Machine {
	t.Helper()
	dir := t.TempDir()
	st, err := OpenStore(filepath.Join(dir, "aided.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	audit, err := OpenAudit(filepath.Join(dir, "audit"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(audit.Close)
	return &Machine{st: st, audit: audit, log: slog.New(slog.DiscardHandler), machineID: "m_1"}
}

func client(exec bool) *Principal {
	return &Principal{Kind: PrincipalClient, ClientID: "c_bot", Grant: &identityv1.Grant{ClientLabel: "bot", MachineExec: exec}}
}

// An agent whose grant has machine_exec runs a command and gets its output
// and exit code; one without it, and the owner's devices, are refused; each
// call is in the ops log, newest first, without its output.
func TestExec(t *testing.T) {
	n := opsMachine(t)
	ctx := context.Background()
	work := t.TempDir()

	res, err := n.exec(ctx, client(true), &machinev1.ExecRequest{Command: "pwd; echo out; echo err >&2; exit 3", Workdir: work})
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(work)
	if got := strings.TrimSpace(strings.SplitN(res.Stdout, "\n", 2)[0]); got != work && got != real {
		t.Errorf("ran in %q, want %q", got, work)
	}
	if !strings.Contains(res.Stdout, "out\n") || res.Stderr != "err\n" || res.ExitCode != 3 || res.TimedOut || res.Shell != "sh" || res.Workdir != work {
		t.Fatalf("exec: %+v", res)
	}
	home, _ := os.UserHomeDir()
	if res, err := n.exec(ctx, client(true), &machinev1.ExecRequest{Command: "true"}); err != nil || res.ExitCode != 0 || res.Workdir != home {
		t.Fatalf("in the home folder: %+v %v", res, err)
	}

	for _, p := range []*Principal{client(false), {Kind: PrincipalOwner}} {
		if _, err := n.exec(ctx, p, &machinev1.ExecRequest{Command: "touch " + filepath.Join(work, "ran")}); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("%s without machine_exec: %v", p.ID(), err)
		}
	}
	if _, err := os.Stat(filepath.Join(work, "ran")); err == nil {
		t.Fatal("a refused command ran")
	}
	if _, err := n.exec(ctx, client(true), &machinev1.ExecRequest{Command: "true", Workdir: filepath.Join(work, "nowhere")}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("no such folder: %v", err)
	}
	if _, err := n.exec(ctx, client(true), &machinev1.ExecRequest{Command: "true", TimeoutSeconds: 601}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("too long a timeout: %v", err)
	}
	n.paused = true
	if _, err := n.exec(ctx, client(true), &machinev1.ExecRequest{Command: "true"}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("paused: %v", err)
	}
	n.paused = false

	log, err := n.ExecLog()
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		who     string
		outcome machinev1.ExecOutcome
	}
	want := []row{
		{"c_bot", machinev1.ExecOutcome_EXEC_OUTCOME_REFUSED}, // paused
		{"c_bot", machinev1.ExecOutcome_EXEC_OUTCOME_REFUSED}, // timeout
		{"c_bot", machinev1.ExecOutcome_EXEC_OUTCOME_FAILED},  // no folder
		{"owner", machinev1.ExecOutcome_EXEC_OUTCOME_REFUSED},
		{"c_bot", machinev1.ExecOutcome_EXEC_OUTCOME_REFUSED},
		{"c_bot", machinev1.ExecOutcome_EXEC_OUTCOME_EXITED},
		{"c_bot", machinev1.ExecOutcome_EXEC_OUTCOME_EXITED},
	}
	if len(log.Records) != len(want) {
		t.Fatalf("%d records, want %d: %v", len(log.Records), len(want), log.Records)
	}
	for i, w := range want {
		r := log.Records[i]
		if r.ClientId != w.who || r.Outcome != w.outcome || r.StartedAt == nil || r.EndedAt == nil {
			t.Errorf("record %d: %v, want %v", i, r, w)
		}
	}
	if first := log.Records[len(log.Records)-1]; first.ExitCode != 3 || first.ClientLabel != "bot" || first.Workdir != work || !strings.Contains(first.Command, "exit 3") {
		t.Errorf("the first call: %v", first)
	}
	if refused := log.Records[4]; !strings.Contains(refused.Reason, "Run commands") || !strings.HasPrefix(refused.Command, "touch ") {
		t.Errorf("the refusal: %v", refused)
	}
	if n, err := VerifyAudit(filepath.Join(filepath.Dir(n.audit.dir), "audit")); err != nil || n != len(want) {
		t.Errorf("audit: %d records, %v", n, err)
	}
}

// A command that runs out of time is ended, with everything it started,
// and said to have; the log keeps it as timed out.
func TestExecTimeout(t *testing.T) {
	n := opsMachine(t)
	began := time.Now()
	res, err := n.exec(context.Background(), client(true), &machinev1.ExecRequest{Command: "echo started; sleep 30 & sleep 30", TimeoutSeconds: 1})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(began); took > 5*time.Second {
		t.Fatalf("ended after %v", took)
	}
	if !res.TimedOut || res.ExitCode != -1 || res.Stdout != "started\n" {
		t.Fatalf("timed out: %+v", res)
	}
	log, _ := n.ExecLog()
	if r := log.Records[0]; r.Outcome != machinev1.ExecOutcome_EXEC_OUTCOME_TIMED_OUT || r.ExitCode != -1 {
		t.Fatalf("logged: %v", r)
	}
}

// Output past 1 MiB a stream is dropped and said to be; what is kept is
// UTF-8.
func TestExecOutputCap(t *testing.T) {
	n := opsMachine(t)
	res, err := n.exec(context.Background(), client(true), &machinev1.ExecRequest{Command: "head -c 3000000 /dev/zero | tr '\\0' 'x'; printf 'small' >&2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stdout) != execMaxOutput || !res.StdoutTruncated || res.Stderr != "small" || res.StderrTruncated || res.ExitCode != 0 {
		t.Fatalf("stdout %d bytes (cut %v), stderr %q (cut %v), exit %d", len(res.Stdout), res.StdoutTruncated, res.Stderr, res.StderrTruncated, res.ExitCode)
	}
	c := &capped{}
	c.Write([]byte(strings.Repeat("a", execMaxOutput-1)))
	c.Write([]byte("é and more"))
	if s := c.String(); len(s) != execMaxOutput-1 || !c.cut {
		t.Fatalf("a character cut in two: %d bytes", len(s))
	}
	if s := (&capped{b: []byte("ok \xff")}).String(); s != "ok \uFFFD" {
		t.Fatalf("not UTF-8: %q", s)
	}
}

// PowerShell gets the command encoded, after lines that keep progress
// records off stderr and make its output UTF-8.
func TestPowershellArgs(t *testing.T) {
	args := powershellArgs(`Get-ChildItem "C:\Program Files" | Select -First 1; echo "日本"`)
	if len(args) != 4 || args[0] != "-NoProfile" || args[1] != "-NonInteractive" || args[2] != "-EncodedCommand" {
		t.Fatalf("args: %q", args)
	}
	b, err := base64.StdEncoding.DecodeString(args[3])
	if err != nil || len(b)%2 != 0 {
		t.Fatal(err)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	if got := string(utf16.Decode(u)); got != "$ProgressPreference = 'SilentlyContinue'\n[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n"+`Get-ChildItem "C:\Program Files" | Select -First 1; echo "日本"` {
		t.Fatalf("script: %q", got)
	}
}
