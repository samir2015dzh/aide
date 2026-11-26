package service

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/samir2015dzh/aide/internal/legacy"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/samir2015dzh/aide/internal/brandenv"
	"github.com/samir2015dzh/aide/internal/flock"
	"github.com/samir2015dzh/aide/internal/paths"
	"github.com/samir2015dzh/aide/internal/resident"
)

// On Windows aided runs from a Task Scheduler task of this user's, at
// logon, so no administrator is needed (docs/windows.md §4.4). The task
// runs aidedw.exe, aided's windowless twin (a console program
// started at logon would open a console window), with --supervise: Task
// Scheduler restarts a task that fails to start, not a program that exits,
// so the supervisor restarts the daemon itself.

// taskName is this user's task: tasks share one namespace per computer. An
// installation from before the rename keeps its task's name while it has
// that task (internal/legacy).
func taskName() string {
	taskOnce.Do(func() {
		u := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`\/:*?"<>|`, r) {
				return '_'
			}
			return r
		}, os.Getenv("USERNAME"))
		task = newTask + " (" + u + ")"
		if old := legacy.LogonTask + " (" + u + ")"; schtasks("/Query", "/TN", old) == nil {
			task = old
		}
	})
	return task
}

var (
	taskOnce sync.Once
	task     string
)

// definition is where Install keeps the task it registered for dataDir: in
// dataDir, so that installing the service makes no directory the data
// directory's choice has not made (2026-11-20: a fixed %LocalAppData%\Aide
// here made the new data directory before the first start, and the one from
// before the rename was left behind).
func definition(dataDir string) string {
	return filepath.Join(dataDir, "service", "aided.xml")
}

// definitions are the places a task's definition for dataDir may be: its
// own, where an installation from before the rename kept it, and where the
// first build after the rename put it, whatever the data directory.
func definitions(dataDir string) []string {
	cache, _ := os.UserCacheDir() // %LocalAppData%
	return []string{definition(dataDir), filepath.Join(cache, legacy.DataDirTitle, "service", legacy.Daemon+".xml"), filepath.Join(cache, "Aide", "service", "aided.xml")}
}

// stray removes what the first build after the rename made outside the data
// directory: its task definition, and the folders that only held it.
func stray(dataDir string) {
	cache, _ := os.UserCacheDir()
	dir := filepath.Join(cache, "Aide")
	if strings.EqualFold(filepath.Clean(dir), filepath.Clean(dataDir)) {
		return
	}
	os.Remove(filepath.Join(dir, "service", "aided.xml"))
	os.Remove(filepath.Join(dir, "service")) // only while empty
	os.Remove(dir)                           // the same
}

// windowless is aidedw.exe next to bin, when there is one.
func windowless(bin string) string {
	w := filepath.Join(filepath.Dir(bin), "aidedw.exe")
	if _, err := os.Stat(w); err == nil {
		return w
	}
	return bin
}

func schtasks(args ...string) error {
	cmd := exec.Command("schtasks", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks %s: %v: %s", args[0], err, bytes.TrimSpace(out))
	}
	return nil
}

// Install registers and starts the task running `bin run`, with args
// before its data directory (a trusted device: --owner --no-machine). The
// daemon writes its own log, under dataDir.
func Install(bin, dataDir, logDir string, args ...string) error {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	argv := append(append([]string{"run", "--supervise"}, args...), "--data-dir", dataDir)
	stray(dataDir)
	path := definition(dataDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, utf16File(taskXML(windowless(bin), argv, u.User.Sid.String())), 0o600); err != nil {
		return err
	}
	schtasks("/End", "/TN", taskName()) // an older one, still running
	if err := schtasks("/Create", "/TN", taskName(), "/XML", path, "/F"); err != nil {
		return err
	}
	return schtasks("/Run", "/TN", taskName())
}

