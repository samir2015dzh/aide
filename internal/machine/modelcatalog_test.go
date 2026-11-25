package machine

import (
	"context"
	"math"
	"testing"

	"github.com/samir2015dzh/aide/internal/modelcatalog"
	"github.com/samir2015dzh/aide/internal/provider"
)

func TestMachineStatusEnrichesKnownModelsOnly(t *testing.T) {
	cat := modelcatalog.Open(modelcatalog.Options{})
	m, err := Open(Config{
		DataDir:      t.TempDir(),
		ModelCatalog: cat,
		Providers:    map[string]provider.Provider{},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer m.Close()

	m.mu.Lock()
	m.infos["claude"] = provider.Info{
		Available: true,
		LoggedIn:  true,
		Models: []provider.Model{
			{ID: "sonnet", Name: "Anthropic · claude-sonnet-4-5", Resolved: "claude-sonnet-4-5"},
			{ID: "private-custom-9", Name: "Custom · private-custom-9"},
		},
	}
	m.mu.Unlock()

	st := m.Status()
	if st.GetModelCatalog() == nil || st.GetModelCatalog().GetSource() != "snapshot" || st.GetModelCatalog().GetModelCount() < 10 {
		t.Fatalf("unexpected model_catalog status: %+v", st.GetModelCatalog())
	}

	var claudeModels = st.GetAgents()[0].GetModels()
	if len(claudeModels) != 2 {
		t.Fatalf("got %d claude models, want 2", len(claudeModels))
	}
	known := claudeModels[0]
	if known.GetContextWindow() != 200000 || known.GetMaxOutputTokens() != 64000 {
		t.Fatalf("known model limits = (%d, %d), want (200000, 64000)", known.GetContextWindow(), known.GetMaxOutputTokens())
	}
	if known.GetInputCostPerMillion() != 3 || known.GetOutputCostPerMillion() != 15 {
		t.Fatalf("known model pricing = (%v, %v), want (3, 15)", known.GetInputCostPerMillion(), known.GetOutputCostPerMillion())
	}
	if len(known.GetCapabilities()) == 0 {
		t.Fatal("expected capabilities on known model")
	}

	unknown := claudeModels[1]
	if unknown.GetName() != "Custom · private-custom-9" {
		t.Fatalf("unknown model name changed to %q", unknown.GetName())
	}
	if unknown.GetContextWindow() != 0 || unknown.GetMaxOutputTokens() != 0 || len(unknown.GetCapabilities()) != 0 ||
		unknown.GetInputCostPerMillion() != 0 || unknown.GetOutputCostPerMillion() != 0 {
		t.Fatalf("unknown model should have zero/empty metadata, got %+v", unknown)
	}

	// EstimateTurnCost resolves alias "sonnet" -> "claude-sonnet-4-5" ($3/1M in, $15/1M out).
	est, ok := m.estimateTurnCost("claude", "sonnet", 1_000_000, 100_000)
	if !ok || math.Abs(est-4.5) > 1e-9 {
		t.Fatalf("estimateTurnCost(sonnet) = (%v, %v), want (4.5, true)", est, ok)
	}
	if _, ok := m.estimateTurnCost("claude", "private-custom-9", 1_000_000, 100_000); ok {
		t.Fatal("expected no cost estimate for unknown custom model")
	}

	_ = context.Background()
}
