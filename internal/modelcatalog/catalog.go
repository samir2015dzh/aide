// Package modelcatalog loads, caches, and periodically syncs open model
// metadata (context window, output limit, capability tags, and reference USD
// pricing per 1M tokens) from MIT-licensed open catalogs (models.dev and
// LiteLLM). It is strictly enrichment-only: local agent discovery decides
// which models exist, and unknown or custom models are never fabricated or
// fuzzy-matched.
package modelcatalog

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultModelsDevURL is anomalyco/models.dev (MIT): model IDs, context
	// windows, capabilities, and per-1M-token USD pricing.
	DefaultModelsDevURL = "https://models.dev/api.json"
	// DefaultLiteLLMURL is BerriAI/litellm (MIT): per-token pricing and context
	// window limits.
	DefaultLiteLLMURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

	// DefaultSyncInterval is how often background sync refreshes open model data.
	DefaultSyncInterval = 24 * time.Hour
	// DefaultRetryInterval is how soon background sync tries again after a
	// sync in which either source failed.
	DefaultRetryInterval = time.Hour

	sourceSnapshot = "snapshot"
	sourceCache    = "cache"
	sourceSynced   = "synced"

	fetchTimeout = 20 * time.Second
	maxBodyBytes = 32 << 20 // 32 MiB guard
)

// embeddedSnapshot is generated from both upstream catalogs by BuildSnapshot;
// regenerate it with `go run ./internal/modelcatalog/gen`, never by hand.
//
//go:embed snapshot.json
var embeddedSnapshot []byte

// Entry holds normalized metadata for a single model ID.
type Entry struct {
	ID                   string   `json:"id"`
	ContextWindow        uint32   `json:"context_window,omitempty"`
	MaxOutputTokens      uint32   `json:"max_output_tokens,omitempty"`
	Capabilities         []string `json:"capabilities,omitempty"`
	InputCostPerMillion  float64  `json:"input_cost_per_million,omitempty"`
	OutputCostPerMillion float64  `json:"output_cost_per_million,omitempty"`
}

// Status describes the current state of the catalog on a machine.
type Status struct {
	Source        string
	ModelCount    int
	SyncedAt      time.Time
	LastAttemptAt time.Time
	LastError     string
}

// Options configures a Catalog instance.
type Options struct {
	CachePath    string
	HTTPClient   *http.Client
	ModelsDevURL string
	LiteLLMURL   string
	SyncInterval time.Duration
	// RetryInterval is the wait after a failed or partial sync (default
	// DefaultRetryInterval, never longer than SyncInterval).
	RetryInterval time.Duration
	Logger        *slog.Logger
	OnUpdate      func()
	Now           func() time.Time
}

// cacheVersion marks caches written with first-party-only bare ids (审计 G).
// Older caches mixed reseller prices into them and are ignored, so the
// catalog falls back to the snapshot and syncs again at once.
const cacheVersion = 2

type persistedCatalog struct {
	Version  int              `json:"version,omitempty"`
	Source   string           `json:"source"`
	SyncedAt time.Time        `json:"synced_at,omitempty"`
	Models   map[string]Entry `json:"models"`
}

// Catalog stores open model metadata in memory, backed by an embedded snapshot
// and an optional persistent cache refreshed in the background.
type Catalog struct {
	mu            sync.RWMutex
	entries       map[string]Entry
	status        Status
	cachePath     string
	httpClient    *http.Client
	modelsDevURL  string
	litellmURL    string
	syncInterval  time.Duration
	retryInterval time.Duration
	log           *slog.Logger
	onUpdate      func()
	now           func() time.Time
}

