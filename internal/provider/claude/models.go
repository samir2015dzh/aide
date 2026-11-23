package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"

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

// listModels asks Claude Code at bin for its models, in an empty folder so
// no project's settings count. own is the specific model it runs unless
// told ("" when it did not say). None when it does not answer so (one
// older than its models list).
func listModels(ctx context.Context, env []string, bin string) (models []provider.Model, own string) {
	dir, err := os.MkdirTemp("", "aide-claude-models-")
	if err != nil {
		return nil, ""
	}
	defer os.RemoveAll(dir)
	cmd := provider.Command(ctx, bin, modelsArgs...)
	cmd.Env, cmd.Dir, cmd.Stdin = env, dir, bytes.NewReader([]byte(modelsRequest))
	out, _ := cmd.Output() // it exits once stdin ends; what it answered counts
	return parseModels(out)
}

// parseModels reads the answer to the initialize request out of Claude
// Code's stream-json lines.
func parseModels(out []byte) (models []provider.Model, own string) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
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
		if json.Unmarshal(sc.Bytes(), &l) != nil || l.Type != "control_response" || l.Response.RequestID != "aide-models" {
			continue
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
		return models, own
	}
	return nil, ""
}
