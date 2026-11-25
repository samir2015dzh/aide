package modelcatalog

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestOpenColdStartUsesEmbeddedSnapshot(t *testing.T) {
	cat := Open(Options{})
	st := cat.Status()
	if st.Source != sourceSnapshot {
		t.Fatalf("source = %q, want %q", st.Source, sourceSnapshot)
	}
	if st.ModelCount < 10 {
		t.Fatalf("model count = %d, want >= 10", st.ModelCount)
	}

	// Exact match via resolved ID and provider-prefixed ID.
	e, ok := cat.Lookup("sonnet", "claude-sonnet-4-5")
	if !ok {
		t.Fatal("expected exact match for resolved claude-sonnet-4-5")
	}
	if e.ContextWindow != 200000 || e.MaxOutputTokens != 64000 {
		t.Fatalf("limits = (%d, %d), want (200000, 64000)", e.ContextWindow, e.MaxOutputTokens)
	}
	if !slices.Contains(e.Capabilities, "vision") || !slices.Contains(e.Capabilities, "tools") {
		t.Fatalf("capabilities = %v, want vision and tools", e.Capabilities)
	}
	if e.InputCostPerMillion != 3 || e.OutputCostPerMillion != 15 {
		t.Fatalf("pricing = (%v, %v), want (3, 15)", e.InputCostPerMillion, e.OutputCostPerMillion)
	}

	// Provider prefix stripping for exact lookup works.
	if _, ok := cat.Lookup("anthropic/claude-sonnet-4-5", ""); !ok {
		t.Fatal("expected provider-prefixed exact match to succeed")
	}

	// Strict exact match: unknown/custom or prefix-similar model must NOT match.
	for _, bad := range []string{"claude-sonnet-4-5-custom", "claude-sonnet", "my-private-model", "gpt-5-turbo-internal"} {
		if got, ok := cat.Lookup(bad, ""); ok {
			t.Fatalf("Lookup(%q) unexpectedly matched %+v", bad, got)
		}
	}
}

func TestSyncDualSourceMergeCacheAndFailureFallback(t *testing.T) {
	failMode := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failMode {
			http.Error(w, "upstream down", http.StatusBadGateway)
			return
		}
		switch r.URL.Path {
		case "/models.dev":
			w.Write([]byte(`{
				"acme": {
					"models": {
						"acme-pro-2": {
							"id": "acme-pro-2",
							"tool_call": true,
							"reasoning": true,
							"modalities": {"input": ["text", "image"]},
							"limit": {"context": 256000, "output": 32000},
							"cost": {"input": 2.5, "output": 10.0}
						}
					}
				}
			}`))
		case "/litellm":
			w.Write([]byte(`{
				"sample_spec": {"max_tokens": 100},
				"acme-pro-2": {
					"max_input_tokens": 256000,
					"max_output_tokens": 32000,
					"input_cost_per_token": 0.000003,
					"output_cost_per_token": 0.000012,
					"supports_response_schema": true
				},
				"litellm-only-1": {
					"max_input_tokens": 128000,
					"max_output_tokens": 16384,
					"input_cost_per_token": 0.0000005,
					"output_cost_per_token": 0.0000015,
					"supports_function_calling": true
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cachePath := filepath.Join(t.TempDir(), "model-catalog.json")
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	cat := Open(Options{
		CachePath:    cachePath,
		HTTPClient:   srv.Client(),
		ModelsDevURL: srv.URL + "/models.dev",
		LiteLLMURL:   srv.URL + "/litellm",
		Now:          func() time.Time { return now },
	})

	if err := cat.Sync(context.Background()); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	st := cat.Status()
	if st.Source != sourceSynced || st.LastError != "" || !st.SyncedAt.Equal(now) {
		t.Fatalf("unexpected status after sync: %+v", st)
	}

	// Merged entry gets capabilities from both models.dev and litellm, and
	// correctly normalized per-1M pricing (litellm per-token * 1e6).
	acme, ok := cat.Lookup("acme-pro-2", "")
	if !ok {
		t.Fatal("expected acme-pro-2 after sync")
	}
	if acme.ContextWindow != 256000 || acme.MaxOutputTokens != 32000 {
		t.Fatalf("acme limits = (%d, %d)", acme.ContextWindow, acme.MaxOutputTokens)
	}
	for _, capTag := range []string{"reasoning", "structured_output", "tools", "vision"} {
		if !slices.Contains(acme.Capabilities, capTag) {
			t.Fatalf("acme capabilities %v missing %q", acme.Capabilities, capTag)
		}
	}
	if math.Abs(acme.InputCostPerMillion-3.0) > 1e-9 || math.Abs(acme.OutputCostPerMillion-12.0) > 1e-9 {
		t.Fatalf("acme pricing = (%v, %v), want (3, 12)", acme.InputCostPerMillion, acme.OutputCostPerMillion)
	}

	// LiteLLM-only entry has accurate unit conversion (0.0000005/tok = $0.50/1M).
	ll, ok := cat.Lookup("litellm-only-1", "")
	if !ok {
		t.Fatal("expected litellm-only-1 after sync")
	}
	if math.Abs(ll.InputCostPerMillion-0.5) > 1e-9 || math.Abs(ll.OutputCostPerMillion-1.5) > 1e-9 {
		t.Fatalf("litellm pricing = (%v, %v), want (0.5, 1.5)", ll.InputCostPerMillion, ll.OutputCostPerMillion)
	}

	// EstimateCostUSD accuracy: 1,000,000 input ($0.5) + 2,000,000 output ($3.0) = $3.50.
	est, ok := cat.EstimateCostUSD("litellm-only-1", "", 1_000_000, 2_000_000)
	if !ok || math.Abs(est-3.5) > 1e-9 {
		t.Fatalf("EstimateCostUSD = (%v, %v), want (3.5, true)", est, ok)
	}
	if _, ok := cat.EstimateCostUSD("unknown-custom-model", "", 1000, 1000); ok {
		t.Fatal("expected unknown model cost estimate to return false")
	}

	// Upstream failure preserves existing catalog and records LastError.
	failMode = true
	now = now.Add(time.Hour)
	if err := cat.Sync(context.Background()); err == nil {
		t.Fatal("expected Sync to return error when both upstreams fail")
	}
	st2 := cat.Status()
	if st2.LastError == "" || !st2.LastAttemptAt.Equal(now) {
		t.Fatalf("expected LastError and LastAttemptAt on failure, got %+v", st2)
	}
	if _, ok := cat.Lookup("acme-pro-2", ""); !ok {
		t.Fatal("existing synced model should survive upstream failure")
	}

	// Reopening from cache path loads persisted catalog offline.
	reopened := Open(Options{CachePath: cachePath})
	if reopened.Status().Source != sourceCache {
		t.Fatalf("reopened source = %q, want %q", reopened.Status().Source, sourceCache)
	}
	if _, ok := reopened.Lookup("acme-pro-2", ""); !ok {
		t.Fatal("reopened catalog should contain cached acme-pro-2")
	}
}