func Uninstall() error {
	schtasks("/End", "/TN", taskName())
	for _, d := range definitions(paths.DataDir()) {
		os.Remove(d)
	}
	if !Installed() {
		return nil
	}
	return schtasks("/Delete", "/TN", taskName(), "/F")
}

// Restart restarts the installed task, and with it the daemon.
func Restart() error {
	schtasks("/End", "/TN", taskName())
	return schtasks("/Run", "/TN", taskName())
}

// taskState is the installed task as Task Scheduler has it: its state
// (Ready, Running, Disabled…: the enum's names, not the system's language),
// the program it starts, and how its last run ended.
type taskState struct {
	state, program string
	lastResult     uint32
	lastRun        string
}

func queryTask() (taskState, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"$t = Get-ScheduledTask -TaskName '"+strings.ReplaceAll(taskName(), "'", "''")+"' -ErrorAction Stop; $i = $t | Get-ScheduledTaskInfo; "+
			"$t.State.ToString(); $t.Actions[0].Execute; $i.LastTaskResult; if ($i.LastRunTime) { $i.LastRunTime.ToString('yyyy-MM-dd HH:mm:ss') }")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.Output()
	if err != nil {
		return taskState{}, err
	}
	return parseTaskState(string(out)), nil
}

func parseTaskState(out string) taskState {
	lines := strings.Split(strings.ReplaceAll(out, "\r", ""), "\n")
	line := func(i int) string {
		if i < len(lines) {
			return strings.TrimSpace(lines[i])
		}
		return ""
	}
	var r int64
	fmt.Sscan(line(2), &r)
	return taskState{state: line(0), program: line(1), lastResult: uint32(r), lastRun: line(3)}
}

// Running reports whether the installed task is running now.
func Running() bool {
	t, err := queryTask()
	return err == nil && t.state == "Running"
}

// AtBoot is whether the service starts by itself at logon, as a
// LaunchAgent does: its task is there, not disabled, and the program it
// starts is there (工单 #128: a task that is only there said "starts at
// login" while it could not).
func AtBoot() bool {
	t, err := queryTask()
	return err == nil && t.willStart() == ""
}

// willStart is why the task cannot start the daemon at logon, or "".
func (t taskState) willStart() string {
	if t.state == "Disabled" {
		return "its logon task is disabled"
	}
	if _, err := os.Stat(t.program); err != nil {
		return "its logon task's program is missing: " + t.program
	}
	return ""
}

// NotRunningWhy says why the installed service is not running, as far as
// Task Scheduler knows: the task cannot start, or how its last run ended.
func NotRunningWhy() string {
	t, err := queryTask()
	if err != nil {
		return ""
	}
	if why := t.willStart(); why != "" {
		return why
	}
	return t.lastRunText()
}

func (t taskState) lastRunText() string {
	switch t.lastResult {
	case 0x41303: // SCHED_S_TASK_HAS_NOT_RUN
		return "its logon task has not run since it was registered"
	case 0x41306: // SCHED_S_TASK_TERMINATED
		return "its logon task was ended (" + t.lastRun + ")"
	case 0:
		return "its supervisor stopped cleanly (" + t.lastRun + ")"
	}
	return fmt.Sprintf("its logon task's last run (%s) ended with %#x", t.lastRun, t.lastResult)
}

func Installed() bool { return schtasks("/Query", "/TN", taskName()) == nil }

// supervisorEnv marks a daemon its supervisor started: the logon task's.
const supervisorEnv = "SUPERVISED" // AIDE_SUPERVISED, or the old prefix's (brandenv): a supervisor from before the rename sets that

// Supervised reports whether this process is the installed service's
// daemon for dataDir, which its supervisor starts again when it exits
// after an update (docs/machine-updates.md).
func Supervised(dataDir string) bool {
	return brandenv.Get(supervisorEnv) == "1" && InstalledFor(dataDir)
}

// ReloadOldJob is for launchd, which keeps a job as it loaded it.
func ReloadOldJob(dataDir string) (bool, error) { return false, nil }

