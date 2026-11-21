package claude

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// This file finds the account's OAuth access token on the machine, to read
// Claude plan usage with. It never leaves aided's memory, never goes to a
// log or over the wire, and is never cached to disk by us; only the usage
// windows it buys are reported. Two sources, in order:
//
//  1. the standard Claude Code credentials file (~/.claude/.credentials.json),
//     a plain JSON the CLI writes when signed in there;
//  2. the signed-in Claude desktop app's token cache (config.json's
//     oauth:tokenCacheV2), sealed with OSCrypt (Windows only so far).
//
// The owner here uses only the desktop app, so (2) is the real source and
// (1) is a fallback for a machine signed in through the CLI.

// oauthToken is an access token and what refreshing and writing it back
// need. The secrets (access, refresh) live only as long as the read.
type oauthToken struct {
	access    string
	refresh   string
	expiresAt time.Time
	clientID  string
	scopes    []string
	src       tokenWriteback // how to persist a refreshed token, if asked
}

// grantsInference reports whether the token's scopes let it call the usage
// endpoint (user:inference). A token that lists no scopes is tried anyway:
// the endpoint is the real check.
func (t oauthToken) grantsInference() bool {
	if len(t.scopes) == 0 {
		return true
	}
	for _, s := range t.scopes {
		if s == "user:inference" {
			return true
		}
	}
	return false
}

// tokenWriteback persists a refreshed token to the source it came from,
// touching only that token and leaving everything else as it was, writing
// atomically and reading back to confirm. A nil writeback means the source
// is read-only to us (we never found a safe way to rewrite it).
type tokenWriteback interface {
	write(access, refresh string, expiresAt time.Time) error
}

// findToken returns the first usable token, or an empty reason:
// "signed_out" (none found), "no_token" (a cache is there but unreadable),
// "unsupported" (a desktop cache is there, on an OS that can't open it).
func findToken(home, appData, localAppData string, keyFn oscryptKeyFunc) (oauthToken, string) {
	// (1) standard credentials.
	if t, ok, _ := credentialsToken(home); ok {
		return t, ""
	}
	// (2) desktop token cache.
	dirs := desktopUserData(home, appData, localAppData)
	return desktopToken(dirs, keyFn)
}

// ---- source 1: standard Claude Code credentials -------------------------

// credentialsPath is where the Claude CLI keeps its OAuth tokens on a
// machine signed in through it.
func credentialsPath(home string) string {
	return filepath.Join(home, ".claude", ".credentials.json")
}

type credentialsFile struct {
	ClaudeAIOAuth struct {
		AccessToken  string   `json:"accessToken"`
		RefreshToken string   `json:"refreshToken"`
		ExpiresAt    int64    `json:"expiresAt"` // Unix milliseconds
		Scopes       []string `json:"scopes"`
		ClientID     string   `json:"clientId"`
	} `json:"claudeAiOauth"`
}

func credentialsToken(home string) (oauthToken, bool, error) {
	path := credentialsPath(home)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return oauthToken{}, false, nil
		}
		return oauthToken{}, false, err
	}
	var cf credentialsFile
	if err := json.Unmarshal(b, &cf); err != nil {
		return oauthToken{}, false, fmt.Errorf("parsing %s: %w", path, err)
	}
	o := cf.ClaudeAIOAuth
	if o.AccessToken == "" {
		return oauthToken{}, false, nil
	}
	return oauthToken{
		access:    o.AccessToken,
		refresh:   o.RefreshToken,
		expiresAt: msTime(o.ExpiresAt),
		clientID:  o.ClientID,
		scopes:    o.Scopes,
		src:       &credentialsWriteback{path: path, raw: b},
	}, true, nil
}

// credentialsWriteback rewrites claudeAiOauth's tokens in place, keeping
// every other key of the JSON.
type credentialsWriteback struct {
	path string
	raw  []byte
}

