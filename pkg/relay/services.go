package relay

import (
	"context"
	"errors"
	"net"
	"time"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/pkg/identity"
	"github.com/samir2015dzh/aide/pkg/transport"
)

func errf(code connect.Code, msg string) error { return connect.NewError(code, errors.New(msg)) }

// ---------------------------------------------------------------------------
// Public API: info, setting up and logging in (account.go), erase acks.

type publicService struct{ s *Server }

func (p *publicService) GetInfo(ctx context.Context, req *relayv1.GetInfoRequest) (*relayv1.GetInfoResponse, error) {
	s := p.s
	resp := &relayv1.GetInfoResponse{RelayId: s.relayID, ProtocolVersion: transport.ProtocolVersion}
	if owner := s.singleOwner(); owner != "" {
		resp.OwnerId, resp.Claimed = owner, true
		if t, err := s.tenantFor(owner); err == nil {
			resp.RootChain = t.trust.RootChain()
		}
	}
	// An owner that erased everything: its order, for its devices to verify
	// against the chain it was signed under (docs/erase-everything.md).
	if req.OwnerId != "" && req.OwnerId != resp.OwnerId {
		if order, chain, acks := s.eraseOrder(req.OwnerId); order != nil {
			resp.EraseOrder, resp.RootChain, resp.EraseAcks = order, chain, acks
		}
	}
	return resp, nil
}

func (p *publicService) AckErase(ctx context.Context, req *relayv1.AckEraseRequest) (*relayv1.AckEraseResponse, error) {
	es, ok := p.s.store.(EraseStore)
	if !ok {
		return nil, errf(connect.CodeUnimplemented, "this relay keeps no erase orders")
	}
	// A trusted device's ack is verified against the chain its owner's
	// order was signed under.
	var trust *identity.Trust
	a, dc := &identityv1.EraseAck{}, &identityv1.OwnerDeviceCert{}
	if identity.Unverified(req.Ack, a) == nil && a.DeviceCert != nil && identity.Unverified(a.DeviceCert, dc) == nil {
		if order, chain, _ := p.s.eraseOrder(dc.OwnerId); order != nil {
			trust = identity.NewTrust(dc.OwnerId)
			trust.AcceptRootChain(chain)
		}
	}
	ack, err := identity.VerifyEraseAck(req.Ack, trust)
	if err != nil {
		return nil, errf(connect.CodeInvalidArgument, err.Error())
	}
	if err := es.AckErase(ack.EraseId, ack.Subject, time.Now()); err != nil {
		return nil, err
	}
	return &relayv1.AckEraseResponse{}, nil
}