// Open initializes a Catalog from the embedded snapshot and overlays any valid
// persisted cache file at opts.CachePath. It makes no network calls.
func Open(opts Options) *Catalog {
	if opts.HTTPClient == nil {
		opts.HTTPClient = http.DefaultClient
	}
	if opts.ModelsDevURL == "" {
		opts.ModelsDevURL = DefaultModelsDevURL
	}
	if opts.LiteLLMURL == "" {
		opts.LiteLLMURL = DefaultLiteLLMURL
	}
	if opts.SyncInterval <= 0 {
		opts.SyncInterval = DefaultSyncInterval
	}
	if opts.RetryInterval <= 0 {
		opts.RetryInterval = DefaultRetryInterval
	}
	if opts.RetryInterval > opts.SyncInterval {
		opts.RetryInterval = opts.SyncInterval
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	c := &Catalog{
		entries:       map[string]Entry{},
		cachePath:     opts.CachePath,
		httpClient:    opts.HTTPClient,
		modelsDevURL:  opts.ModelsDevURL,
		litellmURL:    opts.LiteLLMURL,
		syncInterval:  opts.SyncInterval,
		retryInterval: opts.RetryInterval,
		log:           opts.Logger,
		onUpdate:      opts.OnUpdate,
		now:           opts.Now,
	}
	// Cold start: load built-in snapshot first so offline hosts work immediately.
	if snap, err := decodePersisted(embeddedSnapshot); err == nil && len(snap.Models) > 0 {
		c.entries = normalizeMap(snap.Models)
		c.status = Status{
			Source:     sourceSnapshot,
			ModelCount: len(c.entries),
			SyncedAt:   snap.SyncedAt,
		}
	}
	// Overlay persisted cache when available and non-empty.
	if c.cachePath != "" {
		if raw, err := os.ReadFile(c.cachePath); err == nil {
			if cached, err := decodePersisted(raw); err == nil && cached.Version == cacheVersion && len(cached.Models) > 0 {
				c.entries = normalizeMap(cached.Models)
				c.status = Status{
					Source:     sourceCache,
					ModelCount: len(c.entries),
					SyncedAt:   cached.SyncedAt,
				}
			}
		}
	}
	return c
}

// Status returns a snapshot of the catalog's sync state.
func (c *Catalog) Status() Status {
	if c == nil {
		return Status{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// Lookup finds metadata for a discovered model using strict exact matching
// only (case-insensitive). It prefers resolved over id and, when a candidate
// has a "provider/model" form, checks both the full string and the exact part
// after the last '/'. It never performs prefix or fuzzy matching.
func (c *Catalog) Lookup(id, resolved string) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, raw := range []string{resolved, id} {
		k := strings.ToLower(strings.TrimSpace(raw))
		if k == "" {
			continue
		}
		if e, ok := c.entries[k]; ok {
			return cloneEntry(e), true
		}
		if idx := strings.LastIndex(k, "/"); idx >= 0 && idx+1 < len(k) {
			if e, ok := c.entries[k[idx+1:]]; ok {
				return cloneEntry(e), true
			}
		}
	}
	return Entry{}, false
}

// EstimateCostUSD calculates an estimated USD cost from reference per-1M-token
// prices when the model matches an exact entry with non-zero pricing.
func (c *Catalog) EstimateCostUSD(id, resolved string, inputTokens, outputTokens uint64) (float64, bool) {
	if inputTokens == 0 && outputTokens == 0 {
		return 0, false
	}
	e, ok := c.Lookup(id, resolved)
	if !ok || (e.InputCostPerMillion <= 0 && e.OutputCostPerMillion <= 0) {
		return 0, false
	}
	usd := (float64(inputTokens)*e.InputCostPerMillion + float64(outputTokens)*e.OutputCostPerMillion) / 1_000_000.0
	if usd <= 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return 0, false
	}
	return usd, true
}

// Run refreshes the catalog in the background once every syncInterval (and
// immediately on start when the current data is from the built-in snapshot or
// older than syncInterval). After a sync in which either source failed it
// tries again after retryInterval. Errors are logged and reflected in Status
// without stopping the loop or disturbing callers.
func (c *Catalog) Run(ctx context.Context) {
	if c == nil {
		return
	}
	// Sync at once unless a cached full sync is still fresh; then wait out
	// the rest of its interval.
	var wait time.Duration
	if st := c.Status(); st.Source == sourceCache && !st.SyncedAt.IsZero() {
		if age := c.now().Sub(st.SyncedAt); age >= 0 && age < c.syncInterval {
			wait = c.syncInterval - age
		}
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// A failed or partial sync is retried after retryInterval, not
			// a whole syncInterval later.
			next := c.syncInterval
			if err := c.Sync(ctx); err != nil {
				next = c.retryInterval
			}
			t.Reset(next)
		}
	}
}

// Sync fetches models.dev and LiteLLM data and merges them with the built-in
// snapshot baseline, updates in-memory state, and persists the cache.
//
// Only a sync in which both sources answer replaces the catalog and advances
// SyncedAt. When one source fails, the other's fresh data is overlaid on the
// current catalog, so what the failed source contributed last time (from the
// previous sync or the cache) stays, SyncedAt keeps the time of the last full
// sync, and the error is returned so Run retries soon. If both sources fail,
// existing entries remain intact. LastError records any failure.
func (c *Catalog) Sync(ctx context.Context) error {
	attemptAt := c.now()
	var errs []string
	modelsDevEntries, err := c.fetchSource(ctx, c.modelsDevURL, ParseModelsDev)
	if err != nil {
		errs = append(errs, "models.dev: "+err.Error())
	}
	litellmEntries, err := c.fetchSource(ctx, c.litellmURL, ParseLiteLLM)
	if err != nil {
		errs = append(errs, "litellm: "+err.Error())
	}

	if len(modelsDevEntries) == 0 && len(litellmEntries) == 0 {
		errMsg := strings.Join(errs, "; ")
		if errMsg == "" {
			errMsg = "no upstream catalog entries returned"
		}
		c.mu.Lock()
		c.status.LastAttemptAt = attemptAt
		c.status.LastError = errMsg
		c.mu.Unlock()
		c.log.Warn("model catalog sync failed; keeping existing catalog", "err", errMsg)
		if c.onUpdate != nil {
			c.onUpdate()
		}
		return errors.New(errMsg)
	}

	partialErr := strings.Join(errs, "; ")
	c.mu.RLock()
	prevEntries, prevSyncedAt := c.entries, c.status.SyncedAt
	c.mu.RUnlock()
	// A full sync starts from the built-in snapshot baseline so curated
	// offline models are never dropped if one upstream omits them. A partial
	// one starts from the current catalog instead, which still holds the
	// failed source's last synced data.
	base, syncedAt := snapshotEntries(), attemptAt
	if partialErr != "" {
		base, syncedAt = maps.Clone(prevEntries), prevSyncedAt
	}
	merged := mergeSources(base, modelsDevEntries, litellmEntries)

	if partialErr != "" {
		c.log.Warn("model catalog sync completed with partial source error", "models", len(merged), "err", partialErr)
	} else {
		c.log.Info("model catalog synced", "models", len(merged))
	}

	c.mu.Lock()
	c.entries = merged
	c.status = Status{
		Source:        sourceSynced,
		ModelCount:    len(merged),
		SyncedAt:      syncedAt,
		LastAttemptAt: attemptAt,
		LastError:     partialErr,
	}
	c.mu.Unlock()

	if c.cachePath != "" {
		if err := c.writeCache(persistedCatalog{
			Version:  cacheVersion,
			Source:   sourceSynced,
			SyncedAt: syncedAt,
			Models:   merged,
		}); err != nil {
			c.log.Warn("model catalog cache write failed", "err", err)
		}
	}
	if c.onUpdate != nil {
		c.onUpdate()
	}
	if partialErr != "" {
		return errors.New(partialErr)
	}
	return nil
}

// fetchSource downloads and parses one upstream catalog. An unset URL means
// the source is disabled; a source that parses to no entries is a failure.
func (c *Catalog) fetchSource(ctx context.Context, rawURL string, parse func([]byte) (map[string]Entry, error)) (map[string]Entry, error) {
	if rawURL == "" {
		return nil, nil
	}
	body, err := c.fetch(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	parsed, err := parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if len(parsed) == 0 {
		return nil, errors.New("no entries")
	}
	return parsed, nil
}

// snapshotEntries decodes the embedded built-in snapshot.
func snapshotEntries() map[string]Entry {
	snap, err := decodePersisted(embeddedSnapshot)
	if err != nil {
		return map[string]Entry{}
	}
	return normalizeMap(snap.Models)
}

// mergeSources overlays models.dev and then LiteLLM entries onto base. Both
// sync and the snapshot generator use it, so the two agree field by field.
func mergeSources(base, modelsDev, litellm map[string]Entry) map[string]Entry {
	merged := base
	if merged == nil {
		merged = map[string]Entry{}
	}
	for k, v := range modelsDev {
		merged[k] = mergeEntry(merged[k], v)
	}
	for k, v := range litellm {
		merged[k] = mergeEntry(merged[k], v)
	}
	return merged
}

func (c *Catalog) fetch(ctx context.Context, rawURL string) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBodyBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxBodyBytes)
	}
	return data, nil
}

