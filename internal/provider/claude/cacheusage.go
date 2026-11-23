package claude

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Plan usage as this machine's Claude Code last read it (工单 #104). Claude
// Code keeps the usage endpoint's last answer in its global config,
// ~/.claude.json (or $CLAUDE_CONFIG_DIR/.claude.json), so that its own
// usage displays have figures before they ask again:
//
//	"cachedUsageUtilization": {
//	  "fetchedAtMs": 1763640000000,
//	  "accountUuid": "…",
//	  "utilization": {"five_hour": {"utilization": 20, "resets_at": "…"}, "seven_day": {…}, …}
//	}
//
// utilization is the endpoint's answer as it came (the shape UsageReader
// reads); fetchedAtMs is when Claude Code read it; accountUuid the account
// it read it for, which Claude Code drops the cache over when another
// account signs in (oauthAccount.accountUuid). Seen in Claude Code 2.1.330;
// a version that keeps no such cache leaves the key out, and this source
// has nothing. Reading it asks nothing of anyone and needs no token.

// Why the cache gave no reading, as Usage.Unavailable says it.
const (
	// No config file, or no cached usage in it.
	CacheNone = "cache_none"
	// A cache, but for another account than the one signed in.
	CacheOtherAccount = "cache_other_account"
	// A config file or cache that is not the JSON expected.
	CacheUnreadable = "cache_unreadable"
)

// CacheUsage reads the plan usage Claude Code cached here.
type CacheUsage struct {
	// Home and ConfigDir override discovery ("": the user's home and
	// $CLAUDE_CONFIG_DIR).
	Home, ConfigDir string
}

// configFiles are where Claude Code keeps its global config, in the order
// it looks: $CLAUDE_CONFIG_DIR/.claude.json when that is set, else
// ~/.claude.json; ~/.claude/.config.json is an older version's.
func (c *CacheUsage) configFiles() []string {
	home, dir := c.Home, c.ConfigDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if dir == "" {
		dir = os.Getenv("CLAUDE_CONFIG_DIR")
	}
	var out []string
	if dir != "" {
		out = append(out, filepath.Join(dir, ".claude.json"))
	}
	if home != "" {
		out = append(out, filepath.Join(home, ".claude.json"), filepath.Join(home, ".claude", ".config.json"))
	}
	return out
}

// Read reads the cache once. A reading carries when Claude Code read it
// (FetchedAt), not now: how fresh it is, is the caller's to judge.
func (c *CacheUsage) Read() Usage {
	for _, f := range c.configFiles() {
		b, err := os.ReadFile(f)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			// The error names the full path; the detail keeps to the file's name.
			why := "could not be read"
			if errors.Is(err, fs.ErrPermission) {
				why = "may not be read"
			}
			return Usage{Unavailable: CacheUnreadable, Detail: "Claude Code cache: " + filepath.Base(f) + " " + why}
		}
		return usageFromConfig(b, filepath.Base(f))
	}
	return Usage{Unavailable: CacheNone, Detail: "Claude Code cache: no config file of Claude Code's here"}
}

// usageFromConfig reads the cached usage out of Claude Code's global config.
func usageFromConfig(b []byte, name string) Usage {
	var cfg struct {
		Cached *struct {
			FetchedAtMs *float64        `json:"fetchedAtMs"`
			AccountUUID string          `json:"accountUuid"`
			Utilization json.RawMessage `json:"utilization"`
		} `json:"cachedUsageUtilization"`
		Account *struct {
			AccountUUID string `json:"accountUuid"`
		} `json:"oauthAccount"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Usage{Unavailable: CacheUnreadable, Detail: "Claude Code cache: " + name + " is not JSON"}
	}
	if cfg.Cached == nil {
		return Usage{Unavailable: CacheNone, Detail: "Claude Code cache: " + name + " keeps no plan usage (Claude Code here has not read it)"}
	}
	if cfg.Cached.FetchedAtMs == nil || len(cfg.Cached.Utilization) == 0 {
		return Usage{Unavailable: CacheUnreadable, Detail: "Claude Code cache: its plan usage has no time or figures"}
	}
	if cfg.Account != nil && cfg.Cached.AccountUUID != "" && cfg.Cached.AccountUUID != cfg.Account.AccountUUID {
		return Usage{Unavailable: CacheOtherAccount, Detail: "Claude Code cache: read for another account than the one signed in"}
	}
	windows, err := windowsFromBody(cfg.Cached.Utilization)
	if err != nil {
		return Usage{Unavailable: CacheUnreadable, Detail: "Claude Code cache: its plan usage is not the usage endpoint's answer"}
	}
	at := time.UnixMilli(int64(*cfg.Cached.FetchedAtMs))
	if len(windows) == 0 {
		return Usage{Unavailable: CacheNone, FetchedAt: at, Detail: "Claude Code cache: no plan windows in it"}
	}
	return Usage{Windows: windows, FetchedAt: at}
}
