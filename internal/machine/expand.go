package machine

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// Prompt expansion (docs/cli.md "Session references and macros"): what a
// client sends to start a session or into one may name earlier sessions
// (#s_…) and start with a macro (!plan, !implement). The machine expands
// both before the turn, so every client means the same by them; the
// expanded text is what the agent gets and what the timeline records as
// the user message.

// macros are the work-order macros, by name: the text that replaces the
// "!name" at the start of a prompt, followed by the rest of the prompt.
var macros = map[string]string{
	"plan": "[Aide !plan] Plan only. Do not create, edit or delete any file and do not run commands that change the repository. " +
		"Read what you need, then reply with a numbered plan: the steps, the files each touches, and how each will be verified. Then stop and wait.",
	"implement": "[Aide !implement] Implement the task below. Change the files it needs, keep the change as small as the task allows, " +
		"run the relevant tests, and end with a short summary of what changed and how it was verified.",
}

const (
	maxSessionRefs    = 5
	maxRefResultChars = 4000
)

var (
	// #s_<id>: a session ID after "#", at the start or after a space or an
	// opening bracket. "@" stays the agents' recipient sigil.
	sessionRefRE = regexp.MustCompile(`(?:^|[\s(\[])#(s_[0-9A-Za-z_]+)`)
	macroRE      = regexp.MustCompile(`^!([a-z][a-z-]*)(?:\s|$)`)
)

// expandPrompt expands text for p: the macro it starts with, and the
// sessions it references, which p must be able to read.
func (s *Sessions) expandPrompt(p *Principal, text string) (string, error) {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if m := macroRE.FindStringSubmatch(trimmed); m != nil {
		body, ok := macros[m[1]]
		if !ok {
			names := make([]string, 0, len(macros))
			for n := range macros {
				names = append(names, "!"+n)
			}
			slices.Sort(names)
			return "", cerr(connect.CodeInvalidArgument, "unknown macro !%s (known: %s)", m[1], strings.Join(names, ", "))
		}
		rest := strings.TrimSpace(trimmed[len(m[0]):])
		text = body
		if rest != "" {
			text += "\n\n" + rest
		}
	}
	var ids []string
	for _, m := range sessionRefRE.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(ids, m[1]) {
			ids = append(ids, m[1])
		}
	}
	if len(ids) == 0 {
		return text, nil
	}
	if len(ids) > maxSessionRefs {
		return "", cerr(connect.CodeInvalidArgument, "%d sessions referenced; at most %d per message", len(ids), maxSessionRefs)
	}
	var b strings.Builder
	b.WriteString(text)
	for _, id := range ids {
		l, r, err := s.find(p, id, false)
		if err != nil {
			return "", cerr(connect.CodeNotFound, "referenced session #%s not found, or not visible to you", id)
		}
		if l != nil { // a live row changes under l.mu
			l.mu.Lock()
			c := *r
			c.Progress = proto.Clone(r.Progress).(*sessionv1.SessionProgress)
			l.mu.Unlock()
			r = &c
		}
		res := r.Result
		cut := ""
		if n := len([]rune(res)); n > maxRefResultChars {
			res = string([]rune(res)[:maxRefResultChars])
			cut = fmt.Sprintf("\n[truncated: %d of %d characters shown]", maxRefResultChars, n)
		}
		fmt.Fprintf(&b, "\n\n[Aide] Referenced session #%s\nTitle: %s\nAgent: %s, project: %s, status: %s\n", r.ID, r.Title, r.Agent, r.ProjectID, phaseOf(r.Status))
		if d := r.Progress.GetDiffStat(); d.GetFilesChanged() > 0 {
			fmt.Fprintf(&b, "Changes: branch %s, %d files changed, +%d −%d\n", r.Branch, d.FilesChanged, d.Insertions, d.Deletions)
		}
		if res == "" {
			b.WriteString("Final result: (none yet)\n")
		} else {
			fmt.Fprintf(&b, "Final result:\n%s%s\n", res, cut)
		}
		fmt.Fprintf(&b, "[Aide] End of session #%s", r.ID)
	}
	return b.String(), nil
}
