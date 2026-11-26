package provider

import (
	"context"
	"time"
)

// Plan usage asked of a coding agent when it is checked (工单 #143, after
// upstream 1c862bde): Claude Code's get_usage control request and Codex's
// account/rateLimits/read, each on the process the check already starts for
// the agent's models, so no turn and no extra process. It is the same
// reading as the Claude plan usage of 工单 #86 (docs/claude-usage.md): the
// machine takes it into that one reading (machine/claudeusage.go), never a
// second store of its own.

// UsageWindow is one rate-limit window's reading: Key names it as the
// Claude usage endpoint does ("five_hour", "seven_day", a model's own week
// "seven_day_<model>"), Utilization is how full it is (0 to 100), ResetsAt
// when it starts again (zero: not said).
type UsageWindow struct {
	Key         string    `json:"key"`
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

// PlanUsage is what an agent said of its account's plan usage when its
// check asked (Info.PlanUsage). Unavailable is empty when Windows came,
// else why none did: UsageNone (it answered, with no window: an API key,
// a plan without limits), UsageFailed (it did not answer it). Detail says
// it in one line; never a token.
type PlanUsage struct {
	Plan        string // the plan, when the agent says it ("max", "pro")
	Windows     []UsageWindow
	At          time.Time
	Unavailable string
	Detail      string
}

// Why an asked agent gave no plan usage (PlanUsage.Unavailable).
const (
	UsageNone   = "check_no_windows"
	UsageFailed = "check_failed"
)

type askUsageKey struct{}

// AskUsage marks ctx as a check that may also ask the agent its plan usage
// (工单 #143): the machine reads plan usage (aided's ClaudeUsage switch).
// It asks only when the check starts the agent anyway (OnPurpose) and the
// agent is signed in.
func AskUsage(ctx context.Context) context.Context {
	return context.WithValue(ctx, askUsageKey{}, true)
}

// AsksUsage: the check may ask the agent its plan usage (AskUsage).
func AsksUsage(ctx context.Context) bool {
	on, _ := ctx.Value(askUsageKey{}).(bool)
	return on
}
