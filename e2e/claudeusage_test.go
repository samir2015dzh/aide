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

	var r result
	eventually(t, time.Minute, "aide usage says why it cannot read", func() bool {
		r = w.cli(30*time.Second, "usage")
		return strings.Contains(r.stdout, "read-fail: api_error (usage endpoint: HTTP 401: authentication_error: OAuth token has expired.)")
	})
	j := w.cli(30*time.Second, "usage", "-o", "json")
	if !strings.Contains(j.stdout, `"detail": "usage endpoint: HTTP 401: authentication_error: OAuth token has expired."`) {
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
