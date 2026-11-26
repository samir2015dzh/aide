package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The same model ids under resellers sorted both before and after the
// first-party provider, each with different prices, limits and capabilities.
const resellerModelsDev = `{
	"aaa-resale": {"models": {
		"claude-opus-4-5": {"id": "claude-opus-4-5", "reasoning": true, "limit": {"context": 100000, "output": 8000}, "cost": {"input": 15, "output": 75}},
		"kimi-k2": {"id": "kimi-k2", "limit": {"context": 131072, "output": 8192}, "cost": {"input": 0.58, "output": 2.32}}
	}},
	"anthropic": {"models": {
		"claude-opus-4-5": {"id": "claude-opus-4-5", "tool_call": true, "modalities": {"input": ["text", "image"], "output": ["text"]}, "limit": {"context": 200000, "output": 64000}, "cost": {"input": 5, "output": 25}},
		"claude-sonnet-4-5": {"id": "claude-sonnet-4-5", "tool_call": true, "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 200000, "output": 64000}, "cost": {"input": 3, "output": 15}}
	}},
	"alibaba": {"models": {
		"kimi-k3": {"id": "kimi-k3", "limit": {"context": 64000, "output": 4000}, "cost": {"input": 9, "output": 9}}
	}},
	"google": {"models": {
		"gemini-embedding-001": {"id": "gemini-embedding-001", "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 2048, "output": 3072}, "cost": {"input": 0.15}}
	}},
	"moonshotai": {"models": {
		"kimi-k3": {"id": "kimi-k3", "modalities": {"input": ["text"], "output": ["text"]}, "limit": {"context": 262144, "output": 32768}, "cost": {"input": 0.6, "output": 2.5}}
	}},
	"openai": {"models": {
		"gpt-image-1": {"id": "gpt-image-1", "modalities": {"input": ["text"], "output": ["image"]}, "limit": {"context": 32000}, "cost": {"input": 5, "output": 40}}
	}},
	"opencode": {"models": {
		"kimi-k2": {"id": "kimi-k2", "limit": {"context": 262144, "output": 262144}, "cost": {"input": 0.4, "output": 2.5}}
	}},
	"openrouter": {"models": {
		"anthropic/claude-opus-4-5": {"id": "anthropic/claude-opus-4-5", "limit": {"context": 200000, "output": 32000}, "cost": {"input": 7, "output": 30}}
	}},
	"zzz-resale": {"models": {
		"claude-opus-4-5": {"id": "claude-opus-4-5", "structured_output": true, "limit": {"context": 1000000, "output": 1}, "cost": {"input": 99, "output": 99}},
		"acme-pro-2": {"id": "acme-pro-2", "canonical_model_id": "acme/acme-pro-2", "limit": {"context": 1, "output": 1}, "cost": {"input": 50, "output": 50}},
		"mystery-1": {"id": "mystery-1", "limit": {"context": 8000, "output": 1000}, "cost": {"input": 1, "output": 1}}
	}},
	"acme": {"models": {
		"acme-pro-2": {"id": "acme-pro-2", "canonical_model_id": "acme/acme-pro-2", "limit": {"context": 256000, "output": 32000}, "cost": {"input": 2.5, "output": 10}}
	}}
}`

const resellerLiteLLM = `{
	"sample_spec": {"max_tokens": 100},
	"azure_ai/claude-opus-4-5": {"litellm_provider": "azure_ai", "max_input_tokens": 100000, "input_cost_per_token": 0.000009, "output_cost_per_token": 0.00009},
	"claude-opus-4-5": {"litellm_provider": "anthropic", "max_input_tokens": 200000, "max_output_tokens": 64000, "input_cost_per_token": 0.000005, "output_cost_per_token": 0.000025},
	"aaa-proxy/deepseek-chat": {"litellm_provider": "aaa-proxy", "max_input_tokens": 1000, "input_cost_per_token": 0.00000019, "output_cost_per_token": 0.00000019},
	"deepseek/deepseek-chat": {"litellm_provider": "deepseek", "max_input_tokens": 131072, "max_output_tokens": 8192, "input_cost_per_token": 0.00000028, "output_cost_per_token": 0.00000042},
	"gemini-2.5-pro": {"litellm_provider": "vertex_ai-language-models", "max_input_tokens": 1048576, "input_cost_per_token": 0.00000125, "output_cost_per_token": 0.00001},
	"gemini/gemini-2.5-pro": {"litellm_provider": "gemini", "max_input_tokens": 2000, "input_cost_per_token": 0.000002, "output_cost_per_token": 0.00002},
	"kimi-k2-thinking": {"litellm_provider": "volcengine", "max_input_tokens": 256000, "input_cost_per_token": 0.0000006, "output_cost_per_token": 0.0000025},
	"vercel_ai_gateway/moonshotai/kimi-k2": {"litellm_provider": "vercel_ai_gateway", "max_input_tokens": 131072, "input_cost_per_token": 0.00000055, "output_cost_per_token": 0.0000022},
	"together-only-1": {"litellm_provider": "together_ai", "max_input_tokens": 32000, "input_cost_per_token": 0.0000002, "output_cost_per_token": 0.0000002}
}`

