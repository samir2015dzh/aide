package machine

import (
	"context"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider/claude"
)

// Claude plan usage on this machine (docs/claude-usage.md): the scheduler
// paces work by it. The reading is fetched in the background, not while a
// status is served, because it reaches the network and the token store; a
// status only carries the last reading. aided turns it on (Config.ClaudeUsage).
//
// Where it is read (工单 #86, #104), the first source with the plan's
// windows giving the reading:
//
//  1. what this machine's Claude Code last read of it and keeps in its
//     config (claude.CacheUsage), when that is fresher than a reading of
//     our own would be (usageEvery); asks nothing of anyone;
//  2. this machine's own Claude Code, signed in here, as its /usage tells
//     it (claude.CLIUsage). Where its sign-in does not read the plan it
//     writes only that call's own totals (claude.CLISessionOnly): no
//     reading, the next source is asked. A version without /usage outside
//     a terminal is not asked again. Claude Code answering a plan's report
//     without its rows (its own reading failing, rate limited) is not asked
//     past: the endpoint would be asked twice;
//  3. the usage endpoint with the token found here (claude.UsageReader):
//     Claude Code's credentials, else the signed-in Claude desktop app's.
//
// What each source said on the way rides along in the reading's detail and
// aided's log, so a machine without a reading says why at every source.
// Whatever fails, the last reading stays, marked stale, with when it was
// read — kept on disk, so a restart does not blank it either; a cache older
// than that reading, but newer, takes its place.

// usageEvery is how often a fresh reading is fetched: a quarter of an hour,
// well inside the 5-hour window it reports, and far enough apart that the
// usage endpoint (which rate limits) is not pressed.
const usageEvery = 15 * time.Minute

// kClaudeUsage keeps the last good reading (windows and when; no token).
const kClaudeUsage = "claude_usage"

// usageSource reads a plan-usage snapshot from the usage endpoint.
// *claude.UsageReader is the real one; tests inject a fake.
type usageSource interface {
	Read(context.Context) claude.Usage
}

// cacheUsageSource reads what Claude Code here cached of it.
// *claude.CacheUsage is the real one; tests inject a fake.
type cacheUsageSource interface {
	Read() claude.Usage
}

// cliUsageSource reads it through Claude Code at bin. *claude.CLIUsage is
// the real one; tests inject a fake.
type cliUsageSource interface {
	Read(ctx context.Context, bin string) claude.Usage
}

