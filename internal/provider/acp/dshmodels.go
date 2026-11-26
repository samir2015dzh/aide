package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

// DeepSeek Harness's models before its first session (工单 #166). DSH's ACP
// bridge offers no session/options: it tells its models in a session's
// config options only, so a machine where no DSH session has run yet showed
// the default model alone, and the owner was told to send a first message.
// A probe session is not that simple either: every DSH profile keeps its
// sessions in one place (<DSH home>/sessions, and their projection cache
// under storages), the DSH desktop app's list included, so a session made
// only to read its options would stay there for the owner to find.
//
// So the probe's dsh process gets one more --patch overlay that keeps both
// in a directory of its own, removed when it is done: it creates a session,
// reads the options it says, closes it, and leaves nothing in the owner's
// DSH (verified on dsh 0.2.2-rc.5: same options, no file added under the
// home). Credentials, settings and the desktop profile's providers (工单
// #129, overlaid as a session's are) are read where they are, so the
// models are the ones a session here would offer.

// dshScratch is the overlay keeping a probe's session log and projection
// cache under dir.
func dshScratch(dir string) []byte {
	return fmt.Appendf(nil, "- id: session-persistence-jsonl\n  config:\n    root: %s\n- id: storage-json\n  config:\n    root: %s\n",
		yamlString(filepath.Join(dir, "sessions")), yamlString(filepath.Join(dir, "storages")))
}

// yamlString is s as a YAML scalar (JSON is YAML): any path, Windows' too.
func yamlString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// dshProbeTTL is how long what a probe found stands before a check on
// purpose (工单 #112) asks again; other checks keep it.
const dshProbeTTL = modelCatalogTTL

type dshProbe struct {
	mu     sync.Mutex
	at     time.Time // the last probe, found or not: one failing is not tried again at every check
	found  time.Time // the last probe that found them
	why    string    // what the last probe failed at
	key    string
	offers *provider.Offers
}

// dshOffers is what a new DSH session here would offer, found as above;
// nil when it could not be found (the check then says what it knew, as
// before).
func (p *Provider) dshOffers(ctx context.Context, bin string, pre, env []string, dump []byte) (_ *provider.Offers, found time.Time, why string) {
	key := modelContext(bin, env)
	p.probe.mu.Lock()
	defer p.probe.mu.Unlock()
	// Found: kept, but a check on purpose past its TTL asks again. Not found: a
	// check on purpose asks again at once, others after the TTL.
	if p.probe.key == key && !p.probe.at.IsZero() {
		fresh := time.Since(p.probe.at) < dshProbeTTL
		onPurpose := provider.IsOnPurpose(ctx)
		if p.probe.offers != nil && (!onPurpose || fresh) || p.probe.offers == nil && !onPurpose && fresh {
			return p.probe.offers, p.probe.found, p.probe.why
		}
	}
	o, why := p.probeDSH(ctx, bin, pre, env, dump)
	if p.probe.key != key {
		p.probe.offers, p.probe.found = nil, time.Time{}
	}
	p.probe.at, p.probe.key, p.probe.why = time.Now(), key, why
	if o != nil {
		p.probe.offers, p.probe.found = o, p.probe.at
	}
	return p.probe.offers, p.probe.found, p.probe.why // a failed probe keeps what an earlier one found
}

// probeDSH is what a probe found, or, found nothing, at which step (never
// DSH's words: they may name what it was configured with).
func (p *Provider) probeDSH(ctx context.Context, bin string, pre, env []string, dump []byte) (*provider.Offers, string) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "aide-dsh-models-")
	if err != nil {
		return nil, "scratch directory"
	}
	defer os.RemoveAll(dir)
	scratch := filepath.Join(dir, "scratch.yml")
	if os.WriteFile(scratch, dshScratch(dir), 0o600) != nil {
		return nil, "scratch directory"
	}
	args := append(slices.Clone(pre), p.Args...)
	if desktop := dshDesktopProviders(env); dump != nil && desktop != nil {
		if more, _, err := dshProviderArgs(dump, desktop, dir); err == nil {
			args = append(args, more...)
		}
	}
	args = append(args, "--patch", scratch)
	work := filepath.Join(dir, "work")
	if os.Mkdir(work, 0o700) != nil {
		return nil, "scratch directory"
	}
	proc, err := provider.StartProc(ctx, bin, args, work, env)
	if err != nil {
		return nil, "start"
	}
	defer proc.Stop(time.Second)
	s := &session{name: p.Name(), proc: proc, ctx: ctx, events: make(chan provider.Event, 256), pending: map[int64]func(rpcResult){},
		known: map[string]*toolCall{}, calls: map[string]provider.Decision{}, finished: map[string]bool{},
		gate: func(context.Context, provider.ToolAction) provider.Decision {
			return provider.Decision{Reason: "model discovery only"}
		}}
	go s.read()
	if _, err := s.call(ctx, "initialize", map[string]any{"protocolVersion": 1,
		"clientInfo": map[string]any{"name": "aide", "version": "0.1.0"}, "clientCapabilities": map[string]any{}}); err != nil {
		return nil, stepFailed("initialize", ctx)
	}
	res, err := s.call(ctx, "session/new", map[string]any{"cwd": work, "mcpServers": []any{}})
	if err != nil {
		return nil, stepFailed("session/new", ctx)
	}
	var c struct {
		SessionID string `json:"sessionId"`
		sessionConfig
	}
	if json.Unmarshal(res, &c) != nil {
		return nil, "session/new answer"
	}
	o := c.offers()
	if c.SessionID != "" {
		o = settled(ctx, s, c.SessionID, &c.sessionConfig, o, time.Second, dshSettleReads)
		s.call(ctx, "session/close", map[string]any{"sessionId": c.SessionID})
	}
	if len(o.Models) == 0 {
		return nil, "no model offered"
	}
	return o, ""
}

func stepFailed(step string, ctx context.Context) string {
	if ctx.Err() != nil {
		return step + " timed out"
	}
	return step + " failed"
}

// dshSettleReads is how many times at most a probe asks again (settled).
const dshSettleReads = 6

// settled is what the session offers once it says the same twice (工单 #129
// 返修). DSH answers session/new before its credentials store has loaded
// when the session log's root is new, as a probe's always is: a provider
// whose key is stored there (rather than in the environment) lists no
// models yet, and only the model the session runs is offered — the owner's
// gateway and the DeepSeek account's models were missing (verified on dsh
// 0.2.2-rc.5: a fresh home gives deepseek-official alone, the same home a
// second later all of them). Nothing tells when it has loaded, and a
// session with no turn is sent no config_option_update, so it is asked
// again, setting the model it runs to itself (an answer is every option as
// it now is), every wait, until two answers name the same models.
func settled(ctx context.Context, s *session, id string, c *sessionConfig, o *provider.Offers, wait time.Duration, reads int) *provider.Offers {
	m := c.option("model")
	if m == nil || m.current() == "" {
		return o
	}
	configID, value := m.ID, m.current()
	for range reads {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return o
		}
		res, err := s.call(ctx, "session/set_config_option", map[string]any{"sessionId": id, "configId": configID, "value": value})
		var now sessionConfig
		if err != nil || json.Unmarshal(res, &now) != nil || len(now.ConfigOptions) == 0 {
			return o
		}
		c.ConfigOptions = now.ConfigOptions
		next := c.offers()
		if sameModels(next.Models, o.Models) {
			return next
		}
		o = next
	}
	return o
}

func sameModels(a, b []provider.Model) bool {
	return slices.EqualFunc(a, b, func(x, y provider.Model) bool { return x.ID == y.ID })
}
