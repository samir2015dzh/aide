package owner

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/machine"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// A computer can log in as a machine only, not as one of the owner's
// devices (a work computer, say): it sees only its own projects and
// sessions, which it settles as the owner would (answer, stop, archive).
// It logs in like any machine (logInAsMachine), with the account and
// password.

const kMachineOnly = "machine_only"

func (o *Owner) machineOnly() bool {
	v, _ := o.st.get(kMachineOnly)
	return string(v) == "1"
}

// onlyMachine is this machine's in-process access when this device is only
// a machine; ok is false when it is not one.
func (o *Owner) onlyMachine() (l *machine.Local, ok bool, err error) {
	if o.Initialized() || !o.machineOnly() {
		return nil, false, nil
	}
	o.mu.Lock()
	n := o.localMachine
	o.mu.Unlock()
	if n == nil {
		return nil, true, connect.NewError(connect.CodeUnavailable, errors.New("this computer's machine is not running"))
	}
	return n.Local(), true, nil
}

// rebindLocalMachine publishes the cert that makes this device's running
// machine its own, and hands it the cert and the chain listing the device.
func (o *Owner) rebindLocalMachine(ctx context.Context, chain *identityv1.RootChain, cert *identityv1.Signed) error {
	ctl, _, err := o.control(ctx)
	if err != nil {
		return err
	}
	if _, err := ctl.PublishMachineCert(ctx, &relayv1.PublishMachineCertRequest{MachineCert: cert}); err != nil {
		return err
	}
	o.mu.Lock()
	n := o.localMachine
	o.mu.Unlock()
	if n != nil {
		n.Adopt(chain, cert)
	}
	return nil
}

// machineOnlyStatus describes this device when it is only a machine.
func (o *Owner) machineOnlyStatus(st *localv1.GetStatusResponse) {
	st.MachineOnly = true
	o.mu.Lock()
	st.Account = o.account
	n := o.localMachine
	o.mu.Unlock()
	if n == nil {
		return
	}
	st.OwnerId, st.RelayUrl = n.OwnerID(), n.RelayURL()
	st.RelayConnected, st.Removed = n.Connected(), n.Refused()
}

// leaveAsMachine stops the turns under way, has the relay remove this
// machine from its owner (when it can be reached; otherwise the owner's
// devices list it offline until they remove it), and retires it: it keeps
// its projects and sessions, and joins again, to any owner, under a new id.
func (o *Owner) leaveAsMachine(ctx context.Context) error {
	o.mu.Lock()
	n := o.localMachine
	o.mu.Unlock()
	if n != nil {
		n.Local().InterruptActive(ctx)
		lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		if err := n.LeaveOwner(lctx); err != nil {
			o.log.Warn("leaving: the relay did not remove this machine; the owner's devices list it until they do", "err", err)
		}
		cancel()
	}
	o.stopLocalMachine()
	if err := machine.Retire(o.localMachineConfig()); err != nil {
		return err
	}
	return o.st.set(kMachineOnly, []byte{})
}

// ---------------------------------------------------------------------------
// The local API on a machine only: this machine, in process.

func (o *Owner) onlyMachineList(l *machine.Local) []*localv1.Machine {
	info := l.Info()
	o.mu.Lock()
	n := o.localMachine
	o.mu.Unlock()
	return []*localv1.Machine{{MachineId: info.MachineId, MachineName: info.MachineName, Online: n != nil && n.Connected(), Version: machine.Version,
		Local: true, Status: info.Status}}
}

func onlyMachineProjects(ctx context.Context, l *machine.Local) (*localv1.ListProjectsResponse, error) {
	res, err := l.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	id := l.Info().MachineId
	out := &localv1.ListProjectsResponse{}
	for _, p := range res.Projects {
		out.Projects = append(out.Projects, &localv1.Project{MachineId: id, ProjectId: p.ProjectId, DefaultBaseRef: p.DefaultBaseRef, Workspace: p.Workspace,
			Description: p.Description, Agents: p.Agents, Path: p.Path, Name: p.Name})
	}
	return out, nil
}

// onlyMachineClients are the agents a machine only knows: those its grants
// let start sessions here, scoped to this machine alone (it doesn't see the
// others), with when each last came; then the ones that came under a grant
// no longer valid, revoked, so their sessions keep their names.
func onlyMachineClients(l *machine.Local) []*localv1.Client {
	id := l.Info().MachineId
	known := map[string]machine.KnownClient{}
	for _, c := range l.Clients() {
		known[c.ID] = c
	}
	var out []*localv1.Client
	for _, g := range l.Grants() {
		spec := specFromGrant(g)
		spec.MachineIds = []string{id}
		c := &localv1.Client{ClientId: g.ClientId, GrantId: g.GrantId, Label: g.ClientLabel, Fingerprint: identity.ShortFingerprint(g.ClientPublicKey),
			Spec: spec, GrantVersion: g.Version, ExpiresAt: g.ExpiresAt, Custodian: g.Custodian, Icon: g.ClientIcon, Image: g.ClientImage}
		if k, ok := known[g.ClientId]; ok && !k.Seen.IsZero() {
			c.LastSeen = timestamppb.New(k.Seen)
		}
		delete(known, g.ClientId)
		out = append(out, c)
	}
	for _, k := range known {
		out = append(out, &localv1.Client{ClientId: k.ID, GrantId: k.GrantID, Label: k.Label, Revoked: true})
	}
	slices.SortFunc(out, func(a, b *localv1.Client) int { return strings.Compare(a.Label, b.Label) })
	return out
}

// onlyThisMachine refuses a machine id other than this one.
func onlyThisMachine(l *machine.Local, machineID string) error {
	if info := l.Info(); machineID != "" && machineID != info.MachineId && machineID != info.MachineName {
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("no machine %q", machineID))
	}
	return nil
}

func onlyMachineSettings(ctx context.Context, l *machine.Local, machineID string, build func(*identityv1.AdminCommand)) error {
	if err := onlyThisMachine(l, machineID); err != nil {
		return err
	}
	_, err := l.ApplySettings(ctx, build)
	return err
}

// onlyMachineWatch tells the watcher this machine is reachable, then sends
// its session changes.
func onlyMachineWatch(ctx context.Context, l *machine.Local, send func(*localv1.WatchSessionsResponse) error) error {
	if err := send(&localv1.WatchSessionsResponse{MachinesChanged: true}); err != nil {
		return err
	}
	return l.WatchSessions(ctx, func(m *sessionv1.WatchSessionsResponse) error {
		if len(m.Sessions) == 0 && len(m.DeletedSessionIds) == 0 {
			return nil
		}
		return send(&localv1.WatchSessionsResponse{Sessions: m.Sessions, DeletedSessionIds: m.DeletedSessionIds})
	})
}