func (c *Catalog) writeCache(p persistedCatalog) error {
	if err := os.MkdirAll(filepath.Dir(c.cachePath), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp := c.cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.cachePath)
}

// ParseModelsDev parses anomalyco/models.dev api.json into normalized entries.
// Pricing in models.dev is already USD per 1M tokens.
//
// models.dev lists the same model id under every provider that serves it,
// resellers included, each with its own price and limits. Every listing is
// kept under its provider-qualified key ("opencode/kimi-k2"), but the bare id
// ("kimi-k2") is taken whole from the model's first-party provider only: the
// publisher named by its family (see families) or, for families not listed
// there, by models.dev's canonical_model_id. With no first-party listing the
// bare id is left out rather than borrowed from a reseller.
func ParseModelsDev(data []byte) (map[string]Entry, error) {
	providers, err := decodeModelsDev(data)
	if err != nil {
		return nil, err
	}
	type listing struct {
		provider, key string
		entry         Entry
	}
	out := map[string]Entry{}
	listings := map[string][]listing{} // bare id -> listings in provider/model order
	canonical := map[string][]string{} // bare id -> canonical owners named by listings
	// Deterministic provider ordering so repeated parses yield identical results.
	provKeys := make([]string, 0, len(providers))
	for k := range providers {
		provKeys = append(provKeys, k)
	}
	slices.Sort(provKeys)
	for _, pk := range provKeys {
		p := providers[pk]
		pk = strings.ToLower(strings.TrimSpace(pk))
		modelKeys := make([]string, 0, len(p.Models))
		for mk := range p.Models {
			modelKeys = append(modelKeys, mk)
		}
		slices.Sort(modelKeys)
		for _, mk := range modelKeys {
			m := p.Models[mk]
			id := strings.TrimSpace(m.ID)
			if id == "" {
				id = strings.TrimSpace(mk)
			}
			key := strings.ToLower(id)
			if key == "" {
				continue
			}
			e := m.entry(id)
			if isEmptyEntry(e) {
				continue
			}
			bare := bareID(key)
			if bare == "" {
				continue
			}
			if pk != "" {
				scoped := key
				if !strings.HasPrefix(key, pk+"/") {
					scoped = pk + "/" + key
				}
				if _, exists := out[scoped]; !exists {
					se := e
					se.ID = scoped
					out[scoped] = se
				}
			}
			listings[bare] = append(listings[bare], listing{provider: pk, key: key, entry: e})
			if owner, _, ok := strings.Cut(strings.ToLower(strings.TrimSpace(m.Canonical)), "/"); ok && owner != "" && !slices.Contains(canonical[bare], owner) {
				canonical[bare] = append(canonical[bare], owner)
			}
		}
	}
	for bare, ls := range listings {
		var owners []string
		if f := familyOf(bare); f != nil {
			owners = f.modelsDev
		} else {
			owners = canonical[bare]
			slices.Sort(owners)
		}
		found := false
		for _, owner := range owners {
			for _, l := range ls {
				if l.provider != owner {
					continue
				}
				// A provider may list a model both bare and under its
				// own namespace; the bare listing is the one to keep.
				if !found || l.key == bare {
					e := l.entry
					if i := strings.LastIndex(e.ID, "/"); i >= 0 {
						e.ID = e.ID[i+1:]
					}
					out[bare] = e
					found = true
				}
			}
			if found {
				break
			}
		}
	}
	return out, nil
}

