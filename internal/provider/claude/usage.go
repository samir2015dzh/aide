package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"time"
)

// Claude plan usage (docs/claude-usage.md). aided reads the account's OAuth
// token on the machine (tokencache.go), asks Anthropic's usage endpoint how
// full each rate-limit window is, and — when auto-renew is on — refreshes
// the token in place before it expires. The scheduler paces work by the
// result; the token itself never leaves the machine.

// Default endpoints (undocumented and liable to change; overridable for
// tests and in case they move).
const (
	defaultUsageURL = "https://api.anthropic.com/api/oauth/usage"
	defaultTokenURL = "https://console.anthropic.com/v1/oauth/token"
	oauthBetaHeader = "oauth-2025-04-20"
)

// renewLeeway refreshes a token this long before it would expire.
const renewLeeway = 10 * time.Minute

// Window is one rate-limit window's reading.
type Window struct {
	Key         string    `json:"key"`
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

// Usage is a reading of the account's plan usage.
type Usage struct {
	Windows   []Window
	FetchedAt time.Time
	// Unavailable is empty on success, else why there is no reading:
	// "signed_out", "no_token", "api_error", "parse_error", "unsupported".
	// parse_error: the endpoint answered, but not with a JSON object.
	Unavailable string
	// Renew is the last auto-renew outcome: "", "renewed", "renew_failed".
	Renew string
}

// UsageReader reads plan usage. The zero value works (real endpoints, real
// token sources, no auto-renew); set the fields to override a seam.
type UsageReader struct {
	// AutoRenew refreshes an expiring token and writes it back.
	AutoRenew bool
	// HTTP is the client for both endpoints (nil: a 15s-timeout default).
	HTTP *http.Client
	// UsageURL, TokenURL override the endpoints ("": the defaults).
	UsageURL, TokenURL string
	// Now overrides the clock (nil: time.Now).
	Now func() time.Time
	// Home, AppData, LocalAppData override discovery ("": from the OS).
	Home, AppData, LocalAppData string
	// keyFn overrides the OSCrypt key lookup (nil: the real one). Tests set
	// it to supply a known key.
	keyFn oscryptKeyFunc
	// Log is where renew outcomes go (never a token); nil: slog default.
	Log *slog.Logger
}

func (r *UsageReader) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *UsageReader) httpClient() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (r *UsageReader) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

func (r *UsageReader) home() string {
	if r.Home != "" {
		return r.Home
	}
	h, _ := os.UserHomeDir()
	return h
}

func (r *UsageReader) appData() string {
	if r.AppData != "" {
		return r.AppData
	}
	return os.Getenv("APPDATA")
}

func (r *UsageReader) localAppData() string {
	if r.LocalAppData != "" {
		return r.LocalAppData
	}
	return os.Getenv("LOCALAPPDATA")
}

// Read performs one fetch. It never blocks on more than the HTTP client's
// timeout and never returns a token in any field.
func (r *UsageReader) Read(ctx context.Context) Usage {
	now := r.now()
	tok, reason := findToken(r.home(), r.appData(), r.localAppData(), r.keyFn)
	if reason != "" {
		return Usage{Unavailable: reason, FetchedAt: now}
	}
	var renew string
	if r.AutoRenew && r.needsRenew(tok) {
		newTok, err := r.renew(ctx, tok)
		if err != nil {
			renew = "renew_failed"
			r.log().Warn("Claude usage: could not renew the access token; the owner may need to sign in to Claude Desktop again",
				"err", redactErr(err))
			if !tok.expiresAt.IsZero() && !tok.expiresAt.After(now) {
				// Already expired and we could not renew: nothing to read.
				return Usage{Unavailable: "signed_out", FetchedAt: now, Renew: renew}
			}
			// Not yet expired: try the current token anyway.
		} else {
			tok, renew = newTok, "renewed"
		}
	}
	windows, err := r.fetchUsage(ctx, tok.access)
	if errors.Is(err, errUsageParse) {
		r.log().Warn("Claude usage: the usage endpoint's answer could not be parsed", "err", redactErr(err))
		return Usage{Unavailable: "parse_error", FetchedAt: now, Renew: renew}
	}
	if err != nil {
		r.log().Warn("Claude usage: the usage endpoint failed", "err", redactErr(err))
		return Usage{Unavailable: "api_error", FetchedAt: now, Renew: renew}
	}
	return Usage{Windows: windows, FetchedAt: now, Renew: renew}
}

func (r *UsageReader) needsRenew(t oauthToken) bool {
	if t.refresh == "" || t.expiresAt.IsZero() {
		return false
	}
	return !t.expiresAt.After(r.now().Add(renewLeeway))
}

// ---- usage endpoint -----------------------------------------------------

