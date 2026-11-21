package claude

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A fixed 32-byte key stands in for the OSCrypt key the platform key store
// would yield, so the whole desktop-cache path (seal, open, pick, rewrite)
// runs on any OS in tests.
var testKey = bytes.Repeat([]byte{0x24}, oscryptKeyLen)

func testKeyFn([]byte) oscryptKeyFunc {
	return func(string) ([]byte, error) { return testKey, nil }
}

// sealCacheV2 builds a config.json token-cache value: base64(OSCrypt(json)).
func sealCacheV2(t *testing.T, key []byte, cache map[string]any) string {
	t.Helper()
	plain, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := oscryptSeal(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sealed)
}

// desktopLayout writes a signed-in Claude Desktop layout discoverable by
// desktopUserData, and returns (home, appData) to pass to a UsageReader.
// The token cache holds two entries; only one grants user:inference.
func desktopLayout(t *testing.T, access, refresh string, expiresAt time.Time) (home, appData string) {
	t.Helper()
	home = t.TempDir()
	appData = t.TempDir()
	dir := filepath.Join(appData, "Claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cache := map[string]any{
		// Scopes carried in the record.
		"acct:home-1:client-xyz:profile": map[string]any{
			"token":     "other-token-not-inference",
			"expiresAt": expiresAt.UnixMilli(),
			"scopes":    []string{"user:profile"},
			"clientId":  "client-xyz",
		},
		// Scopes carried only in the MSAL-style key.
		"acct:home-1:client-xyz:https://api.anthropic.com:user:inference user:profile": map[string]any{
			"token":        access,
			"refreshToken": refresh,
			"expiresAt":    expiresAt.UnixMilli(),
		},
	}
	cfg := map[string]any{
		"oauth:tokenCacheV2":    sealCacheV2(t, testKey, cache),
		"lastKnownAccountUuid":  "home-1",
		"windowSizeWasSignedIn": true,
		"someOtherSetting":      "keep-me",
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return home, appData
}

// credentialsLayout writes a standard Claude Code credentials file.
func credentialsLayout(t *testing.T, access, refresh string, expiresAt time.Time) (home string) {
	t.Helper()
	home = t.TempDir()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cf := map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":  access,
			"refreshToken": refresh,
			"expiresAt":    expiresAt.UnixMilli(),
			"scopes":       []string{"user:inference", "user:profile"},
			"clientId":     "client-cli",
		},
		"keepThis": 7,
	}
	b, _ := json.MarshalIndent(cf, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func usageServer(t *testing.T, want string, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+want {
			t.Errorf("usage auth header = %q, want Bearer %q", got, want)
		}
		if got := r.Header.Get("anthropic-beta"); got != oauthBetaHeader {
			t.Errorf("beta header = %q, want %q", got, oauthBetaHeader)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

const usageBody = `{
  "five_hour":  {"utilization": 20.0, "resets_at": "2026-11-20T20:39:59.735515+00:00"},
  "seven_day":  {"utilization": 59.0, "resets_at": "2026-11-26T06:59:59.735549+00:00"},
  "seven_day_opus": null,
  "seven_day_sonnet": {"utilization": 12.5, "resets_at": "2026-11-26T06:59:59Z"}
}`

func TestUsageReader_DesktopLayout(t *testing.T) {
	access := "sk-ant-oat-DESKTOP-abcdefghijklmnop"
	exp := time.Now().Add(48 * time.Hour)
	home, appData := desktopLayout(t, access, "refresh-desktop", exp)
	srv := usageServer(t, access, usageBody)
	defer srv.Close()

	r := &UsageReader{Home: home, AppData: appData, LocalAppData: t.TempDir(), UsageURL: srv.URL, keyFn: testKeyFn(testKey)}
	u := r.Read(context.Background())
	if u.Unavailable != "" {
		t.Fatalf("unavailable = %q, want empty", u.Unavailable)
	}
	if len(u.Windows) != 3 {
		t.Fatalf("windows = %d, want 3 (nulls skipped): %+v", len(u.Windows), u.Windows)
	}
	if u.Windows[0].Key != "five_hour" || u.Windows[1].Key != "seven_day" || u.Windows[2].Key != "seven_day_sonnet" {
		t.Fatalf("window order = %v", []string{u.Windows[0].Key, u.Windows[1].Key, u.Windows[2].Key})
	}
	if u.Windows[0].Utilization != 20.0 || u.Windows[1].Utilization != 59.0 {
		t.Fatalf("utilizations = %v", u.Windows)
	}
}

func TestUsageReader_CredentialsLayout(t *testing.T) {
	access := "sk-ant-oat-CLI-zyxwvutsrqponml"
	home := credentialsLayout(t, access, "refresh-cli", time.Now().Add(24*time.Hour))
	srv := usageServer(t, access, usageBody)
	defer srv.Close()

	// No desktop layout and an empty appData: the credentials file is the
	// only source.
	r := &UsageReader{Home: home, AppData: t.TempDir(), LocalAppData: t.TempDir(), UsageURL: srv.URL, keyFn: testKeyFn(testKey)}
	u := r.Read(context.Background())
	if u.Unavailable != "" || len(u.Windows) != 3 {
		t.Fatalf("read = %+v", u)
	}
}

func TestUsageReader_SignedOut(t *testing.T) {
	r := &UsageReader{Home: t.TempDir(), AppData: t.TempDir(), LocalAppData: t.TempDir(), keyFn: testKeyFn(testKey)}
	if u := r.Read(context.Background()); u.Unavailable != "signed_out" {
		t.Fatalf("unavailable = %q, want signed_out", u.Unavailable)
	}
}

func TestUsageReader_Unsupported(t *testing.T) {
	// A desktop cache is present but the key cannot be obtained (as off
	// Windows): the reading is "unsupported", not "signed_out".
	home, appData := desktopLayout(t, "tok", "ref", time.Now().Add(time.Hour))
	r := &UsageReader{Home: home, AppData: appData, LocalAppData: t.TempDir(),
		keyFn: func(string) ([]byte, error) { return nil, errUnsupportedOS }}
	if u := r.Read(context.Background()); u.Unavailable != "unsupported" {
		t.Fatalf("unavailable = %q, want unsupported", u.Unavailable)
	}
}

func TestUsageReader_APIError(t *testing.T) {
	home := credentialsLayout(t, "tok", "ref", time.Now().Add(time.Hour))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	r := &UsageReader{Home: home, AppData: t.TempDir(), LocalAppData: t.TempDir(), UsageURL: srv.URL}
	if u := r.Read(context.Background()); u.Unavailable != "api_error" {
		t.Fatalf("unavailable = %q, want api_error", u.Unavailable)
	}
}

func TestUsageReader_RenewWritesBackDesktop(t *testing.T) {
	oldAccess := "sk-ant-oat-OLD-aaaaaaaaaaaaaaaa"
	newAccess := "sk-ant-oat-NEW-bbbbbbbbbbbbbbbb"
	// Expired token, so a renew is due.
	home, appData := desktopLayout(t, oldAccess, "refresh-1", time.Now().Add(-time.Minute))

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["grant_type"] != "refresh_token" || body["refresh_token"] != "refresh-1" {
			t.Errorf("token request body = %+v", body)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  newAccess,
			"refresh_token": "refresh-2",
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()
	usageSrv := usageServer(t, newAccess, usageBody) // the NEW token must be used
	defer usageSrv.Close()

	cfgPath := filepath.Join(appData, "Claude", "config.json")
	before, _ := os.ReadFile(cfgPath)

	r := &UsageReader{Home: home, AppData: appData, LocalAppData: t.TempDir(), AutoRenew: true,
		UsageURL: usageSrv.URL, TokenURL: tokenSrv.URL, keyFn: testKeyFn(testKey)}
	u := r.Read(context.Background())
	if u.Renew != "renewed" {
		t.Fatalf("renew = %q, want renewed", u.Renew)
	}
	if u.Unavailable != "" || len(u.Windows) != 3 {
		t.Fatalf("read = %+v", u)
	}

	// The cache now holds the new token, other config keys are kept, and
	// the untouched entry is still there.
	after, _ := os.ReadFile(cfgPath)
	if bytes.Equal(before, after) {
		t.Fatal("config.json unchanged after renew")
	}
	var cfg map[string]json.RawMessage
	json.Unmarshal(after, &cfg)
	var keep string
	json.Unmarshal(cfg["someOtherSetting"], &keep)
	if keep != "keep-me" {
		t.Fatalf("someOtherSetting = %q, lost on rewrite", keep)
	}
	// Re-read the token: it must now be the new one.
	tok, reason := desktopToken(desktopUserData(home, appData, ""), testKeyFn(testKey))
	if reason != "" || tok.access != newAccess || tok.refresh != "refresh-2" {
		t.Fatalf("after renew token = %q/%q reason=%q", tok.access, tok.refresh, reason)
	}
	// The other (profile) entry survived.
	entries := mustCacheEntries(t, home, appData)
	if len(entries) != 2 {
		t.Fatalf("entries after renew = %d, want 2", len(entries))
	}
}

func TestUsageReader_RenewFailRestoresCache(t *testing.T) {
	oldAccess := "sk-ant-oat-OLD-cccccccccccccccc"
	home, appData := desktopLayout(t, oldAccess, "refresh-1", time.Now().Add(-time.Minute))
	cfgPath := filepath.Join(appData, "Claude", "config.json")
	before, _ := os.ReadFile(cfgPath)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest) // refresh refused
	}))
	defer tokenSrv.Close()

	r := &UsageReader{Home: home, AppData: appData, LocalAppData: t.TempDir(), AutoRenew: true,
		TokenURL: tokenSrv.URL, keyFn: testKeyFn(testKey)}
	u := r.Read(context.Background())
	if u.Renew != "renew_failed" {
		t.Fatalf("renew = %q, want renew_failed", u.Renew)
	}
	// Token was already expired and renew failed: no reading.
	if u.Unavailable != "signed_out" {
		t.Fatalf("unavailable = %q, want signed_out", u.Unavailable)
	}
	// The cache is untouched and still opens with the old token.
	after, _ := os.ReadFile(cfgPath)
	if !bytes.Equal(before, after) {
		t.Fatal("config.json changed although renew failed before write")
	}
	tok, reason := desktopToken(desktopUserData(home, appData, ""), testKeyFn(testKey))
	if reason != "" || tok.access != oldAccess {
		t.Fatalf("old token not readable after failed renew: %q/%q", tok.access, reason)
	}
}

