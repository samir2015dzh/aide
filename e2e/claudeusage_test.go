package e2e

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// Work order #35: when the Claude plan usage cannot be read, `aide usage`
// says what lay behind it — the endpoint's HTTP status and its error — and
// not just "api_error"; the account's token appears nowhere, not in the
// output and not in aided's log.
func TestClaudeUsageSaysWhy(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in Claude Code is a shell script")
	}
	const token = "sk-ant-oat01-E2EUSAGETOKEN0123456789abcdefghij"
	home := t.TempDir()
	creds := `{"claudeAiOauth":{"accessToken":"` + token + `","refreshToken":"sk-ant-ort01-E2EREFRESH","expiresAt":` +
		strconv.FormatInt(time.Now().Add(24*time.Hour).UnixMilli(), 10) + `,"scopes":["user:inference"]}}`
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"OAuth token has expired."}}`))
	}))
	defer endpoint.Close()
	claude := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo \"2.1.312 (Claude Code)\"; exit 0;;\nauth) echo '{\"loggedIn\": true}'; exit 0;;\nesac\nexit 1\n"
	if err := os.WriteFile(claude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	w := newOwnerMac(t, []string{"AIDE_CLAUDE_BIN=" + claude, "HOME=" + home, "AIDE_CLAUDE_USAGE_URL=" + endpoint.URL}, &localv1.GrantSpec{})

	// What each source said, in order (工单 #104): Claude Code's cache
	// first, the endpoint last, whose failure is the reason.
	var r result
	eventually(t, time.Minute, "aide usage says why it cannot read", func() bool {
		r = w.cli(30*time.Second, "usage")
		return strings.Contains(r.stdout, "read-fail: api_error (Claude Code cache: no config file of Claude Code's here; ") &&
			strings.Contains(r.stdout, "; usage endpoint: HTTP 401: authentication_error: OAuth token has expired.)")
	})
	j := w.cli(30*time.Second, "usage", "-o", "json")
	if !strings.Contains(j.stdout, `usage endpoint: HTTP 401: authentication_error: OAuth token has expired."`) {
		t.Fatalf("json: %s", j.stdout+j.stderr)
	}
	log := w.owner.output()
	if !strings.Contains(log, "HTTP 401") {
		t.Fatalf("aided's log does not say why:\n%s", log)
	}
	for _, out := range []string{r.stdout, r.stderr, j.stdout, j.stderr, log} {
		if strings.Contains(out, token) || strings.Contains(out, "E2EREFRESH") {
			t.Fatalf("the token leaked:\n%s", out)
		}
	}
}

// Work order #104: the plan usage Claude Code here last read and keeps in
// its config is the reading when it is fresh — as `aide usage` shows it,
// with where it came from — though Claude Code's /usage, asked without a
// terminal, answers with only that call's own totals, and the endpoint
// would fail.
func TestClaudeUsageFromClaudeCodeCache(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in Claude Code is a shell script")
	}
	home := t.TempDir()
	cfg := `{"oauthAccount":{"accountUuid":"00000000-0000-4000-8000-0000000000aa"},"cachedUsageUtilization":{"fetchedAtMs":` +
		strconv.FormatInt(time.Now().Add(-time.Minute).UnixMilli(), 10) + `,"accountUuid":"00000000-0000-4000-8000-0000000000aa","utilization":{` +
		`"five_hour":{"utilization":31.0,"resets_at":"` + time.Now().Add(2*time.Hour).UTC().Format(time.RFC3339) + `"},` +
		`"seven_day":{"utilization":64.0,"resets_at":"` + time.Now().Add(72*time.Hour).UTC().Format(time.RFC3339) + `"},"seven_day_opus":null}}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer endpoint.Close()
	claude := filepath.Join(t.TempDir(), "claude")
	totals := `{"type":"result","subtype":"success","is_error":false,"result":"Estimated cost:        $0.0000\\nTotal duration (API):  0s","local_command":"usage"}`
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo \"2.1.330 (Claude Code)\"; exit 0;;\nauth) echo '{\"loggedIn\": true}'; exit 0;;\n-p) echo '" + totals + "'; exit 0;;\nesac\nexit 1\n"
	if err := os.WriteFile(claude, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	w := newOwnerMac(t, []string{"AIDE_CLAUDE_BIN=" + claude, "HOME=" + home, "CLAUDE_CONFIG_DIR=", "AIDE_CLAUDE_USAGE_URL=" + endpoint.URL}, &localv1.GrantSpec{})

	var j result
	eventually(t, time.Minute, "aide usage shows Claude Code's cached reading", func() bool {
		j = w.cli(30*time.Second, "usage", "-o", "json")
		return strings.Contains(j.stdout, `"source": "claude_code_cache"`)
	})
	for _, want := range []string{`"key": "five_hour"`, `"utilization": 31`, `"key": "seven_day"`, `"utilization": 64`} {
		if !strings.Contains(j.stdout, want) {
			t.Fatalf("json lacks %s: %s", want, j.stdout)
		}
	}
	if strings.Contains(j.stdout, `"stale": true`) || strings.Contains(j.stdout, "seven_day_opus") {
		t.Fatalf("json: %s", j.stdout)
	}
	r := w.cli(30*time.Second, "usage")
	if !strings.Contains(r.stdout, "five_hour") || !strings.Contains(r.stdout, "31%") {
		t.Fatalf("aide usage: %s", r.stdout+r.stderr)
	}
}
