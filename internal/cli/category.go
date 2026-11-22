package cli

import (
	"regexp"
	"strings"
)

// The two kinds of agent (所有者 2026-11-22, 工单 #26/#49): a personal agent
// (Muse, Grok Bot) is the owner's and follows the account; a coding agent on
// a machine (DSH, Claude Desktop, Claude Code, Codex, WorkBuddy) follows that
// machine. The same rule as the apps' (@aide/core grantCategory), from the
// name an agent was given: "DSH on work-pc" or "client@work-pc" is a coding
// agent on the machine work-pc, a coding agent's name alone (Codex) is one
// too, anything else is personal.

const (
	categoryPersonal = "personal"
	categoryLocal    = "local"
)

var (
	onMachine   = regexp.MustCompile(`(?i)^(.+?)\s+on\s+(.+)$`)
	atMachine   = regexp.MustCompile(`^[^@\s]+@(\S+)$`)
	codingAgent = regexp.MustCompile(`(?i)\b(dsh|deepseek|claude code|codex|workbuddy|codebuddy|gemini cli|copilot cli|opencode|qwen code|kimi cli|aide cli)\b`)
	// The personal agents the apps know by name (@aide/ui personalAgents).
	personalNames = []string{"grok bot", "muse", "chatgpt", "claude", "openclaw", "cursor"}
)

// agentCategory is the kind of agent named name, and the machine (of
// machines) it runs on when the name says.
func agentCategory(name string, machines []string) (category, machine string) {
	tail := ""
	if m := onMachine.FindStringSubmatch(name); m != nil {
		tail = m[2]
	} else if m := atMachine.FindStringSubmatch(name); m != nil {
		tail = m[1]
	}
	for _, n := range machines {
		if tail != "" && strings.EqualFold(strings.TrimSpace(tail), n) {
			return categoryLocal, n
		}
	}
	base := strings.ToLower(strings.TrimSpace(regexp.MustCompile(`\s*\([^()]*\)$`).ReplaceAllString(name, "")))
	for _, p := range personalNames {
		if base == p {
			return categoryPersonal, ""
		}
	}
	if codingAgent.MatchString(name) || strings.HasPrefix(strings.ToLower(name), "client@") {
		return categoryLocal, strings.TrimSpace(tail)
	}
	return categoryPersonal, ""
}
