package machine

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/hermeshai/hermesh/gen/go/hermesh/machine/v1"
	"github.com/hermeshai/hermesh/internal/provider"
	"github.com/hermeshai/hermesh/internal/shellenv"
)

// The ops channel (docs/machine-ops.md): an agent whose grant has
// machine_exec runs one-shot commands here, in PowerShell on Windows and sh
// elsewhere, and gets their output and exit code. Each call, refused ones
// too, is kept in the ops log (the owner's devices list it) and the audit
// log; its output is kept nowhere.

const (
	execDefaultTimeout = time.Minute
	execMaxTimeout     = 10 * time.Minute
	// execMaxOutput is what a call returns of each of stdout and stderr.
	execMaxOutput = 1 << 20
	// execLogSize is how many calls the ops log keeps.
	execLogSize = 500
	// kExecLog: the ops log (machinev1.ListExecLogResponse), newest first.
	kExecLog = "exec_log"
)

// execLogMu serializes the ops log's read-modify-write.
var execLogMu sync.Mutex

type opsService struct{ n *Machine }

func (s *opsService) Exec(ctx context.Context, req *machinev1.ExecRequest) (*machinev1.ExecResponse, error) {
	return s.n.exec(ctx, principalFrom(ctx), req)
}

// exec runs req for p, if p's grant lets it.
func (n *Machine) exec(ctx context.Context, p *Principal, req *machinev1.ExecRequest) (*machinev1.ExecResponse, error) {
	rec := &machinev1.ExecRecord{Command: req.Command, Workdir: req.Workdir, StartedAt: timestamppb.Now()}
	if p != nil {
		rec.ClientId = p.ID()
		rec.ClientLabel = p.Grant.GetClientLabel()
	}
	end := func(outcome machinev1.ExecOutcome, reason string) {
		rec.EndedAt, rec.Outcome, rec.Reason = timestamppb.Now(), outcome, reason
		n.logExec(rec)
	}
	refuse := func(code connect.Code, why string) (*machinev1.ExecResponse, error) {
		end(machinev1.ExecOutcome_EXEC_OUTCOME_REFUSED, why)
		return nil, connect.NewError(code, errors.New(why))
	}
	timeout := time.Duration(req.TimeoutSeconds) * time.Second
	switch {
	case p == nil || p.Kind != PrincipalClient || !p.Grant.GetMachineExec():
		return refuse(connect.CodePermissionDenied, "this agent may not run commands on this machine: its owner turns that on for it in Aide (Settings → Access → Run commands)")
	case strings.TrimSpace(req.Command) == "":
		return refuse(connect.CodeInvalidArgument, "no command")
	case timeout > execMaxTimeout:
		return refuse(connect.CodeInvalidArgument, fmt.Sprintf("a timeout of at most %d seconds", int(execMaxTimeout.Seconds())))
	case n.isPaused():
		return refuse(connect.CodeUnavailable, "this machine is paused by its owner")
	}
	if timeout == 0 {
		timeout = execDefaultTimeout
	}
	dir, err := execDir(req.Workdir)
	if err != nil {
		end(machinev1.ExecOutcome_EXEC_OUTCOME_FAILED, err.Error())
		return nil, err
	}
	rec.Workdir = dir
	shell, args := execShell(req.Command)
	run, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stdout, stderr := &capped{}, &capped{}
	err = provider.RunCommandTo(run, shell, args, dir, shellenv.Environ(), stdout, stderr)
	res := &machinev1.ExecResponse{Stdout: stdout.String(), Stderr: stderr.String(), StdoutTruncated: stdout.cut, StderrTruncated: stderr.cut,
		StartedAt: rec.StartedAt, Shell: filepath.Base(strings.TrimSuffix(shell, ".exe")), Workdir: dir}
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		end(machinev1.ExecOutcome_EXEC_OUTCOME_FAILED, "the caller left before it ended; it was ended")
		return nil, connect.NewError(connect.CodeCanceled, ctx.Err())
	case errors.Is(run.Err(), context.DeadlineExceeded):
		res.ExitCode, res.TimedOut = -1, true
		rec.ExitCode = -1
		end(machinev1.ExecOutcome_EXEC_OUTCOME_TIMED_OUT, fmt.Sprintf("ended after %s", timeout))
	case err == nil || errors.Is(err, exec.ErrWaitDelay): // something it started in the background kept its output open
		end(machinev1.ExecOutcome_EXEC_OUTCOME_EXITED, "")
	case errors.As(err, &exit):
		res.ExitCode = int32(exit.ExitCode())
		rec.ExitCode = res.ExitCode
		end(machinev1.ExecOutcome_EXEC_OUTCOME_EXITED, "")
	default:
		end(machinev1.ExecOutcome_EXEC_OUTCOME_FAILED, err.Error())
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("could not run it: %w", err))
	}
	res.EndedAt = rec.EndedAt
	return res, nil
}

