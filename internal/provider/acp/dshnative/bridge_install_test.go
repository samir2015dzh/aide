package dshnative

import (
	"strings"
	"testing"
	"time"
)

func TestInstallReport(t *testing.T) {
	home := t.TempDir()
	if rec, rep := LastInstall(home); rec != nil || rep != nil {
		t.Fatalf("nothing handed to DSH yet: %v %v", rec, rep)
	}
	start := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	if err := StartInstall(home, InstallRecord{SessionID: "s1", Package: "p.tgz", StartedAt: start}); err != nil {
		t.Fatal(err)
	}
	if rec, rep := LastInstall(home); rec == nil || rec.SessionID != "s1" || rep != nil {
		t.Fatalf("at it, no report yet: %v %v", rec, rep)
	}
	// A failure must say why: the owner reads it on the Connect page.
	if err := Report(home, false, "  ", start.Add(time.Minute)); err == nil {
		t.Fatal("a failure without a reason was taken")
	}
	before := ReportStamp(home)
	if err := Report(home, false, "别的插件的常驻连接器占着 uv.exe", start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ReportStamp(home).Equal(before) {
		t.Fatal("a report must change the stamp aided watches")
	}
	_, rep := LastInstall(home)
	if rep == nil || rep.OK || rep.Reason != "别的插件的常驻连接器占着 uv.exe" {
		t.Fatalf("report: %+v", rep)
	}
	// A new install forgets the last report; a report older than it is not its.
	if err := StartInstall(home, InstallRecord{SessionID: "s2", StartedAt: start.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, rep := LastInstall(home); rep != nil {
		t.Fatalf("the last install's report carried over: %+v", rep)
	}
	if err := Report(home, true, "", start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, rep := LastInstall(home); rep != nil {
		t.Fatal("a report from before the install was taken as its")
	}
}

func TestInstallPrompt(t *testing.T) {
	p := InstallPrompt("/home/u/.dsh", "/home/u/.dsh/aide-bridge/aide-dsh-bridge-0.3.1.tgz", "0.3.1", "/opt/aide/aided")
	for _, want := range []string{
		"/home/u/.dsh/aide-bridge/aide-dsh-bridge-0.3.1.tgz",
		"/home/u/.dsh/profiles/desktop",
		// Look past "no write permission": the log, and a running program holding a package's folder (工单 #59).
		"pnpm.log", "EPERM", "ExecutablePath", "package.json",
		// Restart, check, and report in every case.
		`"/opt/aide/aided" dsh-bridge status`,
		`"/opt/aide/aided" dsh-bridge report --ok`,
		`"/opt/aide/aided" dsh-bridge report --failed --reason`,
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	// Windows paths stay as they are (not Go-quoted, which would double the backslashes).
	if w := InstallPrompt(`C:\Users\u\.dsh`, `C:\x.tgz`, "0.3.1", `C:\Aide\aided.exe`); !strings.Contains(w, `"C:\Aide\aided.exe" dsh-bridge report`) {
		t.Error("the Windows path of aided was mangled")
	}
}
