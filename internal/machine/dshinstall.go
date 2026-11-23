package machine

import (
	"context"
	"os"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
	"github.com/samir2015dzh/aide/internal/shellenv"
)

// Installing the bridge plugin is handed to DSH (工单 #59): the owner presses
// Install once; aided prepares the package and starts a DSH session in the
// plugin's folder (a project of its own, aideBridgeProject, that works in
// place) whose task is dshnative.InstallPrompt. DSH reports with
// `aided dsh-bridge report`; serveDSH checks the agents again when the report
// comes, and when the plugin starts answering.

const aideBridgeProject = "aide-dsh-bridge"

func (s *adminService) InstallDSHBridge(ctx context.Context, _ *machinev1.InstallDSHBridgeRequest) (*machinev1.InstallDSHBridgeResponse, error) {
	if err := ownerOnly(ctx); err != nil {
		return nil, err
	}
	return s.n.InstallDSHBridge(ctx)
}

// InstallDSHBridge hands installing the bridge plugin to DSH. One already
// under way is returned as it is, not started twice.
func (n *Machine) InstallDSHBridge(ctx context.Context) (*machinev1.InstallDSHBridgeResponse, error) {
	home, err := dshnative.Home(shellenv.Environ())
	if err != nil {
		return nil, cerr(connect.CodeFailedPrecondition, "%s", err.Error())
	}
	owner := &Principal{Kind: PrincipalOwner}
	if rec, rep := dshnative.LastInstall(home); rec != nil && rep == nil {
		if s, err := n.sessions.Get(owner, rec.SessionID); err == nil && installing(s) {
			return &machinev1.InstallDSHBridgeResponse{SessionId: rec.SessionID, PackagePath: rec.Package}, nil
		}
	}
	p, err := dshnative.PrepareBridge(home, time.Now())
	if err != nil {
		return nil, cerr(connect.CodeFailedPrecondition, "%s", err.Error())
	}
	proj := Project{ID: aideBridgeProject, Path: dshnative.BridgeDir(home), Workspace: identityv1.Workspace_WORKSPACE_IN_PLACE,
		Agents: []string{"deepseek"}, Description: "Where DSH installs the Aide bridge plugin (Connect › DSH › Install)"}
	if err := n.sessions.st.PutProject(proj); err != nil {
		return nil, err
	}
	n.sessions.changes().project(proj.ID)
	aided, err := os.Executable()
	if err != nil {
		return nil, err
	}
	sess, err := n.sessions.Start(ctx, owner, &sessionv1.StartSessionRequest{
		Agent: "deepseek", ProjectId: aideBridgeProject, Title: "安装 DSH 的 Aide 插件 " + p.Version,
		Prompt: dshnative.InstallPrompt(home, p.Package, p.Version, aided),
	})
	if err != nil {
		return nil, err
	}
	if err := dshnative.StartInstall(home, dshnative.InstallRecord{SessionID: sess.SessionId, Package: p.Package, StartedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	n.audit.Log(AuditRecord{Kind: "dsh.bridge.install", Subject: "owner", Session: sess.SessionId, Decision: "allow", Detail: "package=" + p.Package + " backup=" + p.Backup})
	n.CheckAgents(true, 0) // the Connect page shows DSH at it at once
	return &machinev1.InstallDSHBridgeResponse{SessionId: sess.SessionId, PackagePath: p.Package}, nil
}

// installing: the DSH session is still at it (or waits on the owner).
func installing(s *sessionv1.Session) bool {
	switch s.Status {
	case sessionv1.SessionStatus_SESSION_STATUS_QUEUED, sessionv1.SessionStatus_SESSION_STATUS_PREPARING,
		sessionv1.SessionStatus_SESSION_STATUS_RUNNING, sessionv1.SessionStatus_SESSION_STATUS_WAITING_INPUT:
		return true
	}
	return false
}

// dshBridgeState is the bridge as the owner is shown it: dshBridge, with how
// the last install handed to DSH went.
func (n *Machine) dshBridgeState(ctx context.Context, home string) *machinev1.DSHBridge {
	b := dshBridge(ctx, home)
	b.Install = n.dshBridgeInstall(home, b.State == machinev1.DSHBridgeState_DSH_BRIDGE_STATE_RUNNING)
	return b
}

func (n *Machine) dshBridgeInstall(home string, running bool) *machinev1.DSHBridgeInstall {
	rec, rep := dshnative.LastInstall(home)
	if rec == nil {
		return nil
	}
	in := &machinev1.DSHBridgeInstall{SessionId: rec.SessionID, StartedAt: timestamppb.New(rec.StartedAt)}
	switch {
	case rep != nil && rep.OK && running:
		in.State = machinev1.DSHBridgeInstallState_DSH_BRIDGE_INSTALL_STATE_DONE
	case rep != nil && rep.OK:
		in.State = machinev1.DSHBridgeInstallState_DSH_BRIDGE_INSTALL_STATE_RESTART
		in.Reason = rep.Reason
	case rep != nil:
		in.State = machinev1.DSHBridgeInstallState_DSH_BRIDGE_INSTALL_STATE_FAILED
		in.Reason = rep.Reason
	case running:
		// No report, but the plugin answers: installed all the same.
		in.State = machinev1.DSHBridgeInstallState_DSH_BRIDGE_INSTALL_STATE_DONE
	default:
		s, err := n.sessions.Get(&Principal{Kind: PrincipalOwner}, rec.SessionID)
		if err == nil && installing(s) {
			in.State = machinev1.DSHBridgeInstallState_DSH_BRIDGE_INSTALL_STATE_RUNNING
		} else {
			in.State = machinev1.DSHBridgeInstallState_DSH_BRIDGE_INSTALL_STATE_FAILED
			in.Reason = "DSH 的安装任务已经结束，但没有回报结果：打开那个任务看它做到了哪一步。"
		}
	}
	if rep != nil {
		in.ReportedAt = timestamppb.New(rep.At)
	}
	return in
}