func clientIP(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

func (s *Server) RootChain(ownerID string) (*identityv1.RootChain, error) {
	t, err := s.tenantFor(ownerID)
	if err != nil {
		return nil, err
	}
	return t.trust.RootChain(), nil
}

type machineControl struct{ s *Server }

func (n *machineControl) ReportStatus(ctx context.Context, req *relayv1.ReportStatusRequest) (*relayv1.ReportStatusResponse, error) {
	p := peerFrom(ctx)
	n.s.mu.Lock()
	p.t.reported[p.subject.ID] = &relayv1.ReportStatusRequest{Name: req.Name, Version: req.Version}
	n.s.wakeOwnerLocked(p.t)
	n.s.mu.Unlock()
	return &relayv1.ReportStatusResponse{}, nil
}

func (n *machineControl) Leave(ctx context.Context, _ *relayv1.LeaveRequest) (*relayv1.LeaveResponse, error) {
	p := peerFrom(ctx)
	n.s.log.Info("machine leaves its owner", "owner", p.t.ownerID, "machine", p.subject.ID)
	return &relayv1.LeaveResponse{}, n.s.applyRevocations(p.t, &identityv1.RevocationList{OwnerId: p.t.ownerID, RevokedMachineIds: []string{p.subject.ID}})
}

func (n *machineControl) WatchUpdates(ctx context.Context, _ *relayv1.WatchUpdatesRequest, stream *connect.ServerStream[relayv1.MachineUpdate]) error {
	s := n.s
	p := peerFrom(ctx)
	t := p.t
	q := newUpdates()
	s.mu.Lock()
	if t.watchers[p.subject.ID] == nil {
		t.watchers[p.subject.ID] = map[*updates]struct{}{}
	}
	t.watchers[p.subject.ID][q] = struct{}{}
	rev := t.revocations
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(t.watchers[p.subject.ID], q)
		s.mu.Unlock()
	}()
	if err := stream.Send(&relayv1.MachineUpdate{Update: &relayv1.MachineUpdate_RootChain{RootChain: t.trust.RootChain()}}); err != nil {
		return err
	}
	if err := stream.Send(&relayv1.MachineUpdate{Update: &relayv1.MachineUpdate_Revocations{Revocations: rev}}); err != nil {
		return err
	}
	if certs, err := s.store.MachineCerts(t.ownerID); err == nil && certs[p.subject.ID] != nil {
		stream.Send(&relayv1.MachineUpdate{Update: &relayv1.MachineUpdate_MachineCert{MachineCert: certs[p.subject.ID]}})
	}
	targets := &relayv1.PushTargets{}
	for _, pt := range s.pushTargets(t) {
		targets.Targets = append(targets.Targets, pt)
	}
	stream.Send(&relayv1.MachineUpdate{Update: &relayv1.MachineUpdate_PushTargets{PushTargets: targets}})
	if g := s.machineGrants(t, p.subject.ID); g != nil {
		stream.Send(&relayv1.MachineUpdate{Update: &relayv1.MachineUpdate_Grants{Grants: g}})
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-p.sess.CloseChan():
			return nil
		case <-q.wake:
			for _, u := range q.take() {
				if err := stream.Send(u); err != nil {
					return err
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Client control.

type clientControl struct{ s *Server }

func (c *clientControl) WhoAmI(ctx context.Context, _ *relayv1.WhoAmIRequest) (*relayv1.WhoAmIResponse, error) {
	p := peerFrom(ctx)
	return &relayv1.WhoAmIResponse{SubjectId: p.subject.ID, Role: p.role, OwnerId: p.t.ownerID}, nil
}

func (c *clientControl) ListMachines(ctx context.Context, req *relayv1.ListMachinesRequest) (*relayv1.ListMachinesResponse, error) {
	p := peerFrom(ctx)
	machines, err := c.s.machineEntries(p.t, func(id string) bool { return identity.Allows(p.subject.Grant.MachineIds, id) }, req.OnlineOnly)
	if err != nil {
		return nil, err
	}
	return &relayv1.ListMachinesResponse{Machines: machines, RootChain: p.t.trust.RootChain()}, nil
}

func (c *clientControl) GetLatestGrant(ctx context.Context, _ *relayv1.GetLatestGrantRequest) (*relayv1.GetLatestGrantResponse, error) {
	p := peerFrom(ctx)
	g, err := c.s.store.GrantForClient(p.t.ownerID, p.subject.ID)
	if err != nil {
		return nil, err
	}
	return &relayv1.GetLatestGrantResponse{Grant: g, RootChain: p.t.trust.RootChain(), Revoked: c.s.checkNotRevoked(p.t, p.subject) != nil,
		InboxWaiting: uint32(c.s.InboxWaiting(p.t.ownerID, p.subject.ID))}, nil
}

// ---------------------------------------------------------------------------
// Owner control: what the owner's devices ask; the server signs for them
// (account.go).

type ownerControl struct{ s *Server }

func (o *ownerControl) ListMachines(ctx context.Context, req *relayv1.ListMachinesRequest) (*relayv1.ListMachinesResponse, error) {
	t := peerFrom(ctx).t
	machines, err := o.s.machineEntries(t, nil, req.OnlineOnly)
	if err != nil {
		return nil, err
	}
	return &relayv1.ListMachinesResponse{Machines: machines, RootChain: t.trust.RootChain()}, nil
}

func (o *ownerControl) ListClients(ctx context.Context, _ *relayv1.ListClientsRequest) (*relayv1.ListClientsResponse, error) {
	out, err := o.s.clientEntries(peerFrom(ctx).t)
	if err != nil {
		return nil, err
	}
	return &relayv1.ListClientsResponse{Clients: out}, nil
}

func (o *ownerControl) PublishGrant(ctx context.Context, req *relayv1.PublishGrantRequest) (*relayv1.PublishGrantResponse, error) {
	t := peerFrom(ctx).t
	g, _, err := t.trust.VerifyGrant(req.Grant, time.Now())
	if err != nil {
		return nil, errf(connect.CodeInvalidArgument, err.Error())
	}
	defer o.s.wakeOwner(t)
	if err := o.s.store.PutGrant(t.ownerID, g, req.Grant); err != nil {
		return nil, err
	}
	o.s.pushGrants(t)
	return &relayv1.PublishGrantResponse{}, nil
}

func (o *ownerControl) PublishMachineCert(ctx context.Context, req *relayv1.PublishMachineCertRequest) (*relayv1.PublishMachineCertResponse, error) {
	t := peerFrom(ctx).t
	nc, _, err := t.trust.VerifyMachineCert(req.MachineCert)
	if err != nil {
		return nil, errf(connect.CodeInvalidArgument, err.Error())
	}
	if err := o.s.store.PutMachineCert(t.ownerID, nc.MachineId, req.MachineCert); err != nil {
		return nil, err
	}
	o.s.mu.Lock()
	o.s.wakeOwnerLocked(t)
	for q := range t.watchers[nc.MachineId] {
		q.put(&relayv1.MachineUpdate{Update: &relayv1.MachineUpdate_MachineCert{MachineCert: req.MachineCert}})
	}
	o.s.mu.Unlock()
	return &relayv1.PublishMachineCertResponse{}, nil
}

func (o *ownerControl) Revoke(ctx context.Context, req *relayv1.RevokeRequest) (*relayv1.RevokeResponse, error) {
	if req.Revocations == nil {
		return nil, errf(connect.CodeInvalidArgument, "missing revocations")
	}
	return &relayv1.RevokeResponse{}, o.s.applyRevocations(peerFrom(ctx).t, req.Revocations)
}

func (o *ownerControl) EraseOwner(ctx context.Context, req *relayv1.EraseOwnerRequest) (*relayv1.EraseOwnerResponse, error) {
	t := peerFrom(ctx).t
	es, ok := o.s.store.(EraseStore)
	if !ok {
		return nil, errf(connect.CodeUnimplemented, "this relay can't erase an owner yet")
	}
	order, err := t.trust.VerifyEraseOrder(req.Order)
	if err != nil {
		return nil, errf(connect.CodeInvalidArgument, err.Error())
	}
	if at := order.IssuedAt.AsTime(); order.IssuedAt == nil || time.Since(at) > 10*time.Minute || time.Until(at) > identity.ClockSkew {
		return nil, errf(connect.CodeInvalidArgument, "erase order: not issued just now")
	}
	if err := es.EraseOwner(t.ownerID, order.EraseId, req.Order, t.trust.RootChain(), time.Now()); err != nil {
		return nil, err
	}
	o.s.erased(t.ownerID)
	return &relayv1.EraseOwnerResponse{}, nil
}

// GetAccount answers with no account picture: a server of one's own has
// none (GetLogin names the account).
func (o *ownerControl) GetAccount(context.Context, *relayv1.GetAccountRequest) (*relayv1.GetAccountResponse, error) {
	return &relayv1.GetAccountResponse{}, nil
}

func (o *ownerControl) GetScreenServers(context.Context, *relayv1.GetScreenServersRequest) (*relayv1.GetScreenServersResponse, error) {
	return nil, errRemoteDesktop
}

func (n *machineControl) GetScreenServers(context.Context, *relayv1.GetScreenServersRequest) (*relayv1.GetScreenServersResponse, error) {
	return nil, errRemoteDesktop
}

// errRemoteDesktop answers the remote desktop's calls, which the protocol keeps.
var errRemoteDesktop = errf(connect.CodeUnimplemented, "remote desktop was removed")

func (o *ownerControl) GetMCP(context.Context, *relayv1.GetMCPRequest) (*relayv1.GetMCPResponse, error) {
	return &relayv1.GetMCPResponse{Url: o.s.cfg.MCPURL}, nil
}
