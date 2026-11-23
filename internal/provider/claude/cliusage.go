package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

// Plan usage as this machine's Claude Code tells it (工单 #86): the same
// reading /usage shows, asked of the command line without a terminal:
//
//	claude -p /usage --output-format stream-json --verbose --no-session-persistence
//
// /usage is a command of Claude Code's own, not a prompt: it asks Anthropic
// with Claude Code's own sign-in (which Claude Code keeps renewed) and
// writes no session. Its answer carries the plan's usage rows as a
// structured twin of the text (usage_report.rate_limits.limits, Claude Code
// 2.1 and later); a version without them still writes the text, whose
// percentages are read then ("Current session: 20% used"). Nothing of the
// sign-in passes through Aide.
//
// Only a sign-in that lets Claude Code read the plan gets the plan's rows
// (a claude.ai plan sign-in, a token with the profile scope, not billed by
// usage); any other writes this call's own totals instead ("Estimated
// cost: $0.0000 … Usage: 0 input, 0 output …", 工单 #104, seen on a real
// machine). That report is told apart (CLISessionOnly) and never taken for
// a reading.

// CLIUsageArgs are the arguments the reading is asked with.
var CLIUsageArgs = []string{"-p", "/usage", "--output-format", "stream-json", "--verbose", "--no-session-persistence"}

// Why a reading through Claude Code failed, as Usage.Unavailable says it.
const (
	// The command did not run to its end (not there, timed out, failed).
	CLIFailed = "cli_failed"
	// It ran, but took /usage for something else than its usage command:
	// a version that does not know it. Asking again is no use.
	CLIUnsupported = "cli_unsupported"
	// It answered /usage as a plan's (it says the subscription powers its
	// usage) but without the plan's rows: its own reading failing (rate
	// limited, offline). The usage endpoint is not asked past it.
	CLINoReading = "cli_no_reading"
	// It answered /usage with only this call's own totals (cost, duration,
	// tokens) and no plan rows (工单 #104): what Claude Code writes when its
	// sign-in here does not let it read the plan (not a claude.ai plan
	// sign-in, or a token without the profile scope). That is no reading;
	// the next source is asked.
	CLISessionOnly = "cli_session_only"
)

// CLIUsage reads the plan usage through Claude Code.
type CLIUsage struct {
	// Run runs the command line (nil: provider.RunCommand, in a folder of
	// no project's).
	Run func(ctx context.Context, bin string, args []string) ([]byte, error)
	// Now overrides the clock (nil: time.Now).
	Now func() time.Time
}

// Read asks Claude Code at bin for the usage once.
func (c *CLIUsage) Read(ctx context.Context, bin string) Usage {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	run := c.Run
	if run == nil {
		run = func(ctx context.Context, bin string, args []string) ([]byte, error) {
			out, _, err := provider.RunCommand(ctx, bin, args, os.TempDir(), provider.ProbeEnv(""))
			return out, err
		}
	}
	out, err := run(ctx, bin, CLIUsageArgs)
	at := now()
	if err != nil {
		return Usage{Unavailable: CLIFailed, FetchedAt: at, Detail: "claude -p /usage: " + redactErr(err)}
	}
	u := usageFromCLI(out)
	u.FetchedAt = at
	return u
}

// cliLine is the part of a line of Claude Code's stream-json read.
type cliLine struct {
	Type            string `json:"type"`
	LocalCommand    string `json:"local_command"`
	LocalCommandRun *struct {
		Command string `json:"command"`
	} `json:"local_command_run"`
	UsageReport *struct {
		RateLimits *struct {
			Limits []cliLimit `json:"limits"`
		} `json:"rate_limits"`
	} `json:"usage_report"`
	Message *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Result string `json:"result"`
}