func mustCacheEntries(t *testing.T, home, appData string) []cacheEntry {
	t.Helper()
	dir := filepath.Join(appData, "Claude")
	cfg, err := readConfig(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := base64.StdEncoding.DecodeString(cfg.str("oauth:tokenCacheV2"))
	plain, err := oscryptOpen(testKey, blob)
	if err != nil {
		t.Fatal(err)
	}
	e, err := parseTokenCache(plain)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// TestUsage_NoTokenLeak is the ticket's §验收③: a token's fingerprint must
// appear nowhere in the reading or the logs, only its windows.
func TestUsage_NoTokenLeak(t *testing.T) {
	secret := "sk-ant-oat-SECRET-deadbeefdeadbeef0123"
	home, appData := desktopLayout(t, secret, "refresh-SECRET-cafebabecafebabe", time.Now().Add(-time.Minute))
	// Renew fails, exercising the renew log path too.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer tokenSrv.Close()

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := &UsageReader{Home: home, AppData: appData, LocalAppData: t.TempDir(), AutoRenew: true,
		TokenURL: tokenSrv.URL, keyFn: testKeyFn(testKey), Log: logger}
	u := r.Read(context.Background())

	fingerprints := []string{secret, "refresh-SECRET-cafebabecafebabe", "sk-ant-oat-SECRET", "cafebabe"}
	// The reading, marshalled.
	ub, _ := json.Marshal(u)
	for _, hay := range []struct{ name, s string }{{"usage", string(ub)}, {"log", logBuf.String()}} {
		for _, fp := range fingerprints {
			if strings.Contains(hay.s, fp) {
				t.Errorf("%s output carries token fingerprint %q:\n%s", hay.name, fp, hay.s)
			}
		}
	}
}

func TestWindowsFromResponse_OrderAndNulls(t *testing.T) {
	raw := map[string]*windowJSON{
		"seven_day_zulu":  {Utilization: 1},
		"seven_day":       {Utilization: 2},
		"five_hour":       {Utilization: 3},
		"seven_day_alpha": {Utilization: 4},
		"seven_day_nada":  nil,
	}
	got := windowsFromResponse(raw)
	want := []string{"five_hour", "seven_day", "seven_day_alpha", "seven_day_zulu"}
	if len(got) != len(want) {
		t.Fatalf("windows = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Key != want[i] {
			t.Fatalf("window[%d] = %q, want %q", i, got[i].Key, want[i])
		}
	}
}

func TestOSCryptRoundTrip(t *testing.T) {
	plain := []byte(`{"hello":"world","n":42}`)
	blob, err := oscryptSeal(testKey, plain)
	if err != nil {
		t.Fatal(err)
	}
	if string(blob[:3]) != "v10" {
		t.Fatalf("blob prefix = %q", blob[:3])
	}
	got, err := oscryptOpen(testKey, blob)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round-trip = %q, want %q", got, plain)
	}
	// A wrong key fails to open.
	if _, err := oscryptOpen(bytes.Repeat([]byte{1}, oscryptKeyLen), blob); err == nil {
		t.Fatal("open with wrong key succeeded")
	}
}

func TestRedactErr(t *testing.T) {
	// A transport error wrapping a URL is stripped to op + host + cause;
	// the path and any query (a token could hide there) never reach the log.
	ue := &url.Error{Op: "Get", URL: "https://api.anthropic.com/api/oauth/usage?access_token=sk-ant-SECRET", Err: errors.New("connection reset")}
	got := redactErr(ue)
	for _, bad := range []string{"SECRET", "access_token", "/api/oauth/usage"} {
		if strings.Contains(got, bad) {
			t.Fatalf("redactErr leaked %q: %s", bad, got)
		}
	}
	if !strings.Contains(got, "api.anthropic.com") || !strings.Contains(got, "connection reset") {
		t.Fatalf("redactErr = %q, want host and cause", got)
	}
	// A plain error passes through unchanged.
	if redactErr(errors.New("usage endpoint: HTTP 403")) != "usage endpoint: HTTP 403" {
		t.Fatal("redactErr changed a plain error")
	}
}

func TestScopesFromKey(t *testing.T) {
	id := "acct:home-1:client-xyz:https://api.anthropic.com:user:inference user:profile"
	client, scopes := scopesFromKey(id)
	if client != "client-xyz" {
		t.Fatalf("client = %q", client)
	}
	if len(scopes) != 2 || scopes[0] != "user:inference" {
		t.Fatalf("scopes = %v", scopes)
	}
}

func TestPickInferenceEntry(t *testing.T) {
	entries := []cacheEntry{
		{id: "a", scopes: []string{"user:profile"}},
		{id: "b", scopes: []string{"user:inference", "user:profile"}},
	}
	if i := pickInferenceEntry(entries); i != 1 {
		t.Fatalf("pick = %d, want 1", i)
	}
	// None scoped: the first is used.
	if i := pickInferenceEntry([]cacheEntry{{id: "x"}, {id: "y"}}); i != 0 {
		t.Fatalf("pick (no scopes) = %d, want 0", i)
	}
	if i := pickInferenceEntry(nil); i != -1 {
		t.Fatalf("pick (empty) = %d, want -1", i)
	}
}