type modelsDevModel struct {
	ID         string `json:"id"`
	Canonical  string `json:"canonical_model_id"`
	ToolCall   bool   `json:"tool_call"`
	Reasoning  bool   `json:"reasoning"`
	Attachment bool   `json:"attachment"`
	Structured bool   `json:"structured_output"`
	Modalities struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	Limit struct {
		Context uint32 `json:"context"`
		Output  uint32 `json:"output"`
	} `json:"limit"`
	Cost struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
	} `json:"cost"`
}

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

func decodeModelsDev(data []byte) (map[string]modelsDevProvider, error) {
	var providers map[string]modelsDevProvider
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, err
	}
	return providers, nil
}

func (m modelsDevModel) entry(id string) Entry {
	var caps []string
	if m.Attachment {
		caps = append(caps, "attachments")
	}
	if m.Reasoning {
		caps = append(caps, "reasoning")
	}
	if m.Structured {
		caps = append(caps, "structured_output")
	}
	if m.ToolCall {
		caps = append(caps, "tools")
	}
	for _, mod := range m.Modalities.Input {
		if strings.EqualFold(mod, "image") {
			caps = append(caps, "vision")
			break
		}
	}
	return Entry{
		ID:                   id,
		ContextWindow:        m.Limit.Context,
		MaxOutputTokens:      m.Limit.Output,
		Capabilities:         normalizeCaps(caps),
		InputCostPerMillion:  cleanPrice(m.Cost.Input),
		OutputCostPerMillion: cleanPrice(m.Cost.Output),
	}
}

