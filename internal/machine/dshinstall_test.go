package machine

import (
	"context"
	"strings"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
)

// TestInstallDSHBridge (工单 #59): one Install hands the plugin to DSH — a DSH
// session in the plugin's folder, with the package prepared — and the
// bridge's install state follows: at it, ended without a report (failed, with
// why), reported failed (DSH's reason), reported installed but not answering
// (restart DSH Desktop), installed and answering.
func TestInstallDSHBridge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	dsh := &recorder{}
	h := newHarnessWith(t, Limits{}, map[string]provider.Provider{"deepseek": dsh})
	ctx := context.Background()
	owner := &Principal{Kind: PrincipalOwner}

	res, err := h.n.InstallDSHBridge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.PackagePath != dshnative.PreparedPackage(home) || res.PackagePath == "" {
		t.Fatalf("package: %q, prepared %q", res.PackagePath, dshnative.PreparedPackage(home))
	}
	proj, err := h.n.st.Project(aideBridgeProject)
	if err != nil || proj == nil || proj.Path != dshnative.BridgeDir(home) || proj.Workspace != identityv1.Workspace_WORKSPACE_IN_PLACE || !proj.AllowsAgent("deepseek") || proj.AllowsAgent("fake") {
		t.Fatalf("its project: %+v %v", proj, err)
	}
	s, err := h.s.Get(owner, res.SessionId)
	if err != nil || s.Agent != "deepseek" || s.ProjectId != aideBridgeProject || s.CreatedBy != "owner" {
		t.Fatalf("the DSH session: %v %v", s, err)
	}
	h.p = owner
	h.wait(res.SessionId, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	dsh.mu.Lock()
	spec := dsh.starts[len(dsh.starts)-1]
	dsh.mu.Unlock()
	if spec.WorkDir != dshnative.BridgeDir(home) || !strings.Contains(spec.Prompt, res.PackagePath) || !strings.Contains(spec.Prompt, "dsh-bridge report") {
		t.Fatalf("DSH was asked in %q: %.200s", spec.WorkDir, spec.Prompt)
	}

	state := func() *machinev1.DSHBridgeInstall { return h.n.dshBridgeState(ctx, home).Install }
	// Its turn ended, nothing reported: failed, and it says to look at the task.
	if in := state(); in.State != machinev1.DSHBridgeInstallState_DSH_BRIDGE_INSTALL_STATE_FAILED || !strings.Contains(in.Reason, "没有回报") || in.SessionId != res.SessionId {
		t.Fatalf("ended without a report: %v", in)
	}
	now := time.Now()
	if err := dshnative.Report(home, false, "别的插件的常驻连接器正在运行 profile 里的 uv.exe，替换不了", now); err != nil {
		t.Fatal(err)
	}
	if in := state(); in.State != machinev1.DSHBridgeInstallState_DSH_BRIDGE_INSTALL_STATE_FAILED || !strings.Contains(in.Reason, "uv.exe") || in.ReportedAt == nil {
		t.Fatalf("reported failed: %v", in)
	}
	if err := dshnative.Report(home, true, "", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if in := state(); in.State != machinev1.DSHBridgeInstallState_DSH_BRIDGE_INSTALL_STATE_RESTART {
		t.Fatalf("installed, not answering yet: %v", in)
	}
	startFakeBridge(t, home)
	if b := h.n.dshBridgeState(ctx, home); b.State != machinev1.DSHBridgeState_DSH_BRIDGE_STATE_RUNNING || b.Install.State != machinev1.DSHBridgeInstallState_DSH_BRIDGE_INSTALL_STATE_DONE {
		t.Fatalf("installed and answering: %v", b)
	}
}