func (w *credentialsWriteback) write(access, refresh string, expiresAt time.Time) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(w.raw, &top); err != nil {
		return err
	}
	var oauth map[string]json.RawMessage
	if err := json.Unmarshal(top["claudeAiOauth"], &oauth); err != nil {
		return err
	}
	set := func(k string, v any) {
		b, _ := json.Marshal(v)
		oauth[k] = b
	}
	set("accessToken", access)
	if refresh != "" {
		set("refreshToken", refresh)
	}
	set("expiresAt", expiresAt.UnixMilli())
	nb, _ := json.Marshal(oauth)
	top["claudeAiOauth"] = nb
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(w.path, out, 0o600); err != nil {
		return err
	}
	// Read back and confirm the new token is there.
	back, _, err := credentialsToken(filepath.Dir(filepath.Dir(w.path)))
	if err != nil || back.access != access {
		return fmt.Errorf("credentials write-back did not verify")
	}
	return nil
}

// ---- source 2: Claude desktop app token cache ---------------------------

// oscryptKeyFunc returns the OSCrypt key for a desktop user-data dir (the
// one holding config.json and Local State). Overridable in tests; the real
// one is defaultOSCryptKey.
type oscryptKeyFunc func(userDataDir string) ([]byte, error)

// defaultOSCryptKey reads Local State's os_crypt key from the user-data dir
// and unwraps it with the platform key store (DPAPI on Windows).
func defaultOSCryptKey(userDataDir string) ([]byte, error) {
	if !desktopKeyStoreAvailable {
		return nil, errUnsupportedOS
	}
	wrapped, err := localStateEncryptedKey(filepath.Join(userDataDir, "Local State"))
	if err != nil {
		return nil, err
	}
	return dpapiUnprotect(wrapped)
}

// desktopToken opens the token cache of the signed-in desktop app and picks
// the entry that can call the usage endpoint. reason is "" on success, else
// "signed_out" (no cache), "no_token" (cache present but unreadable), or
// "unsupported" (cache present, this OS can't open it). The OS gate lives
// in the key function, so an injected key opens a cache on any OS (tests),
// while the real key function reports unsupported off Windows.
func desktopToken(dirs []string, keyFn oscryptKeyFunc) (oauthToken, string) {
	if keyFn == nil {
		keyFn = defaultOSCryptKey
	}
	var sawCache, unsupported bool
	for _, dir := range dirs {
		cfgPath := filepath.Join(dir, "config.json")
		cfg, err := readConfig(cfgPath)
		if err != nil {
			continue
		}
		enc := cfg.str("oauth:tokenCacheV2")
		if enc == "" {
			enc = cfg.str("oauth:tokenCache")
		}
		if enc == "" {
			continue
		}
		sawCache = true
		key, err := keyFn(dir)
		if err != nil {
			if errors.Is(err, errUnsupportedOS) {
				unsupported = true
			}
			continue
		}
		blob, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			continue
		}
		plain, err := oscryptOpen(key, blob)
		if err != nil {
			continue
		}
		entries, err := parseTokenCache(plain)
		if err != nil {
			continue
		}
		idx := pickInferenceEntry(entries)
		if idx < 0 {
			continue
		}
		e := entries[idx]
		return oauthToken{
			access:    e.token,
			refresh:   e.refreshToken,
			expiresAt: msTime(e.expiresAt),
			clientID:  e.clientID,
			scopes:    e.scopes,
			src: &desktopWriteback{
				cfgPath: cfgPath,
				cfgKey:  cfg.cacheKey,
				keyFn:   keyFn,
				userDir: dir,
				entryID: e.id,
			},
		}, ""
	}
	if !sawCache {
		return oauthToken{}, "signed_out"
	}
	if unsupported {
		return oauthToken{}, "unsupported"
	}
	return oauthToken{}, "no_token"
}

// desktopConfig is the parsed config.json, remembering which key held the
// token cache so a write-back replaces the same one.
type desktopConfig struct {
	m        map[string]json.RawMessage
	cacheKey string
}

func (c desktopConfig) str(k string) string {
	var s string
	if raw, ok := c.m[k]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

func readConfig(path string) (desktopConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return desktopConfig{}, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return desktopConfig{}, err
	}
	key := "oauth:tokenCacheV2"
	if _, ok := m[key]; !ok {
		if _, ok := m["oauth:tokenCache"]; ok {
			key = "oauth:tokenCache"
		}
	}
	return desktopConfig{m: m, cacheKey: key}, nil
}

// cacheEntry is one token-cache record, however the cache names its fields.
type cacheEntry struct {
	id           string
	token        string
	refreshToken string
	expiresAt    int64 // Unix milliseconds
	clientID     string
	scopes       []string
}