// ParseLiteLLM parses BerriAI/litellm model_prices_and_context_window.json into
// normalized entries. LiteLLM stores costs in USD per single token, so they are
// scaled by 1,000,000 to USD per 1M tokens.
//
// Keys are kept as LiteLLM names them ("azure/gpt-5", "deepseek/deepseek-chat").
// For a model family with a known publisher (see families), the bare id comes
// only from a first-party litellm_provider: its unprefixed key first, then a
// first-party prefixed key such as "deepseek/deepseek-chat". An unprefixed key
// served by someone else (a cloud reseller) is kept under that provider's
// prefix instead, so it never stands in for the publisher's own entry.
func ParseLiteLLM(data []byte) (map[string]Entry, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	type litellmItem struct {
		MaxTokens               flexUint32 `json:"max_tokens"`
		MaxInputTokens          flexUint32 `json:"max_input_tokens"`
		MaxOutputTokens         flexUint32 `json:"max_output_tokens"`
		InputCostPerToken       float64    `json:"input_cost_per_token"`
		OutputCostPerToken      float64    `json:"output_cost_per_token"`
		SupportsVision          bool       `json:"supports_vision"`
		SupportsFunctionCalling bool       `json:"supports_function_calling"`
		SupportsReasoning       bool       `json:"supports_reasoning"`
		SupportsResponseSchema  bool       `json:"supports_response_schema"`
		Provider                string     `json:"litellm_provider"`
	}
	type candidate struct {
		prefixed bool
		rank     int
		entry    Entry
	}
	firstParty := map[string]candidate{} // bare id -> best first-party entry
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := map[string]Entry{}
	for _, k := range keys {
		if k == "sample_spec" {
			continue
		}
		var item litellmItem
		if err := json.Unmarshal(raw[k], &item); err != nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(k))
		if key == "" {
			continue
		}
		ctxWin := uint32(item.MaxInputTokens)
		maxOut := uint32(item.MaxOutputTokens)
		if ctxWin == 0 {
			ctxWin = uint32(item.MaxTokens)
		} else if maxOut == 0 {
			maxOut = uint32(item.MaxTokens)
		}
		var caps []string
		if item.SupportsReasoning {
			caps = append(caps, "reasoning")
		}
		if item.SupportsResponseSchema {
			caps = append(caps, "structured_output")
		}
		if item.SupportsFunctionCalling {
			caps = append(caps, "tools")
		}
		if item.SupportsVision {
			caps = append(caps, "vision")
		}
		e := Entry{
			ID:                   k,
			ContextWindow:        ctxWin,
			MaxOutputTokens:      maxOut,
			Capabilities:         normalizeCaps(caps),
			InputCostPerMillion:  cleanPrice(item.InputCostPerToken * 1_000_000.0),
			OutputCostPerMillion: cleanPrice(item.OutputCostPerToken * 1_000_000.0),
		}
		if isEmptyEntry(e) {
			continue
		}
		bare := bareID(key)
		prefixed := bare != key
		if f := familyOf(bare); f != nil {
			prov := strings.ToLower(strings.TrimSpace(item.Provider))
			if rank := slices.Index(f.litellm, prov); rank >= 0 {
				c, seen := firstParty[bare]
				if !seen || (c.prefixed && !prefixed) || (c.prefixed == prefixed && rank < c.rank) {
					fe := e
					fe.ID = strings.TrimSpace(k)
					fe.ID = fe.ID[strings.LastIndex(fe.ID, "/")+1:]
					firstParty[bare] = candidate{prefixed: prefixed, rank: rank, entry: fe}
				}
				if !prefixed {
					continue
				}
			} else if !prefixed {
				if prov == "" {
					continue
				}
				key = prov + "/" + key
				e.ID = key
			}
		}
		out[key] = mergeEntry(out[key], e)
	}
	for bare, c := range firstParty {
		out[bare] = c.entry
	}
	return out, nil
}

