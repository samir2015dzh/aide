package machine

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider/claude"
)

// Claude plan usage on this machine (docs/claude-usage.md): the scheduler
// paces work by it. The reading is fetched in the background, not while a
// status is served, because it reaches the network and the token store; a
// status only carries the last reading. aided turns it on (Config.ClaudeUsage);
// everything about the token stays in claude.UsageReader, on the machine.

// usageEvery is how often a fresh reading is fetched. Five minutes keeps
// the window figures current without hammering the endpoint.
const usageEvery = 5 * time.Minute

// usageSource reads a plan-usage snapshot. *claude.UsageReader is the real
// one; tests inject a fake.
type usageSource interface {
	Read(context.Context) claude.Usage
}

// usageDue reports whether a fresh usage reading is owed and claims the
// fetch so only one runs at a time. The caller must hold n.mu.
func (n *Machine) usageDue() bool {
	if n.usageSrc == nil || n.usageFetch {
		return false
	}
	if !n.usageAt.IsZero() && time.Since(n.usageAt) < usageEvery {
		return false
	}
	n.usageFetch = true
	return true
}

// refreshUsage fetches a reading and stores it, falling back to the last
// good windows (marked stale) when the endpoint fails, then wakes the
// watchers. It runs in its own goroutine.
func (n *Machine) refreshUsage() {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	u := n.usageSrc.Read(ctx)

	n.mu.Lock()
	prev := n.usage
	pb := usageToProto(u, prev)
	n.usage = pb
	n.usageAt = time.Now()
	n.usageFetch = false
	n.mu.Unlock()

	n.logUsage(pb, prev)
	n.kickStatus()
}

// usageToProto turns a reading into the status message. When the endpoint
// failed (api_error) but there is a last good reading, its windows are kept
// and marked stale, so a blip does not blank the figures; any renew outcome
// still shows. Every other unavailability is reported as it is.
func usageToProto(u claude.Usage, prev *machinev1.ClaudeUsage) *machinev1.ClaudeUsage {
	if u.Unavailable == "api_error" && prev != nil && len(prev.Windows) > 0 {
		out := proto.Clone(prev).(*machinev1.ClaudeUsage)
		out.Stale = true
		out.Renew = u.Renew
		return out
	}
	pb := &machinev1.ClaudeUsage{
		FetchedAt:   timestamppb.New(u.FetchedAt),
		Unavailable: u.Unavailable,
		Renew:       u.Renew,
	}
	for _, w := range u.Windows {
		uw := &machinev1.UsageWindow{Key: w.Key, Utilization: w.Utilization}
		if !w.ResetsAt.IsZero() {
			uw.ResetsAt = timestamppb.New(w.ResetsAt)
		}
		pb.Windows = append(pb.Windows, uw)
	}
	return pb
}

// logUsage notes a change worth the owner seeing: usage became unreadable,
// came back, or a renew happened or failed. The token never appears.
func (n *Machine) logUsage(now, prev *machinev1.ClaudeUsage) {
	if now == nil {
		return
	}
	prevUnavail, prevRenew := "", ""
	if prev != nil {
		prevUnavail, prevRenew = prev.Unavailable, prev.Renew
	}
	if now.Unavailable != prevUnavail {
		if now.Unavailable == "" {
			n.log.Info("Claude plan usage readable again")
		} else {
			n.log.Info("Claude plan usage not readable", "why", now.Unavailable)
		}
	}
	if now.Renew != "" && now.Renew != prevRenew {
		n.log.Info("Claude plan usage token auto-renew", "outcome", now.Renew)
	}
}
