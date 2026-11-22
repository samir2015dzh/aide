package claude

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Work order #35: an unreadable usage says what lay behind it — the HTTP
// status and a summary of the answer — in the reading and in the log, never
// just "api_error", and never the token.
func TestUsageReader_DetailSaysWhy(t *testing.T) {
	access := "sk-ant-oat-DETAIL-0123456789abcdef"
	cases := []struct {
		name, body, ctype, unavailable string
		status                          int
		want                            []string
	}{
		{"anthropic error JSON", `{"type":"error","error":{"type":"authentication_error","message":"OAuth token has expired."}}`,
			"application/json", "api_error", http.StatusUnauthorized,
			[]string{"HTTP 401", "authentication_error: OAuth token has expired."}},
		{"HTML from a proxy", "<html>\n<body>Bad   gateway</body></html>", "text/html", "api_error", http.StatusBadGateway,
			[]string{"HTTP 502", "<html> <body>Bad gateway</body></html>"}},
		{"empty body", "", "", "api_error", http.StatusTooManyRequests, []string{"HTTP 429", "(empty body)"}},
		{"200 but not an object", "<!doctype html>login", "text/html", "parse_error", http.StatusOK,
			[]string{"HTTP 200 (text/html)", "not a JSON object", "<!doctype html>login"}},
		{"echoed token is masked", `{"type":"error","error":{"type":"x","message":"bad token ` + access + `"}}`,
			"application/json", "api_error", http.StatusForbidden, []string{"HTTP 403", "[redacted]"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := credentialsLayout(t, access, "refresh-x", time.Now().Add(time.Hour))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.ctype != "" {
					w.Header().Set("Content-Type", c.ctype)
				}
				w.WriteHeader(c.status)
				w.Write([]byte(c.body))
			}))
			defer srv.Close()
			var logBuf bytes.Buffer
			r := &UsageReader{Home: home, AppData: t.TempDir(), LocalAppData: t.TempDir(), UsageURL: srv.URL,
				Log: slog.New(slog.NewTextHandler(&logBuf, nil))}
			u := r.Read(context.Background())
			if u.Unavailable != c.unavailable {
				t.Fatalf("unavailable = %q, want %q", u.Unavailable, c.unavailable)
			}
			for _, w := range c.want {
				if !strings.Contains(u.Detail, w) {
					t.Errorf("detail %q lacks %q", u.Detail, w)
				}
				if !strings.Contains(logBuf.String(), strings.Fields(w)[0]) {
					t.Errorf("log %q lacks %q", logBuf.String(), w)
				}
			}
			if strings.Contains(u.Detail, access) || strings.Contains(logBuf.String(), access) {
				t.Fatalf("the token leaked: detail %q log %q", u.Detail, logBuf.String())
			}
		})
	}
}

// A transport failure names the host and the cause, not the path.
func TestUsageReader_DetailTransport(t *testing.T) {
	home := credentialsLayout(t, "sk-ant-oat-T", "r", time.Now().Add(time.Hour))
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL + "/api/oauth/usage"
	srv.Close() // nothing listens now
	r := &UsageReader{Home: home, AppData: t.TempDir(), LocalAppData: t.TempDir(), UsageURL: url}
	u := r.Read(context.Background())
	if u.Unavailable != "api_error" || u.Detail == "" || strings.Contains(u.Detail, "/api/oauth") {
		t.Fatalf("read = %+v", u)
	}
}

// A long body is cut, and a long unbroken run (what a token looks like) is
// masked even without a known prefix.
func TestClip(t *testing.T) {
	long := strings.Repeat("word ", 100)
	if got := clip([]byte(long)); len([]rune(got)) > summaryMax+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("clip long = %q", got)
	}
	secret := strings.Repeat("A1b2", 12)
	if got := clip([]byte("x " + secret + " y")); strings.Contains(got, secret) {
		t.Fatalf("clip kept %q", got)
	}
}