// cliLimit is one of the plan's usage rows, as the usage endpoint sent it.
type cliLimit struct {
	Kind     string   `json:"kind"`
	Percent  *float64 `json:"percent"`
	ResetsAt *string  `json:"resets_at"`
	Scope    *struct {
		Model *struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

// planHeader is the line /usage starts a plan's report with ("You are
// currently using your subscription to power your Claude Code usage").
var planHeader = regexp.MustCompile(`power your Claude Code usage`)

// sessionTotals is a row of the report of this call's own totals, which
// /usage writes in place of the plan's when it can't read the plan
// ("Estimated cost:  $0.0000", "Total duration (API):  0s").
var sessionTotals = regexp.MustCompile(`(?m)^\s*(Estimated cost|Total cost|Total duration \(API\)):`)

// textRow is a row of /usage's text: "Current week (all models): 59% used · resets …".
var textRow = regexp.MustCompile(`(?m)^\s*Current (session|week \(([^)]*)\)):\s*([\d.]+)% used`)

// usageFromCLI reads the windows out of what `claude -p /usage` wrote.
func usageFromCLI(out []byte) Usage {
	usage := false // it took /usage as its usage command
	var windows []Window
	structured := false
	var text strings.Builder
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		var l cliLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if l.LocalCommand == "usage" || (l.LocalCommandRun != nil && l.LocalCommandRun.Command == "usage") {
			usage = true
		}
		if l.UsageReport != nil && l.UsageReport.RateLimits != nil && !structured {
			structured = true
			for _, r := range l.UsageReport.RateLimits.Limits {
				key := r.windowKey()
				if key == "" || r.Percent == nil {
					continue
				}
				w := Window{Key: key, Utilization: *r.Percent}
				if r.ResetsAt != nil {
					w.ResetsAt, _ = time.Parse(time.RFC3339Nano, *r.ResetsAt)
				}
				windows = append(windows, w)
			}
		}
		if l.Message != nil {
			for _, c := range l.Message.Content {
				if c.Type == "text" {
					text.WriteString(c.Text + "\n")
				}
			}
		}
		text.WriteString(l.Result + "\n")
	}
	if !usage {
		return Usage{Unavailable: CLIUnsupported, Detail: "claude -p /usage: not its usage command in this version"}
	}
	if !structured {
		seen := map[string]bool{}
		for _, m := range textRow.FindAllStringSubmatch(text.String(), -1) {
			key := "five_hour"
			if m[1] != "session" {
				key = textWeekKey(m[2])
			}
			v, err := strconv.ParseFloat(m[3], 64)
			if key == "" || err != nil || seen[key] {
				continue
			}
			seen[key] = true
			windows = append(windows, Window{Key: key, Utilization: v})
		}
	}
	if len(windows) == 0 {
		switch {
		case planHeader.MatchString(text.String()):
			return Usage{Unavailable: CLINoReading, Detail: "claude -p /usage: a plan's report without its rows (its own reading failed)"}
		case sessionTotals.MatchString(text.String()):
			return Usage{Unavailable: CLISessionOnly, Detail: "claude -p /usage: only this call's own totals, no plan windows (its sign-in here does not read the plan)"}
		}
		return Usage{Unavailable: CLISessionOnly, Detail: "claude -p /usage: no plan windows in its answer"}
	}
	byKey := map[string]*windowJSON{}
	for _, w := range windows {
		if _, ok := byKey[w.Key]; !ok {
			byKey[w.Key] = &windowJSON{Utilization: w.Utilization, ResetsAt: w.ResetsAt}
		}
	}
	return Usage{Windows: windowsFromResponse(byKey)}
}

// windowKey names a usage row as the usage endpoint's windows are named
// (what the apps and the scheduler read): the session row five_hour, the
// week's seven_day, a model's week seven_day_<model>; a row of another
// kind is left out.
func (r cliLimit) windowKey() string {
	switch r.Kind {
	case "session":
		return "five_hour"
	case "weekly_all":
		return "seven_day"
	case "weekly_scoped":
		if r.Scope != nil && r.Scope.Model != nil {
			return modelWeekKey(r.Scope.Model.DisplayName)
		}
	}
	return ""
}

// textWeekKey names a week row of the text by what it is for: "all models",
// "Sonnet only", "Opus".
func textWeekKey(what string) string {
	if strings.EqualFold(strings.TrimSpace(what), "all models") {
		return "seven_day"
	}
	return modelWeekKey(strings.TrimSuffix(strings.TrimSpace(what), " only"))
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

func modelWeekKey(name string) string {
	slug := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(name), "_"), "_")
	if slug == "" {
		return ""
	}
	return "seven_day_" + slug
}
