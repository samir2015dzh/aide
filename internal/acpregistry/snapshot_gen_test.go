//go:build acpregistry_snapshot

package acpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestRegenerateSnapshot rewrites snapshot.json from the official Registry
// index (DefaultRegistryURL), verbatim but indented. It needs the network:
//
//	go test -tags acpregistry_snapshot -run TestRegenerateSnapshot ./internal/acpregistry
func TestRegenerateSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DefaultRegistryURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexBytes))
	if err != nil {
		t.Fatal(err)
	}
	var idx Index
	if err := json.Unmarshal(raw, &idx); err != nil || len(idx.Agents) == 0 {
		t.Fatalf("not a registry index (%d agents): %v", len(idx.Agents), err)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		t.Fatal(err)
	}
	out.WriteByte('\n')
	if err := os.WriteFile("snapshot.json", out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("snapshot.json: %d agents, registry version %s", len(idx.Agents), idx.Version)
}
