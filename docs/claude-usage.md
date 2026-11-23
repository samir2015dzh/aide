# Claude plan usage

The scheduler pages work to Claude against hard caps: a five-hour rolling
window and weekly limits. Hitting one mid-batch wastes the run, so Aide
reads the account's usage on the machine and reports how full each window
is and when it resets. This is read-only to the scheduler; the account's
token, which the reading needs, never leaves the machine.

## Where the numbers come from

Three sources, in order; the first with the plan's windows gives the
reading (工单 #86, #104). What each source said on the way is kept in the
reading's `detail` and aided's log (`Claude Code cache: …; claude -p
/usage: …; usage endpoint: …`), so a machine without a reading says why at
every source.

**1. What this machine's Claude Code last read (工单 #104).** Claude Code
keeps the usage endpoint's last answer in its global config,
`~/.claude.json` (or `$CLAUDE_CONFIG_DIR/.claude.json`; an older version's
`~/.claude/.config.json`), under `cachedUsageUtilization`: `fetchedAtMs`
(when it read it), `accountUuid` (whose) and `utilization` (the endpoint's
answer as it came, read as below). It is written whenever Claude Code reads
the plan — its `/usage`, its usage displays — and seen in Claude Code
2.1.330; it is undocumented, and a version without it leaves the key out.
aided takes it when it is fresher than a reading of its own would be
(`usageEvery`, fifteen minutes), with the time Claude Code read it; a cache
for another account than the one signed in there (`oauthAccount`) is not
taken. Reading it asks nothing of anyone and needs no token. The reading
says `source: claude_code_cache`.

**2. This machine's own Claude Code (工单 #86).** Where Claude Code is
installed and signed in here, aided asks it, without a terminal and keeping
no session:

    claude -p /usage --output-format stream-json --verbose --no-session-persistence

`/usage` is Claude Code's own command, not a prompt: it asks Anthropic with
Claude Code's own sign-in (which Claude Code renews itself) and writes the
same report its `/usage` shows. Its answer carries the plan's usage rows as a
structured twin of that text (`usage_report.rate_limits.limits`: `kind`
`session` → `five_hour`, `weekly_all` → `seven_day`, `weekly_scoped` →
`seven_day_<model>`, each `percent` and `resets_at`); a version that writes
no twin is read from its text (`Current session: 20% used`), percentages
only. The reading says `source: claude_code`.

- Only a sign-in that lets Claude Code read the plan gets those rows (a
  claude.ai plan sign-in whose token carries the profile scope, not billed
  by usage). Any other — seen on a real machine — gets **only that call's
  own totals** (`Estimated cost: $0.0000`, `Total duration (API): 0s`,
  `Usage: 0 input, 0 output, …`). That is no plan reading and is never taken
  for one (`cli_session_only`): the endpoint below is asked.
- Claude Code answering a plan's report without its rows (`You are
  currently using your subscription…` and nothing more: its own reading
  failing, rate limited, offline; `cli_no_reading`): the last reading
  stays, stale; the endpoint below is **not** asked past it, since it is the
  same endpoint and asking twice only presses its limit.
- A version that takes `/usage` for something else than its command (it
  does not answer as `local_command: usage`): the endpoint below instead,
  and that version is not asked again until aided restarts.
- No signed-in Claude Code here, or the command failing: the endpoint below.

aided does not read Claude Desktop's own usage page, nor the app's web
session it is read with; the endpoint below, asked with the desktop app's
own token, stands in for it.

**3. The usage endpoint:** Anthropic's usage endpoint `GET https://api.anthropic.com/api/oauth/usage`
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
  `renew`, `source`. aided refreshes it in the background every fifteen
  minutes (`usageEvery`) and keeps the last good reading — on disk too, so a
  restart does not blank it; any failed refresh keeps it, marked stale, with
  when it was read. A status is never held up on the fetch. The desktop
  shows one small line on the Claude row and on Claude Code's bot page (e.g.
  `Plan 5h 20% · Week 59% · read 3 min ago`; `last reading … ago` when
  stale), or `plan usage not read yet` when there has never been a reading.
- **CLI** — `aide usage` prints the windows per machine (and `-o json` the
  machine-readable fields); the same `usage` is on `aide agent list -o json`
  for Claude Code. Fields are in [cli-output.md](cli-output.md) under
  **Usage**.

### When there is no reading

`unavailable` says why, and the UI shows "read-fail" / "读不到" rather than
a number: `signed_out` (no signed-in token found), `no_token` (a cache was
found but could not be read), `api_error` (the endpoint failed),
`unsupported` (this OS can't open the desktop token cache), `parse_error`
(the endpoint answered 200 but not with a JSON object), or `cli_no_reading` (Claude Code's own reading failed; the endpoint
was not asked past it). The source tried last gives the reason; `detail`
says what every source said.

`api_error` and `parse_error` never come alone: `detail` carries what lay
behind them — the HTTP status and a summary of the answer (Anthropic's
`error.type: error.message` when the body is its error JSON, else the first
200 characters with whitespace collapsed and anything token-like masked), or
the transport error with only its host. `AIDE_CLAUDE_USAGE_URL` in aided's
environment points the reading at a stand-in endpoint (the end-to-end test
uses one; it is also a way to see what a proxy in the way answers). The same line goes to aided's log,
`aide usage` prints it after the reason, and the desktop shows it on hover
over the "unavailable" note. A stale reading carries the detail of the
refresh that failed. A failed auto-renew adds `renew: …` with the token
endpoint's status and error code — its body is otherwise never shown,
since a successful answer carries tokens. Local transcript
token counts are never substituted for the plan figure — they miss other
devices and the web, so the cap they imply is wrong.
