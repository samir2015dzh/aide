package owner

import (
	"context"
	"slices"
	"sync"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/machine"
	"github.com/samir2015dzh/aide/internal/peer"
)

// The messages each machine holds for the owner before they reach a
// session (docs/agent-messages.md §15.5): listed over the machines that
// answer, released or discarded on the one that holds it.

// ListHeldMessages gathers them, oldest first.
func (o *Owner) ListHeldMessages(ctx context.Context) (*localv1.ListHeldMessagesResponse, error) {
	on, off, err := o.onlineMachines(ctx, "")
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	out := &localv1.ListHeldMessagesResponse{Unreachable: off}
	add := func(res *machinev1.ListHeldMessagesResponse, meta *localv1.Machine) {
		mu.Lock()
		defer mu.Unlock()
		for _, m := range res.Messages {
			out.Messages = append(out.Messages, &localv1.HeldMessageEntry{MachineId: meta.MachineId, MachineName: meta.MachineName, Message: m})
		}
	}
	bad, err := o.fanOut(ctx, on, func(l *machine.Local, meta *localv1.Machine) error {
		add(l.HeldMessages(), meta)
		return nil
	}, func(ctx context.Context, n *peer.Machine, meta *localv1.Machine) error {
		res, err := n.Admin.ListHeldMessages(ctx, &machinev1.ListHeldMessagesRequest{})
		if err == nil {
			add(res, meta)
		}
		return err
	})
	if err != nil {
		for _, n := range on {
			if !n.Local {
				bad = append(bad, &localv1.Unreachable{MachineId: n.MachineId, MachineName: n.MachineName, Error: err.Error()})
			}
		}
	}
	out.Unreachable = append(out.Unreachable, bad...)
	slices.SortStableFunc(out.Messages, func(a, b *localv1.HeldMessageEntry) int {
		return a.Message.HeldAt.AsTime().Compare(b.Message.HeldAt.AsTime())
	})
	return out, nil
}

// ReleaseHeldMessage releases one to its session, or discards it, on its
// machine.
func (o *Owner) ReleaseHeldMessage(ctx context.Context, req *localv1.ReleaseHeldMessageRequest) error {
	m, err := o.findMachine(ctx, req.MachineId)
	if err != nil {
		return err
	}
	if l := o.local(); m.Local && l != nil {
		return l.ReleaseHeldMessage(ctx, req.MessageId, req.Release)
	}
	n, err := o.machineClient(ctx, m.MachineId)
	if err != nil {
		return err
	}
	_, err = n.Admin.ReleaseHeldMessage(ctx, &machinev1.ReleaseHeldMessageRequest{MessageId: req.MessageId, Release: req.Release})
	return err
}
