package machine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/claude"
)

// fakeUsage returns queued readings, counting the calls.
type fakeUsage struct {
	mu    sync.Mutex
	out   []claude.Usage
	calls int
}

func (f *fakeUsage) Read(context.Context) claude.Usage {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(f.out) == 0 {
		return claude.Usage{}
	}
	u := f.out[0]
	if len(f.out) > 1 {
		f.out = f.out[1:]
	}
	return u
}

func (f *fakeUsage) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

func openTestMachine(t *testing.T) *Machine {
	t.Helper()
	n, err := Open(Config{DataDir: t.TempDir(), Providers: map[string]provider.Provider{"claude": &claude.Provider{Binary: "/nonexistent/claude"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	// Check agents once so the Claude row is listed (it is listed whether or
	// not Claude Code is found).
	n.Discover(context.Background())
	return n
}

// claudeAgent returns the status row for Claude Code.
func claudeAgent(st *machinev1.MachineStatus) *machinev1.AgentStatus {
	for _, a := range st.Agents {
		if a.Name == "claude" {
			return a
		}
	}
	return nil
}

func waitUsage(t *testing.T, n *Machine) *machinev1.ClaudeUsage {
	t.Helper()
	for i := 0; i < 200; i++ {
		if u := claudeAgent(n.Status()).GetUsage(); u != nil {
			return u
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("usage never attached to the Claude agent")
	return nil
}

func TestStatusAttachesUsage(t *testing.T) {
	n := openTestMachine(t)
	f := &fakeUsage{out: []claude.Usage{{
		Windows:   []claude.Window{{Key: "five_hour", Utilization: 20, ResetsAt: time.Now().Add(time.Hour)}, {Key: "seven_day", Utilization: 59}},
		FetchedAt: time.Now(),
	}}}
	n.usageSrc = f

	// First Status kicks a background fetch; the reading follows.
	u := waitUsage(t, n)
	if len(u.Windows) != 2 || u.Windows[0].Key != "five_hour" {
		t.Fatalf("windows = %+v", u.Windows)
	}
	if u.Unavailable != "" {
		t.Fatalf("unavailable = %q", u.Unavailable)
	}

	// Within the TTL no further fetch is made, however often status is read.
	for i := 0; i < 5; i++ {
		n.Status()
	}
	if c := f.count(); c != 1 {
		t.Fatalf("usage fetched %d times within TTL, want 1", c)
	}
}

func TestUsageStaleFallback(t *testing.T) {
	n := openTestMachine(t)
	good := claude.Usage{Windows: []claude.Window{{Key: "five_hour", Utilization: 42}}, FetchedAt: time.Now()}
	f := &fakeUsage{out: []claude.Usage{good, {Unavailable: "api_error", Detail: "usage endpoint: HTTP 500: overloaded", FetchedAt: time.Now()}}}
	n.usageSrc = f

	first := waitUsage(t, n)
	if first.Stale || len(first.Windows) != 1 {
		t.Fatalf("first reading = %+v", first)
	}

	// Force the TTL to lapse and fetch again: the endpoint now fails, so the
	// last good windows are kept and marked stale.
	n.mu.Lock()
	n.usageAt = time.Now().Add(-2 * usageEvery)
	n.mu.Unlock()
	n.Status() // kicks the second fetch
	for i := 0; i < 200; i++ {
		u := claudeAgent(n.Status()).GetUsage()
		if u != nil && u.Stale {
			if len(u.Windows) != 1 || u.Windows[0].Utilization != 42 {
				t.Fatalf("stale windows = %+v", u.Windows)
			}
			// Why the refresh failed rides along with the kept figures (#35).
			if u.Detail != "usage endpoint: HTTP 500: overloaded" {
				t.Fatalf("stale detail = %q", u.Detail)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("stale fallback never applied")
}

// fakeCLI answers as Claude Code would, counting the calls.
type fakeCLI struct {
	mu    sync.Mutex
	out   claude.Usage
	calls int
}

func (f *fakeCLI) Read(context.Context, string) claude.Usage {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.out
}

func (f *fakeCLI) set(u claude.Usage) { f.mu.Lock(); defer f.mu.Unlock(); f.out = u }

func (f *fakeCLI) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

// Plan usage first through Claude Code signed in here (工单 #86): its reading
// is the one shown, the endpoint not asked; Claude Code answering without a
// reading keeps the last one, stale, and the endpoint is still not asked; a
// version without /usage outside a terminal falls back to the endpoint, and
// is not asked again; the last good reading outlives a restart, stale.
func TestUsageThroughClaudeCode(t *testing.T) {
	n := openTestMachine(t)
	n.mu.Lock()
	n.infos["claude"] = provider.Info{Name: "claude", Available: true, LoggedIn: true, Binary: "/opt/claude", Version: "2.1.300"}
	n.mu.Unlock()
	api := &fakeUsage{out: []claude.Usage{{Windows: []claude.Window{{Key: "five_hour", Utilization: 1}}, FetchedAt: time.Now()}}}
	cli := &fakeCLI{out: claude.Usage{Windows: []claude.Window{{Key: "five_hour", Utilization: 20}, {Key: "seven_day", Utilization: 59}}, FetchedAt: time.Now()}}
	n.usageSrc, n.usageCLI = api, cli
	again := func() *machinev1.ClaudeUsage {
		t.Helper()
		n.mu.Lock()
		n.usageFetch = true // no Status meanwhile starts one of its own
		n.mu.Unlock()
		n.refreshUsage()
		n.mu.Lock()
		defer n.mu.Unlock()
		return n.usage
	}

	u := again()
	if u.Source != "claude_code" || u.Stale || len(u.Windows) != 2 || u.Windows[0].Utilization != 20 {
		t.Fatalf("through Claude Code: %+v", u)
	}
	if api.count() != 0 {
		t.Fatal("the endpoint was asked though Claude Code answered")
	}

	cli.set(claude.Usage{Unavailable: claude.CLINoReading, Detail: "claude -p /usage: no plan usage in its answer", FetchedAt: time.Now()})
	u = again()
	if !u.Stale || len(u.Windows) != 2 || u.Windows[0].Utilization != 20 || u.FetchedAt == nil {
		t.Fatalf("no reading: %+v, want the last one, stale, with when it was read", u)
	}
	if api.count() != 0 {
		t.Fatal("the endpoint was asked after Claude Code had no reading")
	}

	// A restart: the last good reading is there at once, stale.
	n.mu.Lock()
	n.usage = nil
	n.mu.Unlock()
	n.loadUsage()
	if u := n.usage; u == nil || !u.Stale || len(u.Windows) != 2 || u.Windows[0].Utilization != 20 {
		t.Fatalf("after a restart: %+v", u)
	}

	cli.set(claude.Usage{Unavailable: claude.CLIUnsupported, FetchedAt: time.Now()})
	u = again()
	if u.Source != "api" || u.Stale || u.Windows[0].Utilization != 1 {
		t.Fatalf("fallback: %+v", u)
	}
	calls := cli.count()
	again()
	if cli.count() != calls {
		t.Fatal("a Claude Code without /usage outside a terminal was asked again")
	}
	if api.count() != 2 {
		t.Fatalf("endpoint asked %d times, want 2", api.count())
	}
}

func TestUsageDisabledByDefault(t *testing.T) {
	// No usageSrc: status carries no usage and nothing is fetched.
	n := openTestMachine(t)
	for i := 0; i < 3; i++ {
		if u := claudeAgent(n.Status()).GetUsage(); u != nil {
			t.Fatalf("usage attached with no source: %+v", u)
		}
	}
}

// fakeCache answers as Claude Code's cache would, counting the calls.
type fakeCache struct {
	mu    sync.Mutex
	out   claude.Usage
	calls int
}

func (f *fakeCache) Read() claude.Usage {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.out
}

func (f *fakeCache) set(u claude.Usage) { f.mu.Lock(); defer f.mu.Unlock(); f.out = u }

// Plan usage from the first source that has the plan's windows (工单 #104):
// Claude Code's /usage writing only that call's own totals is no reading,
// and the endpoint is asked; Claude Code's cache, when fresh, is the
// reading, nothing else asked; every source failing keeps the last reading
// with when it was read, and says why at each source; a cache too old to be
// the reading still stands in for an older last reading.
func TestUsageSourcesInOrder(t *testing.T) {
	n := openTestMachine(t)
	n.mu.Lock()
	n.infos["claude"] = provider.Info{Name: "claude", Available: true, LoggedIn: true, Binary: "/opt/claude", Version: "2.1.330"}
	n.mu.Unlock()
	cache := &fakeCache{out: claude.Usage{Unavailable: claude.CacheNone, Detail: "Claude Code cache: .claude.json keeps no plan usage (Claude Code here has not read it)"}}
	cli := &fakeCLI{out: claude.Usage{Unavailable: claude.CLISessionOnly, Detail: "claude -p /usage: only this call's own totals, no plan windows (its sign-in here does not read the plan)", FetchedAt: time.Now()}}
	api := &fakeUsage{out: []claude.Usage{
		{Windows: []claude.Window{{Key: "five_hour", Utilization: 33}, {Key: "seven_day", Utilization: 61}}, FetchedAt: time.Now()},
		{Unavailable: "api_error", Detail: "usage endpoint: HTTP 429: rate_limit_error: Rate limited.", FetchedAt: time.Now()},
	}}
	n.usageCache, n.usageCLI, n.usageSrc = cache, cli, api
	again := func() *machinev1.ClaudeUsage {
		t.Helper()
		n.mu.Lock()
		n.usageFetch = true
		n.mu.Unlock()
		n.refreshUsage()
		n.mu.Lock()
		defer n.mu.Unlock()
		return n.usage
	}

	// /usage gives only the call's own totals: not taken, the endpoint asked.
	u := again()
	if u.Source != "api" || u.Stale || len(u.Windows) != 2 || u.Windows[0].Utilization != 33 {
		t.Fatalf("past the call's own totals: %+v", u)
	}
	if cli.count() != 1 || api.count() != 1 {
		t.Fatalf("asked Claude Code %d, the endpoint %d times; want 1, 1", cli.count(), api.count())
	}
	if !strings.Contains(u.Detail, "keeps no plan usage") || !strings.Contains(u.Detail, "own totals") {
		t.Fatalf("detail does not say why the earlier sources gave nothing: %q", u.Detail)
	}

	// A fresh cache is the reading, with when Claude Code read it.
	cachedAt := time.Now().Add(-2 * time.Minute).Truncate(time.Millisecond)
	cache.set(claude.Usage{Windows: []claude.Window{{Key: "five_hour", Utilization: 40}, {Key: "seven_day", Utilization: 62}}, FetchedAt: cachedAt})
	u = again()
	if u.Source != "claude_code_cache" || u.Stale || u.Windows[0].Utilization != 40 || !u.FetchedAt.AsTime().Equal(cachedAt) {
		t.Fatalf("fresh cache: %+v", u)
	}
	if cli.count() != 1 || api.count() != 1 {
		t.Fatal("Claude Code or the endpoint was asked though the cache was fresh")
	}

	// Every source failing: the last reading, stale, with when it was read,
	// and why at each source.
	cache.set(claude.Usage{Unavailable: claude.CacheNone, Detail: "Claude Code cache: no config file of Claude Code's here"})
	u = again()
	if !u.Stale || u.Windows[0].Utilization != 40 || !u.FetchedAt.AsTime().Equal(cachedAt) {
		t.Fatalf("all failing: %+v, want the last reading, stale, with when it was read", u)
	}
	for _, want := range []string{"no config file", "own totals", "HTTP 429"} {
		if !strings.Contains(u.Detail, want) {
			t.Fatalf("detail %q lacks %q", u.Detail, want)
		}
	}

	// A cache too old to be the reading, but newer than the last reading,
	// stands in for it, stale; older than the last reading, it does not.
	n.mu.Lock()
	n.usage.FetchedAt = timestamppb.New(time.Now().Add(-3 * time.Hour))
	n.mu.Unlock()
	oldAt := time.Now().Add(-40 * time.Minute).Truncate(time.Millisecond)
	cache.set(claude.Usage{Windows: []claude.Window{{Key: "five_hour", Utilization: 55}}, FetchedAt: oldAt})
	u = again()
	if !u.Stale || u.Windows[0].Utilization != 55 || !u.FetchedAt.AsTime().Equal(oldAt) || u.Source != "claude_code_cache" || !strings.Contains(u.Detail, "too old") {
		t.Fatalf("older cache: %+v", u)
	}
	cache.set(claude.Usage{Windows: []claude.Window{{Key: "five_hour", Utilization: 70}}, FetchedAt: oldAt.Add(-time.Hour)})
	if u = again(); !u.Stale || u.Windows[0].Utilization != 55 {
		t.Fatalf("a cache older than the last reading: %+v", u)
	}
}
