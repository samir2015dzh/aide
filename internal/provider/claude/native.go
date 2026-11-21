package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// The Claude Code sessions the Claude desktop app keeps, read only, so the
// owner sees in Aide those he has there (docs/claude-sessions.md). The
// app lists each in its own index, one record a session
// (<index>/<account>/<workspace>/local_<id>.json: its title, folder and
// times); Claude Code writes the conversation, in plain JSON lines, to
// <config>/projects/<folder>/<Claude Code's id>.jsonl, config being
// $CLAUDE_CONFIG_DIR or ~/.claude. Where the app keeps its index is found
// as desktopIndexes finds it, never by the names seen on one computer.

// ErrNoSession: the app lists no session by that id.
var ErrNoSession = errors.New("the Claude desktop app has no such session")

// Places is where a machine's Claude sessions are read from.
type Places struct {
	Indexes []string // the app's claude-code-sessions folders
	Config  string   // Claude Code's configuration folder
}

// PlacesFor finds them for a process with environment env (APPDATA,
// LOCALAPPDATA and CLAUDE_CONFIG_DIR as there; the home folder this
// process's).
func PlacesFor(env []string) Places {
	get := func(name string) string {
		v := ""
		for _, kv := range env {
			if k, val, _ := strings.Cut(kv, "="); strings.EqualFold(k, name) {
				v = val
			}
		}
		return v
	}
	home, _ := os.UserHomeDir()
	config := get("CLAUDE_CONFIG_DIR")
	if strings.TrimSpace(config) == "" {
		config = filepath.Join(home, ".claude")
	}
	var idx []string
	for _, d := range desktopIndexes(home, get("APPDATA"), get("LOCALAPPDATA")) {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			idx = append(idx, d)
		}
	}
	return Places{Indexes: idx, Config: config}
}

// nativeRecord is the part of the app's record read.
type nativeRecord struct {
	desktopRecord
	Model           string `json:"model"`
	PostTurnSummary struct {
		StatusCategory string `json:"status_category"`
	} `json:"postTurnSummary"`
	GitAnchors []struct {
		GitRoot string `json:"gitRoot"`
	} `json:"gitAnchors"`
}

// records are the app's records in the index folders, one per session (a
// session listed under two accounts: its newest), with whether each is
// archived; deleted ones (a deleted_<id> beside) are not.
func records(indexes []string) []nativeRecord {
	byID := map[string]nativeRecord{}
	archived := map[string]bool{}
	for _, idx := range indexes {
		dirs, _ := filepath.Glob(filepath.Join(idx, "*", "*"))
		for _, dir := range dirs {
			for _, a := range archivedIndex(dir) {
				archived[a] = true
			}
			files, _ := filepath.Glob(filepath.Join(dir, "local_*.json"))
			for _, f := range files {
				b, err := os.ReadFile(f)
				var r nativeRecord
				if err != nil || json.Unmarshal(b, &r) != nil || r.SessionID == "" {
					continue
				}
				id := strings.TrimPrefix(r.SessionID, "local_")
				if _, err := os.Stat(filepath.Join(dir, "deleted_"+id)); err == nil {
					continue
				}
				if old, ok := byID[r.SessionID]; !ok || r.LastActivityAt > old.LastActivityAt {
					byID[r.SessionID] = r
				}
			}
		}
	}
	out := make([]nativeRecord, 0, len(byID))
	for _, r := range byID {
		r.IsArchived = r.IsArchived || archived[r.SessionID] || archived[strings.TrimPrefix(r.SessionID, "local_")]
		out = append(out, r)
	}
	return out
}

// archivedIndex is the sessions archived-sessions.idx in dir names:
// {"v":1,"archived":["local_…", …]}.
func archivedIndex(dir string) []string {
	b, err := os.ReadFile(filepath.Join(dir, "archived-sessions.idx"))
	if err != nil {
		return nil
	}
	var a struct{ Archived []string }
	json.Unmarshal(b, &a)
	return a.Archived
}

// List lists the sessions the app lists, newest activity first, but those
// skip names (by Claude Code's id, and the folder it works in).
func List(p Places, skip func(cliID, cwd string) bool) []*machinev1.ClaudeSession {
	var out []*machinev1.ClaudeSession
	repos := map[string]string{}
	for _, r := range records(p.Indexes) {
		if skip != nil && skip(r.CLISessionID, r.Cwd) {
			continue
		}
		s := describeNative(r)
		dir := workFolder(r)
		if _, ok := repos[dir]; !ok {
			repos[dir] = RepoOf(dir)
		}
		s.Repo = repos[dir]
		if transcriptPath(p.Config, r.CLISessionID) == "" {
			s.Error = "no Claude Code transcript of it on this machine"
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b *machinev1.ClaudeSession) int {
		if c := b.LastActivityAt.AsTime().Compare(a.LastActivityAt.AsTime()); c != 0 {
			return c
		}
		return strings.Compare(a.SessionId, b.SessionId)
	})
	return out
}

