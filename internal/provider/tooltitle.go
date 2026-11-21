package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ToolTitle is the line a tool call shows by, worked out from its whole
// arguments (a session read from another app's records, where the arguments
// are all there is): the call's own description when it gives one (DSH's
// pwsh and Claude Code's Bash do), else a short natural summary: an edit's
// file and the lines it adds and removes, a write's file and its lines, the
// file or folder read, the command's first line, what is searched for, the
// address fetched. "" when the arguments say nothing of the kind; the apps
// then show the tool's name alone, the raw arguments folded under it.
func ToolTitle(name string, args []byte) string {
	var in map[string]any
	if json.Unmarshal(args, &in) != nil {
		return ""
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if s, ok := in[k].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		return ""
	}
	if d := str("description", "desc"); d != "" {
		return titleClip(titleLine(d))
	}
	path := str("file_path", "filePath", "path", "notebook_path", "file", "filename")
	oldS, hasOld := in["old_string"].(string)
	newS, hasNew := in["new_string"].(string)
	switch {
	case hasOld || hasNew:
		return titleClip(titleJoin(path, titleDelta(titleLines(newS), titleLines(oldS))))
	case edits(in) > 0:
		return titleClip(titleJoin(path, fmt.Sprintf("%d edits", edits(in))))
	}
	// A search names a folder too: what it looks for comes first.
	if p := str("pattern", "query", "q", "glob"); p != "" {
		return titleClip(titleJoin(p, str("path", "dir", "directory")))
	}
	if c, ok := in["content"].(string); ok && path != "" {
		return titleClip(titleJoin(path, fmt.Sprintf("+%d", titleLines(c))))
	}
	if path != "" {
		return titleClip(path)
	}
	if c := str("command", "cmd", "script"); c != "" {
		return titleClip(titleLine(c))
	}
	if u := str("url", "uri"); u != "" {
		return titleClip(u)
	}
	if p := str("prompt", "task", "title"); p != "" {
		return titleClip(titleLine(p))
	}
	return ""
}

// edits is the number of edits a multi-edit call makes.
func edits(in map[string]any) int {
	if e, ok := in["edits"].([]any); ok {
		return len(e)
	}
	return 0
}

func titleLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}

// titleDelta is "+a −r" for a lines added and r removed.
func titleDelta(added, removed int) string {
	return fmt.Sprintf("+%d −%d", added, removed)
}

func titleLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(l)
}

func titleJoin(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " · " + b
}

// titleClip keeps a title to a line's worth (120 characters).
func titleClip(s string) string {
	const max = 120
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}
