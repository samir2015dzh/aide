# Claude plan usage

The scheduler pages work to Claude against hard caps: a five-hour rolling
window and weekly limits. Hitting one mid-batch wastes the run, so Aide
reads the account's usage on the machine and reports how full each window
is and when it resets. This is read-only to the scheduler; the account's
token, which the reading needs, never leaves the machine.

## Where the numbers come from

Anthropic's usage endpoint `GET https://api.anthropic.com/api/oauth/usage`
(the same data Claude Code's `/usage` shows; undocumented and liable to
change) answers with each window's `utilization` (0–100) and `resets_at`,
called with the account's OAuth access token as a bearer and the
`anthropic-beta: oauth-2025-04-20` header. `five_hour` and `seven_day` are
always there; a plan may add model-specific weekly caps (`seven_day_opus`,
…), and a window the account has none of comes as `null` and is dropped.

## Finding the token (on the machine only)

aided looks for the access token in two places, in order, and never writes
it anywhere of its own:

1. **The standard Claude Code credentials** —
   `~/.claude/.credentials.json`, the plain JSON the CLI writes when signed
   in through it (`claudeAiOauth`: `accessToken`, `refreshToken`,
   `expiresAt`, `scopes`).
2. **The signed-in Claude desktop app's token cache** — `config.json`'s
   `oauth:tokenCacheV2` (or the older `oauth:tokenCache`), sealed with
   Chromium's OSCrypt. The owner here uses only the app, so this is the
   real source.

The desktop data root is found by mechanism, not by the paths seen on one
computer: the same discovery as the Code-session listing and the login-state
probe (`internal/provider/claude`), which covers the MSIX (Microsoft Store)
and ordinary installs and multiple data roots.

### Opening the desktop token cache

The cache value is `base64( OSCrypt( MSAL-style JSON ) )`. OSCrypt v10 is
`"v10"` + a 12-byte AES-GCM nonce + ciphertext + a 16-byte tag, under a
256-bit key the app keeps, DPAPI-wrapped, in `Local State`'s
`os_crypt.encrypted_key`. On Windows aided unwraps that key with
`CryptUnprotectData` for the current user (so a copy of the files taken to
another machine opens nothing) and decrypts the cache. Off Windows there is
no equivalent key store, so the desktop cache is reported `unsupported` and
only the standard credentials path works. The decrypted cache holds a few
entries; the one whose scopes grant `user:inference` is the one that can
call the usage endpoint.

## Auto-renew

Both switches — reading and automatic renewal — are on. When the access
token is expired or within ten minutes of it, aided exchanges the entry's
refresh token for a new access token (standard OAuth refresh against the
token endpoint) and writes the new tokens back to the source they came
from, touching only that one entry and leaving every other entry and
config key as it was. The write is atomic (temp file then rename) and read
back to confirm; if the readback does not match, aided restores the prior
cache so the desktop app can still read it. A refresh or write-back that
fails never corrupts the cache: the reading falls back to the old token
when it is still valid, or reports that the owner should sign in to Claude
Desktop again.

## Safety boundary

The access token and refresh token are used only in aided's memory on the
machine. They never reach the relay, a log, an upload field, or a cache
file of ours. What is reported — over the machine's status, the CLI, and
the desktop row — is only each window's `utilization` and `resets_at`, when
the reading was taken, whether it is a stale fallback, and whether a renew
succeeded or failed. Development fixtures use synthetic data and a known
key; no real token or response sample is needed to build or test this.

## Reading it

- **Status / desktop** — the Claude Code agent row carries a `ClaudeUsage`
  (`proto/aide/machine/v1`): `windows` (`UsageWindow`: `key`,
  `utilization`, `resets_at`), `fetched_at`, `stale`, `unavailable`,
  `renew`. aided refreshes it in the background at most every five minutes
  and keeps the last good reading; a status is never held up on the fetch.
  The desktop Agent list shows one small line on the Claude row (e.g.
  `Plan 5h 20% · Week 59%`), or `plan usage unavailable` when there is no
  reading.
- **CLI** — `aide usage` prints the windows per machine (and `-o json` the
  machine-readable fields); the same `usage` is on `aide agent list -o json`
  for Claude Code. Fields are in [cli-output.md](cli-output.md) under
  **Usage**.

### When there is no reading

`unavailable` says why, and the UI shows "read-fail" / "读不到" rather than
a number: `signed_out` (no signed-in token found), `no_token` (a cache was
found but could not be read), `api_error` (the endpoint failed),
`unsupported` (this OS can't open the desktop token cache). Local transcript
token counts are never substituted for the plan figure — they miss other
devices and the web, so the cap they imply is wrong.