// workFolder is the folder the session works in: its cwd, else where it
// started, else its git checkout.
func workFolder(r nativeRecord) string {
	if r.Cwd != "" {
		return r.Cwd
	}
	if r.OriginCwd != "" {
		return r.OriginCwd
	}
	if len(r.GitAnchors) > 0 {
		return r.GitAnchors[0].GitRoot
	}
	return ""
}

func describeNative(r nativeRecord) *machinev1.ClaudeSession {
	ts := func(ms int64) *timestamppb.Timestamp {
		if ms <= 0 {
			return nil
		}
		return timestamppb.New(time.UnixMilli(ms))
	}
	return &machinev1.ClaudeSession{SessionId: r.SessionID, CliSessionId: r.CLISessionID, Title: r.Title, Cwd: workFolder(r),
		CreatedAt: ts(r.CreatedAt), LastActivityAt: ts(max(r.LastActivityAt, r.CreatedAt)), Archived: r.IsArchived, Model: r.Model,
		Status: r.PostTurnSummary.StatusCategory}
}

// transcriptPath is Claude Code's transcript of the session: the
// <id>.jsonl in one of its projects' folders (named after the folder it
// works in, a name not worked out here: each is looked in).
func transcriptPath(config, cliID string) string {
	if cliID == "" || strings.ContainsAny(cliID, `/\`) || cliID == "." || cliID == ".." {
		return ""
	}
	paths, _ := filepath.Glob(filepath.Join(config, "projects", "*", cliID+".jsonl"))
	var newest string
	var at time.Time
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.ModTime().After(at) {
			newest, at = p, st.ModTime()
		}
	}
	return newest
}

// Read reads one session the app lists (by the app's id or Claude Code's),
// and its conversation as session events (seq: the transcript's line
// number times 1000, plus the block's place in the line).
func Read(p Places, id string, skip func(cliID, cwd string) bool) (*machinev1.ClaudeSession, []*sessionv1.SessionEvent, error) {
	for _, r := range records(p.Indexes) {
		if r.SessionID != id && r.CLISessionID != id || skip != nil && skip(r.CLISessionID, r.Cwd) {
			continue
		}
		s := describeNative(r)
		s.Repo = RepoOf(workFolder(r))
		path := transcriptPath(p.Config, r.CLISessionID)
		if path == "" {
			s.Error = "no Claude Code transcript of it on this machine"
			return s, nil, nil
		}
		evs, err := readTranscript(path)
		if err != nil {
			s.Error = err.Error()
			return s, nil, nil
		}
		return s, evs, nil
	}
	return nil, nil, ErrNoSession
}

// transcriptLine is the part of a line of Claude Code's transcript read.
type transcriptLine struct {
	Type        string          `json:"type"`
	IsSidechain bool            `json:"isSidechain"`
	IsMeta      bool            `json:"isMeta"`
	Timestamp   string          `json:"timestamp"`
	Message     json.RawMessage `json:"message"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// blocks is a message's content: a string is one text block.
func blocks(msg json.RawMessage) []contentBlock {
	var m struct{ Content json.RawMessage }
	if json.Unmarshal(msg, &m) != nil {
		return nil
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return []contentBlock{{Type: "text", Text: s}}
	}
	var bs []contentBlock
	json.Unmarshal(m.Content, &bs)
	return bs
}

// resultText is a tool result's text (a string, or text blocks).
func resultText(c json.RawMessage) string {
	var s string
	if json.Unmarshal(c, &s) == nil {
		return s
	}
	var bs []contentBlock
	json.Unmarshal(c, &bs)
	var parts []string
	for _, b := range bs {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// harnessText: what Claude Code adds as the user's that the owner did not
// write (a command's output, a reminder), shown as nothing.
var harnessText = regexp.MustCompile(`^\s*<(command-name|command-message|local-command-stdout|local-command-stderr|system-reminder|local-command-caveat)>`)

// readTranscript is the conversation in a transcript: the owner's
// prompts, Claude's replies, tool calls and results; not a subagent's
// side conversation, nor what Claude Code adds itself.
func readTranscript(path string) ([]*sessionv1.SessionEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rd := bufio.NewReaderSize(f, 1<<20)
	var out []*sessionv1.SessionEvent
	for n := uint64(1); ; n++ {
		line, err := rd.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			out = append(out, lineEvents(n, line)...)
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// lineEvents is what one line of a transcript shows, numbered after it
// (a line can carry several blocks; each gets a seq of its own, the
// line's times 1000 plus its place).
func lineEvents(n uint64, line []byte) []*sessionv1.SessionEvent {
	var l transcriptLine
	if json.Unmarshal(line, &l) != nil || l.IsSidechain || l.IsMeta || l.Type != "user" && l.Type != "assistant" {
		return nil
	}
	var ts *timestamppb.Timestamp
	if t, err := time.Parse(time.RFC3339Nano, l.Timestamp); err == nil {
		ts = timestamppb.New(t)
	}
	var out []*sessionv1.SessionEvent
	add := func(k any) {
		ev := &sessionv1.SessionEvent{Seq: n*1000 + uint64(len(out)), Ts: ts}
		switch k := k.(type) {
		case *sessionv1.UserMessage:
			ev.Kind = &sessionv1.SessionEvent_UserMessage{UserMessage: k}
		case *sessionv1.AgentMessage:
			ev.Kind = &sessionv1.SessionEvent_AgentMessage{AgentMessage: k}
		case *sessionv1.ToolCall:
			ev.Kind = &sessionv1.SessionEvent_ToolCall{ToolCall: k}
		case *sessionv1.ToolResult:
			ev.Kind = &sessionv1.SessionEvent_ToolResult{ToolResult: k}
		}
		out = append(out, ev)
	}
	for _, b := range blocks(l.Message) {
		switch {
		case l.Type == "user" && b.Type == "text":
			if strings.TrimSpace(b.Text) != "" && !harnessText.MatchString(b.Text) {
				add(&sessionv1.UserMessage{Text: b.Text})
			}
		case l.Type == "user" && b.Type == "image":
			add(&sessionv1.UserMessage{Text: "[image]"})
		case l.Type == "user" && b.Type == "tool_result":
			text, cut := provider.Truncate(resultText(b.Content), 16<<10)
			add(&sessionv1.ToolResult{CallId: b.ToolUseID, Summary: text, IsError: b.IsError, Truncated: cut})
		case l.Type == "assistant" && b.Type == "text":
			if strings.TrimSpace(b.Text) != "" {
				add(&sessionv1.AgentMessage{Text: b.Text})
			}
		case l.Type == "assistant" && b.Type == "tool_use":
			args, _ := provider.Truncate(string(b.Input), 500)
			add(&sessionv1.ToolCall{CallId: b.ID, Kind: string(provider.ToolOther), Name: b.Name, Summary: args, Title: provider.ToolTitle(b.Name, b.Input)})
		}
	}
	return out
}

// RepoOf is "owner/repo" of the GitHub repository dir is a checkout of, as
// the checkout's own origin remote says (its .git folder, or the .git file
// of a worktree, found from dir upwards); "" when none.
func RepoOf(dir string) string {
	if dir == "" {
		return ""
	}
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		if gitDir := gitDirOf(filepath.Join(d, ".git")); gitDir != "" {
			return githubRepo(originURL(gitDir))
		}
		if filepath.Dir(d) == d {
			return ""
		}
	}
}

// gitDirOf is the repository's own git folder for a .git at p (a folder,
// or a worktree's file "gitdir: …" whose commondir leads to it); "" when
// p is neither.
func gitDirOf(p string) string {
	st, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if st.IsDir() {
		return p
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	g, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return ""
	}
	g = strings.TrimSpace(g)
	if !filepath.IsAbs(g) {
		g = filepath.Join(filepath.Dir(p), g)
	}
	if c, err := os.ReadFile(filepath.Join(g, "commondir")); err == nil {
		common := strings.TrimSpace(string(c))
		if !filepath.IsAbs(common) {
			common = filepath.Join(g, common)
		}
		return filepath.Clean(common)
	}
	return g
}

// originURL is remote "origin"'s url in the git folder's config.
func originURL(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "config"))
	if err != nil {
		return ""
	}
	in := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			in = remoteOrigin.MatchString(line)
			continue
		}
		if k, v, ok := strings.Cut(line, "="); in && ok && strings.EqualFold(strings.TrimSpace(k), "url") {
			return strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	return ""
}

var remoteOrigin = regexp.MustCompile(`^\[\s*remote\s+"origin"\s*\]$`)

// githubURL: https://github.com/o/r(.git), git@github.com:o/r(.git),
// ssh://git@github.com/o/r, git://github.com/o/r; a user or token before
// the host is allowed.
var githubURL = regexp.MustCompile(`^(?:(?:https?|ssh|git)://(?:[^@/]+@)?github\.com(?::\d+)?/|(?:[^@/]+@)?github\.com:)([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// githubRepo is "owner/repo" of a GitHub remote url; "" for another host.
func githubRepo(url string) string {
	m := githubURL.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return ""
	}
	return m[1] + "/" + m[2]
}