// execShell is how a command runs here: as a new terminal of the machine's
// would run it.
func execShell(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell.exe", powershellArgs(command)
	}
	return "/bin/sh", []string{"-c", command}
}

// powershellArgs run command in Windows PowerShell, its output in UTF-8
// whatever the console's code page, and without progress records, which
// PowerShell writes to a redirected stderr as CLIXML ("Preparing modules
// for first use"). It goes encoded (UTF-16LE, base64), as no quoting of a
// command line survives PowerShell's reading of it whole.
func powershellArgs(command string) []string {
	script := "$ProgressPreference = 'SilentlyContinue'\n[Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n" + command
	var b []byte
	for _, u := range utf16.Encode([]rune(script)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return []string{"-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(b)}
}

// execDir is the full path of a command's folder: "~/…" (or "~\…") from
// the home folder, empty the home folder itself.
func execDir(dir string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", connect.NewError(connect.CodeFailedPrecondition, err)
	}
	switch {
	case dir == "" || dir == "~":
		dir = home
	case strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, "~"+string(filepath.Separator)):
		dir = filepath.Join(home, dir[2:])
	case !filepath.IsAbs(dir):
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%q is not a full path (or ~/…)", dir))
	}
	dir = filepath.Clean(dir)
	st, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", connect.NewError(connect.CodeNotFound, fmt.Errorf("no folder %s", dir))
	case err != nil:
		return "", connect.NewError(connect.CodeFailedPrecondition, err)
	case !st.IsDir():
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s is not a folder", dir))
	}
	return dir, nil
}

// capped keeps the first execMaxOutput bytes written to it and drops the
// rest, so a command that writes without end costs no memory.
type capped struct {
	b   []byte
	cut bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := execMaxOutput - len(c.b); room < len(p) {
		c.b, c.cut = append(c.b, p[:max(room, 0)]...), true
	} else {
		c.b = append(c.b, p...)
	}
	return len(p), nil
}

// String is what was kept, as UTF-8: a character cut in two at the end is
// dropped, other bytes that aren't UTF-8 read as U+FFFD.
func (c *capped) String() string {
	b := c.b
	if c.cut {
		for i := 0; i < utf8.UTFMax && len(b) > 0 && !utf8.Valid(b); i++ {
			b = b[:len(b)-1]
		}
	}
	return strings.ToValidUTF8(string(b), "�")
}

// logExec keeps rec in the ops log and the audit log.
func (n *Machine) logExec(rec *machinev1.ExecRecord) {
	decision := "allow"
	if rec.Outcome == machinev1.ExecOutcome_EXEC_OUTCOME_REFUSED {
		decision = "deny"
	}
	n.audit.Log(AuditRecord{Kind: "exec", Subject: rec.ClientId, Action: rec.Command, Decision: decision, Reason: rec.Reason,
		Detail: fmt.Sprintf("outcome=%s exit=%d dir=%s", strings.ToLower(strings.TrimPrefix(rec.Outcome.String(), "EXEC_OUTCOME_")), rec.ExitCode, rec.Workdir)})
	execLogMu.Lock()
	defer execLogMu.Unlock()
	log := &machinev1.ListExecLogResponse{}
	if _, err := n.st.GetProto(kExecLog, log); err != nil {
		n.log.Warn("ops log unreadable; starting it again", "err", err)
		log.Reset()
	}
	log.Records = append([]*machinev1.ExecRecord{proto.CloneOf(rec)}, log.Records...)
	if len(log.Records) > execLogSize {
		log.Records = log.Records[:execLogSize]
	}
	if err := n.st.SetProto(kExecLog, log); err != nil {
		n.log.Warn("ops log not kept", "err", err)
	}
}

func (s *adminService) ListExecLog(context.Context, *machinev1.ListExecLogRequest) (*machinev1.ListExecLogResponse, error) {
	return s.n.ExecLog()
}

// ExecLog is the ops log, newest first.
func (n *Machine) ExecLog() (*machinev1.ListExecLogResponse, error) {
	execLogMu.Lock()
	defer execLogMu.Unlock()
	log := &machinev1.ListExecLogResponse{}
	_, err := n.st.GetProto(kExecLog, log)
	return log, err
}
