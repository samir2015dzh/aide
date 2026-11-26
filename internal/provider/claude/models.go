package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

// Claude Code's models on this machine (工单 #99): it takes aliases
// ("opus", "sonnet", …) and resolves each to a specific model for the
// account it is signed in to. It says which, and which model it runs
// unless told, to the initialize control request its SDK sends
// (supportedModels): each model's value, display name, resolvedModel and
// reasoning efforts. Nothing goes to a model for it, and no session is
// kept.

// modelsArgs start Claude Code to answer one initialize request: no session
// saved, no MCP server started, no hook run (the owner's SessionStart hooks
// run in every session that starts, this is none).
var modelsArgs = []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
	"--no-session-persistence", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--settings", `{"disableAllHooks":true}`}

const modelsRequest = `{"type":"control_request","request_id":"aide-models","request":{"subtype":"initialize"}}` + "\n"

// usageRequest asks the same process, once it answered initialize, for the
// account's plan usage (工单 #143, after upstream 1c862bde): get_usage,
// skip_behaviors so it reads only the plan's limits, not every transcript.
const usageRequest = `{"type":"control_request","request_id":"aide-usage","request":{"subtype":"get_usage","skip_behaviors":true}}` + "\n"

// listModels asks Claude Code at bin for its models, in an empty folder so
// no project's settings count. own is the specific model it runs unless
// told ("" when it did not say). None when it does not answer so (one
// older than its models list). withUsage: it is then asked its plan usage
// too (get_usage), before its input ends; usage is nil when not asked.
func listModels(ctx context.Context, env []string, bin string, withUsage bool) (models []provider.Model, own string, usage *provider.PlanUsage) {
	dir, err := os.MkdirTemp("", "aide-claude-models-")
	if err != nil {
		return nil, "", failedUsage(withUsage, "claude get_usage: "+err.Error())
	}
	defer os.RemoveAll(dir)
	// It answers within a second or so; one that does not (stuck starting,
	// or not Claude Code) is stopped with all it started, so the check goes
	// on without its models and nothing is left running. Its usage, read
	// from Anthropic, gets a little longer.
	wait := modelsWait
	if withUsage {
		wait += usageWait
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	cmd := provider.Command(ctx, bin, modelsArgs...)
	cmd.Env, cmd.Dir = env, dir
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, "", failedUsage(withUsage, "claude get_usage: "+err.Error())
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, "", failedUsage(withUsage, "claude get_usage: "+err.Error())
	}
	waitProc, err := provider.StartInGroup(cmd)
	if err != nil {
		return nil, "", failedUsage(withUsage, "claude get_usage: "+err.Error())
	}
	defer waitProc() // it exits once its input ends
	if _, err := io.WriteString(in, modelsRequest); err != nil || !withUsage {
		in.Close() // only the models: its input ends with the one request
	}
	lines := make(chan []byte)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(out)
		sc.Buffer(nil, 16<<20)
		for sc.Scan() {
			select {
			case lines <- bytes.Clone(sc.Bytes()):
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() {
		in.Close()
		for range lines { // what it writes until it exits
		}
	}()
	// Its usage gets usageWait from its models' answer: one that does not
	// answer get_usage (a version without it) is not waited on past that.
	var usageBy <-chan time.Time
	gotModels := false
	for {
		select {
		case line, open := <-lines:
			if !open {
				if !gotModels {
					models, own = nil, ""
				}
				return models, own, failedUsage(withUsage, "claude get_usage: no answer (this version may not take it)")
			}
			if !gotModels && bytes.Contains(line, []byte(`"aide-models"`)) {
				if ms, o, ok := modelsFrom(line); ok {
					models, own, gotModels = ms, o, true
					if !withUsage {
						continue
					}
					if _, err := io.WriteString(in, usageRequest); err != nil {
						return models, own, failedUsage(true, "claude get_usage: "+err.Error())
					}
					usageBy = time.After(usageWait)
				}
				continue
			}
			if withUsage && gotModels && bytes.Contains(line, []byte(`"aide-usage"`)) {
				if u, ok := usageFrom(line); ok {
					u.At = time.Now()
					return models, own, u
				}
			}
		case <-usageBy:
			return models, own, failedUsage(true, "claude get_usage: no answer in "+usageWait.String())
		}
	}
}

// modelsWait bounds asking Claude Code for its models; usageWait is what
// asking its usage adds.
var modelsWait, usageWait = 8 * time.Second, 10 * time.Second

func failedUsage(asked bool, detail string) *provider.PlanUsage {
	if !asked {
		return nil
	}
	return &provider.PlanUsage{At: time.Now(), Unavailable: provider.UsageFailed, Detail: detail}
}

// usageFrom reads get_usage's answer (Claude Code 2.1.330,
// testdata/claude-2.1.330-get-usage.json). Its rate_limits are the usage
// endpoint's answer as it came (docs/claude-usage.md §3), with the plan's
// rows in limits as /usage's structured twin has them (cliusage.go), so it
// reads into the same windows as the other sources of 工单 #86: the rows
// first (they name a model's own week), else the endpoint's windows. ok:
// it is the answer to get_usage (an error answer is one too, no windows).
func usageFrom(line []byte) (u *provider.PlanUsage, ok bool) {
	var a struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Error     string `json:"error"`
			Response  struct {
				SubscriptionType    *string         `json:"subscription_type"`
				RateLimitsAvailable bool            `json:"rate_limits_available"`
				RateLimits          json.RawMessage `json:"rate_limits"`
			} `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(line, &a) != nil || a.Type != "control_response" || a.Response.RequestID != "aide-usage" {
		return nil, false
	}
	r := a.Response.Response
	if a.Response.Subtype != "success" {
		return &provider.PlanUsage{Unavailable: provider.UsageFailed, Detail: "claude get_usage: " + strings.TrimSpace("refused "+a.Response.Error)}, true
	}
	u = &provider.PlanUsage{}
	if r.SubscriptionType != nil {
		u.Plan = *r.SubscriptionType
	}
	if r.RateLimitsAvailable {
		var rows struct {
			Limits []cliLimit `json:"limits"`
		}
		if json.Unmarshal(r.RateLimits, &rows) == nil {
			u.Windows = windowsFromRows(rows.Limits)
		}
		if len(u.Windows) == 0 {
			u.Windows, _ = windowsFromBody(r.RateLimits)
		}
	}
	if len(u.Windows) == 0 {
		u.Unavailable, u.Detail = provider.UsageNone, "claude get_usage: no plan windows (not a plan sign-in)"
	}
	return u, true
}

// parseModels reads the answer to the initialize request out of Claude
// Code's stream-json lines.
func parseModels(out []byte) (models []provider.Model, own string) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		if ms, o, ok := modelsFrom(sc.Bytes()); ok {
			return ms, o
		}
	}
	return nil, ""
}

// modelsFrom reads one line as the answer to the initialize request; ok: it
// is that answer.
func modelsFrom(line []byte) (models []provider.Model, own string, ok bool) {
	var l struct {
		Type     string `json:"type"`
		Response struct {
			RequestID string `json:"request_id"`
			Response  struct {
				Models []struct {
					Value           string   `json:"value"`
					DisplayName     string   `json:"displayName"`
					ResolvedModel   string   `json:"resolvedModel"`
					SupportsEffort  bool     `json:"supportsEffort"`
					SupportedEffort []string `json:"supportedEffortLevels"`
				} `json:"models"`
			} `json:"response"`
		} `json:"response"`
	}
	if json.Unmarshal(line, &l) != nil || l.Type != "control_response" || l.Response.RequestID != "aide-models" {
		return nil, "", false
	}
	for _, m := range l.Response.Response.Models {
		if m.Value == "default" { // what it runs unless told: no --model
			own = m.ResolvedModel
			continue
		}
		if m.Value == "" {
			continue
		}
		x := provider.Model{ID: m.Value, Name: m.DisplayName, Resolved: m.ResolvedModel}
		if x.Resolved == x.ID {
			x.Resolved = ""
		}
		if m.SupportsEffort {
			x.Efforts = m.SupportedEffort
		}
		models = append(models, x)
	}
	return models, own, true
}