func TestParseModelsDevBareIDFromFirstPartyOnly(t *testing.T) {
	got, err := ParseModelsDev([]byte(resellerModelsDev))
	if err != nil {
		t.Fatal(err)
	}
	// The first-party entry wins whole, whatever sorts before or after it:
	// no reseller price, limit or capability is mixed in.
	want := Entry{ID: "claude-opus-4-5", ContextWindow: 200000, MaxOutputTokens: 64000, Capabilities: []string{"tools", "vision"}, InputCostPerMillion: 5, OutputCostPerMillion: 25}
	if e := got["claude-opus-4-5"]; !sameEntry(e, want) {
		t.Fatalf("claude-opus-4-5 = %+v, want %+v", e, want)
	}
	if e := got["kimi-k3"]; e.InputCostPerMillion != 0.6 || e.ContextWindow != 262144 {
		t.Fatalf("kimi-k3 = %+v, want moonshotai's", e)
	}
	// A family without a family rule uses canonical_model_id's owner.
	if e := got["acme-pro-2"]; e.InputCostPerMillion != 2.5 || e.ContextWindow != 256000 {
		t.Fatalf("acme-pro-2 = %+v, want acme's", e)
	}
	// With no first-party listing there is no bare entry at all.
	for _, id := range []string{"kimi-k2", "mystery-1"} {
		if e, ok := got[id]; ok {
			t.Fatalf("%s taken from a reseller: %+v", id, e)
		}
	}
	// Reseller listings stay addressable by provider-qualified key.
	for key, price := range map[string]float64{
		"aaa-resale/claude-opus-4-5":           15,
		"zzz-resale/claude-opus-4-5":           99,
		"anthropic/claude-opus-4-5":            5,
		"opencode/kimi-k2":                     0.4,
		"openrouter/anthropic/claude-opus-4-5": 7,
		"zzz-resale/mystery-1":                 1,
		"alibaba/kimi-k3":                      9,
		"moonshotai/kimi-k3":                   0.6,
		"aaa-resale/kimi-k2":                   0.58,
		"zzz-resale/acme-pro-2":                50,
	} {
		if e, ok := got[key]; !ok || e.InputCostPerMillion != price {
			t.Fatalf("%s = (%+v, %v), want input price %v", key, e, ok, price)
		}
	}
}

func TestParseLiteLLMPrefersFirstParty(t *testing.T) {
	got, err := ParseLiteLLM([]byte(resellerLiteLLM))
	if err != nil {
		t.Fatal(err)
	}
	for id, price := range map[string]float64{
		"claude-opus-4-5":             5,    // unprefixed anthropic, not azure_ai
		"deepseek-chat":               0.28, // deepseek/ prefix, not the cheaper proxy
		"gemini-2.5-pro":              1.25, // unprefixed vertex before gemini/
		"together-only-1":             0.2,  // no known publisher: kept as LiteLLM names it
		"azure_ai/claude-opus-4-5":    9,
		"aaa-proxy/deepseek-chat":     0.19,
		"volcengine/kimi-k2-thinking": 0.6,
	} {
		if e, ok := got[id]; !ok || e.InputCostPerMillion != price {
			t.Fatalf("%s = (%+v, %v), want input price %v", id, e, ok, price)
		}
	}
	if e := got["deepseek-chat"]; e.ID != "deepseek-chat" || e.ContextWindow != 131072 {
		t.Fatalf("deepseek-chat = %+v", e)
	}
	for _, id := range []string{"kimi-k2-thinking", "kimi-k2"} {
		if e, ok := got[id]; ok {
			t.Fatalf("%s taken from a reseller: %+v", id, e)
		}
	}
}

