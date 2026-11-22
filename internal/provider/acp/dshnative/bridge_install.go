package dshnative

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Installing the bridge plugin is handed to DSH itself (工单 #59): the owner
// presses Install once, aided prepares the package and starts a DSH session in
// BridgeDir(home) whose task is InstallPrompt. DSH Desktop reserves its profile
// for its own plugin manager, which DSH's agent can drive; aided does not write
// into the profile. When it is done, DSH says how it went with
// `aided dsh-bridge report`, which writes ReportFile: aided reads it, checks the
// plugin again and shows the outcome on the Connect page.

// InstallRecord is the install aided last handed to DSH: the session doing it.
type InstallRecord struct {
	SessionID string    `json:"session_id"`
	Package   string    `json:"package"`
	StartedAt time.Time `json:"started_at"`
}

// InstallReport is what DSH reported (`aided dsh-bridge report`).
type InstallReport struct {
	OK bool `json:"ok"`
	// Reason: why it failed, or what is left to do, in words for the owner.
	Reason string    `json:"reason,omitempty"`
	At     time.Time `json:"at"`
}

func installFile(home string) string { return filepath.Join(BridgeDir(home), "install.json") }

// ReportFile is where `aided dsh-bridge report` leaves DSH's report.
func ReportFile(home string) string { return filepath.Join(BridgeDir(home), "install-report.json") }

// StartInstall records a new install handed to DSH, forgetting the last report.
func StartInstall(home string, r InstallRecord) error {
	if err := os.MkdirAll(BridgeDir(home), 0o700); err != nil {
		return err
	}
	if err := os.Remove(ReportFile(home)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeJSON(installFile(home), r)
}

// LastInstall is the install last handed to DSH, and its report if DSH made
// one since; nil, nil: none.
func LastInstall(home string) (*InstallRecord, *InstallReport) {
	var rec InstallRecord
	if readJSON(installFile(home), &rec) != nil || rec.SessionID == "" {
		return nil, nil
	}
	var rep InstallReport
	if readJSON(ReportFile(home), &rep) != nil || rep.At.Before(rec.StartedAt) {
		return &rec, nil
	}
	return &rec, &rep
}

// Report writes DSH's report for aided to read.
func Report(home string, ok bool, reason string, now time.Time) error {
	reason = strings.TrimSpace(reason)
	if !ok && reason == "" {
		return errors.New("say why it failed (--reason): the owner reads it on the Connect page")
	}
	if err := os.MkdirAll(BridgeDir(home), 0o700); err != nil {
		return err
	}
	return writeJSON(ReportFile(home), InstallReport{OK: ok, Reason: reason, At: now.UTC()})
}

// ReportStamp changes when a report is written: aided checks the plugin again then.
func ReportStamp(home string) time.Time {
	st, err := os.Stat(ReportFile(home))
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// InstallPrompt is DSH's task: install the plugin's package in DSH Desktop's
// profile and say how it went. It is written from what installing it by hand
// met on a Windows machine (工单 #59): DSH Desktop's "没有写入权限" there was
// not the folder's permissions but EPERM renaming a package's folder whose
// program another plugin's resident connector was running (Windows will not
// replace a running image); DSH sorts every EPERM as a permission problem. The
// failed install had also left a package of the profile half removed, and the
// repair after it hit the same lock.
func InstallPrompt(home, pkg, version, aided string) string {
	profile := filepath.Join(home, "profiles", "desktop")
	// Quoted as a shell takes it (not Go's %q, which would double a Windows path's backslashes).
	aided = `"` + aided + `"`
	report := aided + " dsh-bridge report"
	return fmt.Sprintf(`请把 Aide 桥接插件装进 DSH Desktop，装好后让它生效，并把结果回报给 Aide。所有者只点了一次「安装」，不会再动手：不要让所有者去插件页操作或碰文件路径。

- 插件包：%[1]s（@aide/dsh-bridge %[2]s）
- DSH Desktop 的 profile：%[3]s
- Aide 已把 DSH 的配置文件备份在 %[4]s/backups/ 下最新的目录里。

步骤：

1. 用 DSH 自己的插件管理把这个包装进 DSH Desktop 的 desktop profile（已装旧版就升级）。不要手工往 node_modules 里拷文件，也不要改别的插件。

2. 装不上时先找真因，不要只看「没有写入权限」这句话：
   - 读插件管理这次操作的 pnpm 日志（profile 下 .plugin-manager/logs/ 里最新那次操作的 pnpm.log）最后几行，看真正的报错。
   - EPERM / EBUSY 且是 rename 或 unlink 某个包的目录时，多半不是权限：是有程序正在运行那个包里的可执行文件，系统不让替换（Windows 尤其如此）。找出可执行文件路径落在这个 profile 的 node_modules 下、正在运行的进程（Windows 可用 Get-CimInstance Win32_Process 看 ExecutablePath），弄清它属于哪个插件（常见是别的插件的常驻连接器）。先退出 DSH Desktop 再装；若这类进程在 DSH Desktop 退出后仍在跑，只结束这些进程（它们随 DSH Desktop 重启会再起来），不要动别的进程。
   - 真是目录权限问题时（Get-Acl / ls -l 能看出当前用户没有写权限），说明是哪个目录、属主是谁。
   - 用户目录不匹配：确认 DSH Desktop 实际用的 DSH home 就是 %[7]s（不是另一个用户或另一个 DSH_HOME），不是就说出实际用的是哪个。

3. 检查 profile 有没有被上一次失败的安装弄坏：node_modules 里有没有包缺了 package.json、只剩一半文件。有就用插件管理的修复 / 按 lockfile 重装把它修好（锁解开以后再修）。

4. 让插件生效：重启 DSH Desktop（退出后再打开）。重启后运行 %[5]s dsh-bridge status，看到「plugin runs」才算装好。

5. 最后必须回报，二选一：
   - 装好且 status 显示 plugin runs：%[6]s --ok
   - 装好了但还没生效（比如没能重启 DSH Desktop）：%[6]s --ok --reason "<还差什么，一句话>"
   - 没装上：%[6]s --failed --reason "<真因，用所有者看得懂的中文一句话说清：是什么、在哪、为什么>"
   reason 会原样显示在 Aide 的连接页上，不要写成「失败」「权限不足」这种空话。

全程只动 DSH Desktop 的插件与它的 profile；需要结束进程、改权限时，在最后的回报里写明做了什么。`, pkg, version, profile, BridgeDir(home), aided, report, home)
}