// flexUint32 tolerates integer, float, or string numbers in upstream JSON.
type flexUint32 uint32

func (f *flexUint32) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "" || s == "null" {
		return nil
	}
	var n float64
	if err := json.Unmarshal(b, &n); err == nil {
		if n > 0 && n <= math.MaxUint32 {
			*f = flexUint32(uint32(n))
		}
		return nil
	}
	return nil
}

func decodePersisted(data []byte) (persistedCatalog, error) {
	var p persistedCatalog
	if err := json.Unmarshal(data, &p); err != nil {
		return persistedCatalog{}, err
	}
	return p, nil
}

func normalizeMap(in map[string]Entry) map[string]Entry {
	out := make(map[string]Entry, len(in))
	for k, v := range in {
		nk := strings.ToLower(strings.TrimSpace(k))
		if nk == "" {
			continue
		}
		if v.ID == "" {
			v.ID = k
		}
		v.Capabilities = normalizeCaps(v.Capabilities)
		v.InputCostPerMillion = cleanPrice(v.InputCostPerMillion)
		v.OutputCostPerMillion = cleanPrice(v.OutputCostPerMillion)
		out[nk] = v
	}
	return out
}

func mergeEntry(base, next Entry) Entry {
	out := base
	if out.ID == "" {
		out.ID = next.ID
	}
	if next.ContextWindow > 0 {
		out.ContextWindow = next.ContextWindow
	}
	if next.MaxOutputTokens > 0 {
		out.MaxOutputTokens = next.MaxOutputTokens
	}
	if next.InputCostPerMillion > 0 {
		out.InputCostPerMillion = next.InputCostPerMillion
	}
	if next.OutputCostPerMillion > 0 {
		out.OutputCostPerMillion = next.OutputCostPerMillion
	}
	if len(next.Capabilities) > 0 {
		out.Capabilities = normalizeCaps(append(slices.Clone(out.Capabilities), next.Capabilities...))
	}
	return out
}

func normalizeCaps(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

func cleanPrice(v float64) float64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*1_000_000) / 1_000_000
}

func isEmptyEntry(e Entry) bool {
	return e.ContextWindow == 0 && e.MaxOutputTokens == 0 && len(e.Capabilities) == 0 &&
		e.InputCostPerMillion == 0 && e.OutputCostPerMillion == 0
}

func cloneEntry(e Entry) Entry {
	e.Capabilities = slices.Clone(e.Capabilities)
	return e
}