func TestSyncFirstPartyWinsAcrossSources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models.dev":
			w.Write([]byte(resellerModelsDev))
		case "/litellm":
			w.Write([]byte(resellerLiteLLM))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cat := Open(Options{HTTPClient: srv.Client(), ModelsDevURL: srv.URL + "/models.dev", LiteLLMURL: srv.URL + "/litellm"})
	if err := cat.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if usd, ok := cat.EstimateCostUSD("opus", "claude-opus-4-5", 1_000_000, 1_000_000); !ok || usd != 30 {
		t.Fatalf("claude-opus-4-5 cost = (%v, %v), want 30 at $5/$25", usd, ok)
	}
	if e, ok := cat.Lookup("opencode/kimi-k2", ""); !ok || e.InputCostPerMillion != 0.4 {
		t.Fatalf("opencode/kimi-k2 = (%+v, %v)", e, ok)
	}
	if e, ok := cat.Lookup("kimi-k2", ""); ok {
		t.Fatalf("kimi-k2 matched a reseller's entry: %+v", e)
	}
}

func TestSyncPartialFailureKeepsFailedSourceData(t *testing.T) {
	var mdDown, llDown atomic.Bool
	llPrice := atomic.Value{}
	llPrice.Store("0.000001")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/models.dev" && !mdDown.Load():
			w.Write([]byte(`{"acme": {"models": {"md-only-1": {"id": "md-only-1", "canonical_model_id": "acme/md-only-1", "limit": {"context": 64000}, "cost": {"input": 2, "output": 4}}}}}`))
		case r.URL.Path == "/litellm" && !llDown.Load():
			w.Write([]byte(`{"ll-only-1": {"max_input_tokens": 32000, "input_cost_per_token": ` + llPrice.Load().(string) + `}}`))
		default:
			http.Error(w, "down", http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	cachePath := filepath.Join(t.TempDir(), "model-catalog.json")
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	opts := Options{CachePath: cachePath, HTTPClient: srv.Client(), ModelsDevURL: srv.URL + "/models.dev", LiteLLMURL: srv.URL + "/litellm", Now: func() time.Time { return now }}
	cat := Open(opts)
	if err := cat.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	fullAt := now

	// models.dev fails, LiteLLM answers with a new price.
	mdDown.Store(true)
	llPrice.Store("0.000003")
	now = now.Add(time.Hour)
	if err := cat.Sync(context.Background()); err == nil || !strings.Contains(err.Error(), "models.dev") {
		t.Fatalf("partial Sync err = %v, want a models.dev error", err)
	}
	if e, ok := cat.Lookup("md-only-1", ""); !ok || e.InputCostPerMillion != 2 {
		t.Fatalf("models.dev-only model after a models.dev failure = (%+v, %v)", e, ok)
	}
	if e, ok := cat.Lookup("ll-only-1", ""); !ok || e.InputCostPerMillion != 3 {
		t.Fatalf("LiteLLM model not refreshed: (%+v, %v)", e, ok)
	}
	st := cat.Status()
	if !st.SyncedAt.Equal(fullAt) || !st.LastAttemptAt.Equal(now) || !strings.Contains(st.LastError, "models.dev") {
		t.Fatalf("status after partial sync = %+v, want SyncedAt %v kept", st, fullAt)
	}
	reopened := Open(Options{CachePath: cachePath})
	if _, ok := reopened.Lookup("md-only-1", ""); !ok {
		t.Fatal("cache written by a partial sync lost the models.dev-only model")
	}
	if !reopened.Status().SyncedAt.Equal(fullAt) {
		t.Fatalf("cached SyncedAt = %v, want %v", reopened.Status().SyncedAt, fullAt)
	}

	// And the other way round, starting from the cache.
	mdDown.Store(false)
	llDown.Store(true)
	cat = Open(opts)
	now = now.Add(time.Hour)
	if err := cat.Sync(context.Background()); err == nil {
		t.Fatal("expected a litellm error")
	}
	if _, ok := cat.Lookup("ll-only-1", ""); !ok {
		t.Fatal("LiteLLM-only model dropped after a LiteLLM failure")
	}
	if !cat.Status().SyncedAt.Equal(fullAt) {
		t.Fatalf("SyncedAt = %v, want %v", cat.Status().SyncedAt, fullAt)
	}

	// A full sync advances SyncedAt again.
	llDown.Store(false)
	now = now.Add(time.Hour)
	if err := cat.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st := cat.Status(); !st.SyncedAt.Equal(now) || st.LastError != "" {
		t.Fatalf("status after full sync = %+v", st)
	}
}

func TestRunRetriesSoonAfterFailedSync(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/litellm" && hits.Add(1) == 1 {
			http.Error(w, "down", http.StatusBadGateway) // the first sync fails
			return
		}
		switch r.URL.Path {
		case "/models.dev":
			w.Write([]byte(`{}`))
		case "/litellm":
			w.Write([]byte(`{"ll-only-1": {"max_input_tokens": 32000}}`))
		}
	}))
	defer srv.Close()
	cat := Open(Options{HTTPClient: srv.Client(), ModelsDevURL: srv.URL + "/models.dev", LiteLLMURL: srv.URL + "/litellm",
		SyncInterval: time.Hour, RetryInterval: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { cat.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(10 * time.Second)
	for hits.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("no retry after a failed first sync (%d attempts)", hits.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	for {
		if _, ok := cat.Lookup("ll-only-1", ""); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retry did not load the catalog")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBuildSnapshotUsesFirstPartyData(t *testing.T) {
	at := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	data, missing, err := BuildSnapshot([]byte(resellerModelsDev), []byte(resellerLiteLLM), at)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := decodePersisted(data)
	if err != nil {
		t.Fatalf("snapshot does not decode: %v\n%s", err, data)
	}
	if snap.Source != sourceSnapshot || !snap.SyncedAt.Equal(at) {
		t.Fatalf("snapshot header = %q %v", snap.Source, snap.SyncedAt)
	}
	ids := make([]string, 0, len(snap.Models))
	for id := range snap.Models {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	// First-party text models of shipped families, plus extras LiteLLM's
	// first party has; no reseller keys, embeddings, image models, models
	// of families the app does not ship, or ids no first party lists.
	if want := []string{"claude-opus-4-5", "claude-sonnet-4-5", "deepseek-chat", "kimi-k3"}; !slices.Equal(ids, want) {
		t.Fatalf("snapshot ids = %v, want %v", ids, want)
	}
	if e := snap.Models["claude-opus-4-5"]; e.InputCostPerMillion != 5 || e.OutputCostPerMillion != 25 || e.ContextWindow != 200000 {
		t.Fatalf("claude-opus-4-5 = %+v", e)
	}
	if !slices.Contains(missing, "gpt-5-codex") || slices.Contains(missing, "deepseek-chat") {
		t.Fatalf("missing = %v", missing)
	}
}

// The embedded snapshot is generated, so it carries upstream's first-party
// prices (claude-opus-4-5 was hand-written at $15/$75) and only models of
// the families the app ships with.
func TestEmbeddedSnapshotIsGenerated(t *testing.T) {
	snap, err := decodePersisted(embeddedSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"claude-opus-4-5", "claude-opus-4-6"} {
		if e := snap.Models[id]; e.InputCostPerMillion != 5 || e.OutputCostPerMillion != 25 {
			t.Fatalf("%s pricing = (%v, %v), want (5, 25)", id, e.InputCostPerMillion, e.OutputCostPerMillion)
		}
	}
	for id, e := range snap.Models {
		if f := familyOf(id); f == nil || !f.snapshot || strings.Contains(id, "/") || e.ID != id || isEmptyEntry(e) {
			t.Fatalf("unexpected snapshot entry %q: %+v", id, e)
		}
	}
}

func sameEntry(a, b Entry) bool {
	return a.ID == b.ID && a.ContextWindow == b.ContextWindow && a.MaxOutputTokens == b.MaxOutputTokens &&
		slices.Equal(a.Capabilities, b.Capabilities) && a.InputCostPerMillion == b.InputCostPerMillion &&
		a.OutputCostPerMillion == b.OutputCostPerMillion
}

// A cache written before bare ids were first-party only may carry reseller
// prices; it is ignored rather than trusted until the next sync.
func TestOpenIgnoresCacheFromBeforeFirstPartyRule(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "model-catalog.json")
	old := `{"source":"synced","synced_at":"2026-11-25T00:00:00Z","models":{"claude-opus-4-5":{"id":"claude-opus-4-5","input_cost_per_million":15,"output_cost_per_million":75}}}`
	if err := os.WriteFile(cachePath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cat := Open(Options{CachePath: cachePath})
	if st := cat.Status(); st.Source != sourceSnapshot {
		t.Fatalf("source = %q, want the snapshot over an old cache", st.Source)
	}
	if e, _ := cat.Lookup("claude-opus-4-5", ""); e.InputCostPerMillion != 5 {
		t.Fatalf("claude-opus-4-5 = %+v, want the snapshot's $5", e)
	}
}
