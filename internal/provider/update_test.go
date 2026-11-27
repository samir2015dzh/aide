package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Release versions compare as paseo's release check does (工单 #178;
// provider-update-checks.test.ts, Apache License 2.0): never "current" when
// it cannot tell.
func TestCompareRelease(t *testing.T) {
	for _, c := range []struct {
		installed, latest string
		want              UpdateState
		message           string
	}{
		{"codex-cli 0.165.0", "0.168.0", UpdateAvailable, ""},
		{"2.1.323 (Claude Code)", "2.1.323", UpdateCurrent, ""},
		{"2.1.323 (Claude Code)", "2.1.311", UpdateCurrent, ""},
		{"2.1.323", "<html>", UpdateUnknown, `unrecognized release "<html>"`},
		{"0.200.0-alpha.1", "0.200.0", UpdateAvailable, ""},
		{"0.200.0", "0.200.0-alpha.1", UpdateCurrent, ""},
		{"0.200.0-alpha.1", "0.200.0-alpha.2", UpdateUnknown, "cannot compare 0.200.0-alpha.1 with 0.200.0-alpha.2"},
		{"", "2.1.330", UpdateUnknown, "the installed version is not readable"},
	} {
		if got := CompareRelease(c.installed, c.latest); got.State != c.want || got.Message != c.message {
			t.Errorf("%q against %q: %+v", c.installed, c.latest, got)
		}
	}
}

// npm's latest tag is the version a channel offers; an answer without one
// is an error, never a version.
func TestNPMLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/@anthropic-ai%2Fclaude-code/latest":
			w.Write([]byte(`{"name":"@anthropic-ai/claude-code","version":"2.1.330"}`))
		case "/broken/latest":
			w.Write([]byte(`<html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer func(u string) { npmRegistry = u }(npmRegistry)
	npmRegistry = srv.URL
	if v, err := NPMLatest(context.Background(), "@anthropic-ai/claude-code"); err != nil || v != "2.1.330" {
		t.Fatalf("latest: %q %v", v, err)
	}
	if _, err := NPMLatest(context.Background(), "broken"); err == nil {
		t.Fatal("a version from an unreadable answer")
	}
	if _, err := NPMLatest(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing: %v", err)
	}
}