// usageDue reports whether a fresh usage reading is owed and claims the
// fetch so only one runs at a time. The caller must hold n.mu.
func (n *Machine) usageDue() bool {
	if (n.usageSrc == nil && n.usageCLI == nil && n.usageCache == nil) || n.usageFetch {
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	u, older := n.readUsage(ctx)

	n.mu.Lock()
	prev := n.usage
	kept := prev
	if older != nil && (kept == nil || len(kept.Windows) == 0 || kept.FetchedAt.AsTime().Before(older.FetchedAt)) {
		kept = usageToProto(*older, nil)
	}
	pb := usageToProto(u, kept)
	n.usage = pb
	n.usageAt = time.Now()
	n.usageFetch = false
	n.mu.Unlock()

	if u.Unavailable == "" {
		if b, err := proto.Marshal(pb); err == nil {
			n.st.Set(kClaudeUsage, b)
		}
	}
	n.logUsage(pb, prev)
	n.kickStatus()
}

// readUsage reads the usage once, from the first source that has it. When
// none has, older is Claude Code's cache when it had windows too old to be
// the reading.
func (n *Machine) readUsage(ctx context.Context) (u claude.Usage, older *claude.Usage) {
	var tried []string
	// 1. Claude Code's cache.
	if n.usageCache != nil {
		c := n.usageCache.Read()
		c.Source = "claude_code_cache"
		switch {
		case c.Unavailable != "":
			tried = append(tried, c.Detail)
		case time.Since(c.FetchedAt) < usageEvery:
			return c, nil
		default:
			older = &c
			tried = append(tried, "Claude Code cache: read "+time.Since(c.FetchedAt).Round(time.Minute).String()+" ago, too old to be the reading")
		}
	}
	// 2. Claude Code's /usage.
	if n.usageCLI != nil {
		n.mu.Lock()
		i := n.infos["claude"]
		key := i.Binary + "@" + i.Version
		noCLI := n.usageNoCLI[key]
		n.mu.Unlock()
		switch {
		case !i.Available || !i.LoggedIn || i.Binary == "":
			tried = append(tried, "claude -p /usage: not asked (no signed-in Claude Code found here)")
		case noCLI:
			tried = append(tried, "claude -p /usage: not asked (this version has no /usage outside a terminal)")
		default:
			u := n.usageCLI.Read(ctx, i.Binary)
			switch u.Unavailable {
			case "":
				u.Source = "claude_code"
				u.Detail = strings.Join(tried, "; ")
				return u, older
			case claude.CLINoReading:
				u.Detail = strings.Join(append(tried, u.Detail), "; ")
				return u, older
			case claude.CLIUnsupported:
				n.mu.Lock()
				n.usageNoCLI[key] = true
				n.mu.Unlock()
				n.log.Info("Claude Code here has no /usage outside a terminal; plan usage from the usage endpoint", "version", i.Version)
			}
			tried = append(tried, u.Detail)
		}
	}
	// 3. The usage endpoint.
	if n.usageSrc == nil {
		return claude.Usage{Unavailable: "signed_out", FetchedAt: time.Now(), Detail: strings.Join(tried, "; ")}, older
	}
	u = n.usageSrc.Read(ctx)
	if u.Unavailable == "" {
		u.Source = "api"
	} else if !strings.HasPrefix(u.Detail, "usage endpoint") {
		u.Detail = strings.TrimSuffix("usage endpoint: "+u.Unavailable+"; "+u.Detail, "; ")
	}
	if u.Detail != "" {
		tried = append(tried, u.Detail)
	}
	u.Detail = strings.Join(tried, "; ")
	return u, older
}

// loadUsage reads the last good reading kept, as stale until a fresh one.
func (n *Machine) loadUsage() {
	b, err := n.st.Get(kClaudeUsage)
	if err != nil || b == nil {
		return
	}
	u := &machinev1.ClaudeUsage{}
	if proto.Unmarshal(b, u) == nil && len(u.Windows) > 0 {
		u.Stale = true
		n.usage = u
	}
}

// usageToProto turns a reading into the status message. When no reading
// came (whatever the reason) but there is a last good one, its windows are
// kept, with when they were read, and marked stale, so a failure never
// blanks the figures (工单 #86); why the refresh failed and any renew
// outcome still show. With no last reading, the unavailability is reported
// as it is.
func usageToProto(u claude.Usage, prev *machinev1.ClaudeUsage) *machinev1.ClaudeUsage {
	if u.Unavailable != "" && prev != nil && len(prev.Windows) > 0 {
		out := proto.Clone(prev).(*machinev1.ClaudeUsage)
		out.Stale = true
		out.Renew = u.Renew
		out.Detail = u.Detail
		return out
	}
	pb := &machinev1.ClaudeUsage{
		FetchedAt:   timestamppb.New(u.FetchedAt),
		Unavailable: u.Unavailable,
		Renew:       u.Renew,
		Detail:      u.Detail,
		Source:      u.Source,
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
	prevUnavail, prevRenew, prevDetail := "", "", ""
	if prev != nil {
		prevUnavail, prevRenew, prevDetail = prev.Unavailable, prev.Renew, prev.Detail
	}
	if now.Unavailable != prevUnavail || (now.Unavailable != "" && now.Detail != prevDetail) {
		if now.Unavailable == "" {
			n.log.Info("Claude plan usage readable again", "source", now.Source, "detail", now.Detail)
		} else {
			n.log.Info("Claude plan usage not readable", "why", now.Unavailable, "detail", now.Detail)
		}
	}
	if now.Renew != "" && now.Renew != prevRenew {
		n.log.Info("Claude plan usage token auto-renew", "outcome", now.Renew)
	}
}