// parseTokenCache reads the decrypted cache. Its exact shape is the desktop
// app's private one and may change, so this is lenient: the cache is a JSON
// object of id→record; a record is an object carrying a token under one of
// a few field names. Scopes and client id come from the record when it has
// them, else from the id string (MSAL keys them
// acct:<account>:<clientId>:https://api.anthropic.com:<scopes>).
func parseTokenCache(plain []byte) ([]cacheEntry, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(plain, &raw); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []cacheEntry
	for _, id := range ids {
		var rec struct {
			Token        string          `json:"token"`
			AccessToken  string          `json:"accessToken"`
			Secret       string          `json:"secret"`
			RefreshToken string          `json:"refreshToken"`
			ExpiresAt    json.RawMessage `json:"expiresAt"`
			ExpiresOn    json.RawMessage `json:"expires_on"`
			ClientID     string          `json:"clientId"`
			ClientID2    string          `json:"client_id"`
			Scopes       []string        `json:"scopes"`
			Scope        string          `json:"scope"`
			Target       string          `json:"target"`
		}
		if json.Unmarshal(raw[id], &rec) != nil {
			continue
		}
		tok := firstNonEmpty(rec.Token, rec.AccessToken, rec.Secret)
		if tok == "" {
			continue
		}
		e := cacheEntry{
			id:           id,
			token:        tok,
			refreshToken: rec.RefreshToken,
			expiresAt:    parseEpochMillis(rec.ExpiresAt, rec.ExpiresOn),
			clientID:     firstNonEmpty(rec.ClientID, rec.ClientID2),
			scopes:       recordScopes(rec.Scopes, rec.Scope, rec.Target),
		}
		if e.clientID == "" || len(e.scopes) == 0 {
			kClient, kScopes := scopesFromKey(id)
			if e.clientID == "" {
				e.clientID = kClient
			}
			if len(e.scopes) == 0 {
				e.scopes = kScopes
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// pickInferenceEntry chooses the entry that can call the usage endpoint:
// the first with user:inference in its scopes, else, if none lists scopes,
// the first entry at all. Returns -1 when there is nothing usable.
func pickInferenceEntry(entries []cacheEntry) int {
	anyScoped := false
	for i, e := range entries {
		for _, s := range e.scopes {
			anyScoped = true
			if s == "user:inference" {
				return i
			}
		}
	}
	if !anyScoped && len(entries) > 0 {
		return 0
	}
	return -1
}

// desktopWriteback re-seals the cache with a refreshed token in the chosen
// entry, keeping every other entry and every other config key byte for
// byte, writes config.json atomically, and reads it back to confirm.
type desktopWriteback struct {
	cfgPath string
	cfgKey  string
	keyFn   oscryptKeyFunc
	userDir string
	entryID string
}

func (w *desktopWriteback) write(access, refresh string, expiresAt time.Time) error {
	key, err := w.keyFn(w.userDir)
	if err != nil {
		return err
	}
	cfg, err := readConfig(w.cfgPath)
	if err != nil {
		return err
	}
	enc := cfg.str(w.cfgKey)
	blob, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return err
	}
	plain, err := oscryptOpen(key, blob)
	if err != nil {
		return err
	}
	var cache map[string]json.RawMessage
	if err := json.Unmarshal(plain, &cache); err != nil {
		return err
	}
	rec, ok := cache[w.entryID]
	if !ok {
		return fmt.Errorf("token-cache entry %q is gone", w.entryID)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(rec, &m); err != nil {
		return err
	}
	keep := cloneRawMap(cache) // to restore on a verify failure
	setField(m, []string{"token", "accessToken", "secret"}, access)
	if refresh != "" {
		setField(m, []string{"refreshToken"}, refresh)
	}
	setEpoch(m, []string{"expiresAt", "expires_on"}, expiresAt.UnixMilli())
	nm, _ := json.Marshal(m)
	cache[w.entryID] = nm
	newPlain, _ := json.Marshal(cache)
	sealed, err := oscryptSeal(key, newPlain)
	if err != nil {
		return err
	}
	cfg.m[w.cfgKey], _ = json.Marshal(base64.StdEncoding.EncodeToString(sealed))
	out, err := json.MarshalIndent(cfg.m, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(w.cfgPath, out, 0o600); err != nil {
		return err
	}
	if err := w.verify(key, access); err != nil {
		// Put the old cache back so the desktop app can still read it.
		cfg.m[w.cfgKey] = reseal(key, keep, enc)
		if restored, mErr := json.MarshalIndent(cfg.m, "", "  "); mErr == nil {
			_ = atomicWrite(w.cfgPath, restored, 0o600)
		}
		return err
	}
	return nil
}

func (w *desktopWriteback) verify(key []byte, want string) error {
	cfg, err := readConfig(w.cfgPath)
	if err != nil {
		return err
	}
	blob, err := base64.StdEncoding.DecodeString(cfg.str(w.cfgKey))
	if err != nil {
		return err
	}
	plain, err := oscryptOpen(key, blob)
	if err != nil {
		return err
	}
	entries, err := parseTokenCache(plain)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.id == w.entryID && e.token == want {
			return nil
		}
	}
	return errors.New("token-cache write-back did not verify")
}

// reseal re-encrypts the kept cache, or falls back to the original base64
// when that fails, so a restore never leaves an unreadable cache.
func reseal(key []byte, cache map[string]json.RawMessage, original string) json.RawMessage {
	if plain, err := json.Marshal(cache); err == nil {
		if sealed, err := oscryptSeal(key, plain); err == nil {
			b, _ := json.Marshal(base64.StdEncoding.EncodeToString(sealed))
			return b
		}
	}
	b, _ := json.Marshal(original)
	return b
}

// ---- small helpers ------------------------------------------------------

func msTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func recordScopes(scopes []string, scope, target string) []string {
	if len(scopes) > 0 {
		return scopes
	}
	if s := firstNonEmpty(scope, target); s != "" {
		return strings.Fields(s)
	}
	return nil
}

// scopesFromKey reads the client id and scopes out of an MSAL-style cache
// key: acct:<account>:<clientId>:https://api.anthropic.com:<space scopes>.
// The resource URL has its own colons, so it is matched as a whole rather
// than split blindly.
func scopesFromKey(id string) (clientID string, scopes []string) {
	parts := strings.Split(id, ":")
	if len(parts) < 3 {
		return "", nil
	}
	clientID = parts[2]
	// Scopes follow the resource URL: everything after the first colon past
	// "https://<host>" (each scope is itself "word:word", so they can't be
	// split off blindly). A host with a port would fool this; the API has
	// none.
	const scheme = "https://"
	if i := strings.Index(id, scheme); i >= 0 {
		rest := id[i+len(scheme):] // <host>:<space-separated scopes>
		if c := strings.Index(rest, ":"); c >= 0 {
			scopes = strings.Fields(rest[c+1:])
		}
	}
	return clientID, scopes
}

func parseEpochMillis(raws ...json.RawMessage) int64 {
	for _, raw := range raws {
		if len(raw) == 0 {
			continue
		}
		// Number (ms) or a quoted number.
		var n int64
		if json.Unmarshal(raw, &n) == nil && n != 0 {
			return normalizeMillis(n)
		}
		var s string
		if json.Unmarshal(raw, &s) == nil && s != "" {
			if v, err := parseIntLoose(s); err == nil {
				return normalizeMillis(v)
			}
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return t.UnixMilli()
			}
		}
	}
	return 0
}

// normalizeMillis treats a value small enough to be seconds (10 digits) as
// seconds, scaling it to milliseconds; a 13-digit value is already ms.
func normalizeMillis(v int64) int64 {
	if v > 0 && v < 1e12 {
		return v * 1000
	}
	return v
}

func parseIntLoose(s string) (int64, error) {
	s = strings.TrimSpace(s)
	var n int64
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

func setField(m map[string]json.RawMessage, names []string, value string) {
	b, _ := json.Marshal(value)
	for _, n := range names {
		if _, ok := m[n]; ok {
			m[n] = b
			return
		}
	}
	m[names[0]] = b
}

func setEpoch(m map[string]json.RawMessage, names []string, ms int64) {
	for _, n := range names {
		if raw, ok := m[n]; ok {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				m[n], _ = json.Marshal(fmt.Sprintf("%d", ms))
			} else {
				m[n], _ = json.Marshal(ms)
			}
			return
		}
	}
	m[names[0]], _ = json.Marshal(ms)
}

func cloneRawMap(m map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

// atomicWrite writes b to path through a temp file in the same directory
// and a rename, so a reader never sees a half-written file.
func atomicWrite(path string, b []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".aide-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