// InstalledFor reports whether the installed service runs the machine in
// dataDir.
func InstalledFor(dataDir string) bool {
	arg := xmlEscape(" --data-dir "+syscall.EscapeArg(dataDir)) + "</Arguments>"
	for _, d := range definitions(dataDir) {
		if b, err := os.ReadFile(d); err == nil && strings.Contains(fromUTF16(b), arg) {
			return Installed()
		}
	}
	return false
}

// taskXML is the task Install registers: at this user's logon, as this
// user, with no time limit, even on battery.
func taskXML(bin string, argv []string, sid string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = syscall.EscapeArg(a)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>Aide: runs coding agent sessions for your agents.</Description></RegistrationInfo>
  <Triggers>
    <LogonTrigger><Enabled>true</Enabled><UserId>%[1]s</UserId></LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author"><UserId>%[1]s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <IdleSettings><StopOnIdleEnd>false</StopOnIdleEnd><RestartOnIdle>false</RestartOnIdle></IdleSettings>
    <Priority>7</Priority>
  </Settings>
  <Actions Context="Author">
    <Exec><Command>%[2]s</Command><Arguments>%[3]s</Arguments></Exec>
  </Actions>
</Task>
`, xmlEscape(sid), xmlEscape(bin), xmlEscape(strings.Join(quoted, " ")))
}

// utf16File is s as Task Scheduler reads a task file: UTF-16LE, with a BOM.
func utf16File(s string) []byte {
	u := utf16.Encode([]rune("\uFEFF" + s))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return b
}

func fromUTF16(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return strings.TrimPrefix(string(utf16.Decode(u)), "\uFEFF")
}

// RunInBackground is for Linux and macOS: Setup.exe installs the logon task.
func RunInBackground(bin, dataDir string) (int, error) { return 0, errors.ErrUnsupported }

// Supervise runs exe with args, the daemon, in a job of its own (it ends
// with the supervisor), and starts it again whenever it exits but for
// a clean stop (exit status 0): soon at first, then backing off. A clean
// stop after an update replaced exe starts the new program at once. One
// supervisor runs per data directory: the logon task's and the app's
// may start together, and the second one leaves.
func Supervise(dataDir, exe string, args []string) error {
	lock, err := flock.Acquire(filepath.Join(dataDir, "run", "supervisor"))
	if err != nil {
		return err
	}
	defer lock.Release()
	// It only waits on the daemon: below normal and throttled for good
	// (docs/resident.md §2); the daemon starts at normal and saves by itself.
	resident.SaveAlways()
	backoff := time.Second
	for {
		began := time.Now()
		before, _ := os.Stat(exe)
		code, err := runInJob(exe, args)
		if err == nil && code == 0 {
			// Not os.SameFile: on Windows it reads the file ID by path when
			// asked, so before would be the new program too.
			if after, _ := os.Stat(exe); before != nil && after != nil && (!after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size()) {
				continue // updated
			}
			return nil
		}
		if time.Since(began) > time.Minute {
			backoff = time.Second
		}
		time.Sleep(backoff)
		backoff = min(2*backoff, time.Minute)
	}
}

var ntResumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

func runInJob(exe string, args []string) (int, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(job) // ends the daemon if it is still there
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE}}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return 0, err
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), brandenv.Both(supervisorEnv, "1")...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW | windows.NORMAL_PRIORITY_CLASS}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME, false, uint32(cmd.Process.Pid))
	if err == nil {
		if err = windows.AssignProcessToJobObject(job, h); err == nil {
			if st, _, _ := ntResumeProcess.Call(uintptr(h)); st != 0 {
				err = fmt.Errorf("resuming the daemon: NTSTATUS %#x", st)
			}
		}
		windows.CloseHandle(h)
	}
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return 0, err
	}
	cmd.Wait()
	return cmd.ProcessState.ExitCode(), nil
}
