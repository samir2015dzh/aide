package modelcatalog

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// snapshotExtraIDs are models the app's agents report that models.dev's
// first-party providers do not list but LiteLLM's first-party entries do.
// BuildSnapshot keeps each one only if upstream has it.
var snapshotExtraIDs = []string{
	"gpt-5-codex", "gpt-5.1-codex", "gpt-5.1-codex-mini", "gpt-5.1-codex-max", "gpt-5.2-codex", "o4-mini",
	"gemini-3-pro-preview",
	"deepseek-chat", "deepseek-reasoner",
	"qwen3-coder-plus",
}

// BuildSnapshot builds the built-in snapshot (snapshot.json) from raw
// models.dev and LiteLLM data with the same parsing and merge as Sync, minus
// the snapshot baseline. It keeps the bare ids of the families the app ships
// with: text chat models their first-party provider lists on models.dev,
// plus snapshotExtraIDs. It also returns the extra ids upstream lacks, which
// are left out rather than invented. `go run ./internal/modelcatalog/gen`
// regenerates the file.
func BuildSnapshot(modelsDevData, litellmData []byte, syncedAt time.Time) ([]byte, []string, error) {
	modelsDev, err := ParseModelsDev(modelsDevData)
	if err != nil {
		return nil, nil, err
	}
	litellm, err := ParseLiteLLM(litellmData)
	if err != nil {
		return nil, nil, err
	}
	merged := mergeSources(nil, modelsDev, litellm)

	providers, err := decodeModelsDev(modelsDevData)
	if err != nil {
		return nil, nil, err
	}
	want := map[string]bool{}
	for pk, p := range providers {
		pk = strings.ToLower(strings.TrimSpace(pk))
		for mk, m := range p.Models {
			id := strings.TrimSpace(m.ID)
			if id == "" {
				id = strings.TrimSpace(mk)
			}
			bare := bareID(strings.ToLower(id))
			f := familyOf(bare)
			if f == nil || !f.snapshot || !slices.Contains(f.modelsDev, pk) {
				continue
			}
			if m.Limit.Context == 0 || strings.Contains(bare, "embedding") ||
				!slices.Contains(m.Modalities.Input, "text") || !slices.Equal(m.Modalities.Output, []string{"text"}) {
				continue
			}
			want[bare] = true
		}
	}
	var missing []string
	for _, id := range snapshotExtraIDs {
		if _, ok := merged[id]; ok {
			want[id] = true
		} else {
			missing = append(missing, id)
		}
	}

	ids := make([]string, 0, len(want))
	for id := range want {
		if _, ok := merged[id]; ok {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	// One model per line keeps the file short and its diffs readable.
	var buf bytes.Buffer
	stamp, _ := json.Marshal(syncedAt.UTC())
	buf.WriteString("{\n  \"source\": \"snapshot\",\n  \"synced_at\": " + string(stamp) + ",\n  \"models\": {\n")
	for i, id := range ids {
		k, _ := json.Marshal(id)
		v, err := json.Marshal(merged[id])
		if err != nil {
			return nil, nil, err
		}
		buf.WriteString("    " + string(k) + ": " + string(v))
		if i < len(ids)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("  }\n}\n")
	return buf.Bytes(), missing, nil
}