func (r *UsageReader) usageURL() string {
	if r.UsageURL != "" {
		return r.UsageURL
	}
	return defaultUsageURL
}

// windowJSON is one window in the endpoint's response; a window the account
// has none of comes as JSON null and is skipped.
type windowJSON struct {
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

// errUsageParse: the usage endpoint answered 200, but not with a JSON
// object; told apart from the endpoint failing (parse_error, not api_error).
var errUsageParse = errors.New("usage endpoint: the answer is not a JSON object")

// windowsFromBody reads the windows out of the endpoint's answer, leniently:
// the endpoint adds keys over time (limits, weekly_scoped_shares and
// member_dashboard_available came as arrays and a bool, and decoding every
// key as a window failed the whole reading). A key is a window when its
// value is an object with a numeric utilization; any other key is skipped,
// and a resets_at that is missing, null or not a time leaves the time
// unknown rather than the window out. Only an answer that is no JSON object
// at all is an error.
func windowsFromBody(body []byte) ([]Window, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil || top == nil {
		return nil, errUsageParse
	}
	raw := map[string]*windowJSON{}
	for k, v := range top {
		var w struct {
			Utilization *float64        `json:"utilization"`
			ResetsAt    json.RawMessage `json:"resets_at"`
		}
		if json.Unmarshal(v, &w) != nil || w.Utilization == nil {
			continue // null, an array, a bool, or an object that is no window
		}
		win := &windowJSON{Utilization: *w.Utilization}
		var at time.Time
		if json.Unmarshal(w.ResetsAt, &at) == nil {
			win.ResetsAt = at
		}
		raw[k] = win
	}
	return windowsFromResponse(raw), nil
}

func (r *UsageReader) fetchUsage(ctx context.Context, access string) ([]Window, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.usageURL(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("anthropic-beta", oauthBetaHeader)
	req.Header.Set("Accept", "application/json")
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("usage endpoint: HTTP %d", resp.StatusCode)
	}
	return windowsFromBody(body)
}

// windowsFromResponse turns the response map into windows, skipping nulls,
// with five_hour and seven_day first and the rest in name order (so the UI
// and CLI are stable).
func windowsFromResponse(raw map[string]*windowJSON) []Window {
	order := func(k string) int {
		switch k {
		case "five_hour":
			return 0
		case "seven_day":
			return 1
		default:
			return 2
		}
	}
	keys := make([]string, 0, len(raw))
	for k, v := range raw {
		if v != nil {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if order(keys[i]) != order(keys[j]) {
			return order(keys[i]) < order(keys[j])
		}
		return keys[i] < keys[j]
	})
	var out []Window
	for _, k := range keys {
		out = append(out, Window{Key: k, Utilization: raw[k].Utilization, ResetsAt: raw[k].ResetsAt})
	}
	return out
}

// ---- token refresh ------------------------------------------------------

func (r *UsageReader) tokenURL() string {
	if r.TokenURL != "" {
		return r.TokenURL
	}
	return defaultTokenURL
}

// renew exchanges the refresh token for a new access token and writes the
// new tokens back to their source, confirming the write.
func (r *UsageReader) renew(ctx context.Context, t oauthToken) (oauthToken, error) {
	reqBody, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": t.refresh,
		"client_id":     t.clientID,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.tokenURL(), bytes.NewReader(reqBody))
	if err != nil {
		return oauthToken{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := r.httpClient().Do(req)
	if err != nil {
		return oauthToken{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return oauthToken{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return oauthToken{}, fmt.Errorf("token endpoint: HTTP %d", resp.StatusCode)
	}
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil || tr.AccessToken == "" {
		return oauthToken{}, fmt.Errorf("token endpoint: no access_token")
	}
	newRefresh := tr.RefreshToken
	if newRefresh == "" {
		newRefresh = t.refresh
	}
	expiresAt := t.expiresAt
	if tr.ExpiresIn > 0 {
		expiresAt = r.now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}
	if t.src != nil {
		if err := t.src.write(tr.AccessToken, newRefresh, expiresAt); err != nil {
			return oauthToken{}, fmt.Errorf("write-back: %w", err)
		}
	}
	t.access, t.refresh, t.expiresAt = tr.AccessToken, newRefresh, expiresAt
	return t, nil
}

// redactErr keeps a logged error from carrying anything but an error's
// shape. Our own errors never embed the token (it travels only in the
// Authorization header, never in a URL), but a transport error wraps the
// request URL, so this strips a *url.Error down to its op, the host, and
// the underlying cause — never the path or any query.
func redactErr(err error) string {
	if err == nil {
		return ""
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		host := ue.URL
		if u, perr := url.Parse(ue.URL); perr == nil && u.Host != "" {
			host = u.Host
		}
		return fmt.Sprintf("%s %s: %v", ue.Op, host, ue.Err)
	}
	return err.Error()
}
