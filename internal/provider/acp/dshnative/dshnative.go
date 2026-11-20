// Package dshnative reads the sessions DeepSeek Harness (DSH) keeps in its
// home, so the owner can see those Aide did not start: the ones they
// opened in DSH Desktop, or another tool did. It only reads DSH's files,
// never writes in its home, and never takes a session's write lock: it only
// looks whether a process holds it.
//
// What it knows of DSH's files comes from its npm packages (0.2.2-rc.1 to
// rc.4): @deepseek-ai/dsh-home-paths, dsh-session,
// dsh-session-persistence-jsonl, dsh-session-projection-cache,
// dsh-session-title and dsh-workspace.
package dshnative

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// ErrNotFound: DSH has no session by that id.
var ErrNotFound = errors.New("no such DSH session")

// Home is DSH's home for a process with environment env, as DSH finds it:
// $DSH_HOME unless blank ("~" is the home folder), else ~/.dsh
// (%USERPROFILE%\.dsh on Windows).
func Home(env []string) (string, error) {
	h := ""
	for _, kv := range env {
		name, v, _ := strings.Cut(kv, "=")
		if name == "DSH_HOME" || runtime.GOOS == "windows" && strings.EqualFold(name, "DSH_HOME") {
			h = v
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch {
	case strings.TrimSpace(h) == "":
		h = filepath.Join(home, ".dsh")
	case h == "~":
		h = home
	case strings.HasPrefix(h, "~/") || strings.HasPrefix(h, `~\`):
		h = filepath.Join(home, h[2:])
	}
	return filepath.Abs(h)
}

// List lists the sessions in DSH's home, newest activity first, but its
// subagents' and those skip names (by DSH's id, and the folder it works
// in).
func List(home string, skip func(id, cwd string) bool) ([]*machinev1.DSHSession, error) {
	root := filepath.Join(home, "sessions")
	projects, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	archived := archivedIDs(home)
	held := lockProbe()
	var out []*machinev1.DSHSession
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		dirs, err := os.ReadDir(filepath.Join(root, p.Name()))
		if err != nil {
			continue
		}
		for _, d := range dirs {
			dir := filepath.Join(root, p.Name(), d.Name())
			sum, ok := summarize(dir)
			if !ok || sum.header.Origin == "subagent" || skip != nil && skip(sum.header.ID, sum.header.Cwd) {
				continue
			}
			out = append(out, describe(home, dir, sum, archived, held))
		}
	}
	slices.SortFunc(out, func(a, b *machinev1.DSHSession) int {
		if c := b.LastActivityAt.AsTime().Compare(a.LastActivityAt.AsTime()); c != 0 {
			return c
		}
		return strings.Compare(a.SessionId, b.SessionId)
	})
	return out, nil
}

// Read reads one session: what List tells of it, and its conversation as
// session events (seq: DSH's own, plus one).
func Read(home, id string) (*machinev1.DSHSession, []*sessionv1.SessionEvent, error) {
	if id == "" {
		return nil, nil, ErrNotFound
	}
	root := filepath.Join(home, "sessions")
	projects, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	for _, p := range projects {
		dir := filepath.Join(root, p.Name(), encodeSegment(id))
		path, err := logFile(dir)
		if err != nil || path == "" {
			continue
		}
		log, err := parse(path)
		if err != nil {
			return nil, nil, err
		}
		if log.header.ID != id {
			continue
		}
		s := describe(home, dir, log.summary(), archivedIDs(home), lockProbe())
		if log.problem != "" {
			return s, nil, nil
		}
		return s, log.transcript(), nil
	}
	return nil, nil, ErrNotFound
}

// summary is what a list shows of a log.
type summary struct {
	header  header
	title   string // the last session/title
	prompt  string // the first line of the first prompt
	last    int64  // the last event's time, Unix ms
	open    bool   // a turn has started and not ended
	problem string
}

func (p *parsed) summary() summary {
	s := summary{header: p.header, last: p.header.CreatedAt, problem: p.problem}
	for _, e := range p.events {
		s.last = max(s.last, e.Time)
		switch e.Type {
		case "turn/start":
			s.open = true
		case "turn/end":
			s.open = false
		case "session/title":
			var t struct{ Title string }
			if json.Unmarshal(e.Data, &t) == nil && t.Title != "" {
				s.title = t.Title
			}
		case "user/message":
			if s.prompt == "" {
				if text, _, ok := userText(e); ok {
					s.prompt, _, _ = strings.Cut(strings.TrimSpace(text), "\n")
				}
			}
		}
	}
	return s
}

// summaries keeps what List read of each log, until the log changes: DSH
// only appends to it, so its size tells.
var summaries sync.Map // path → cachedSummary

type cachedSummary struct {
	size int64
	mod  time.Time
	sum  summary
}

// summarize is the summary of the session in dir; false when dir holds no
// session DSH could read either.
func summarize(dir string) (summary, bool) {
	path, err := logFile(dir)
	if err != nil || path == "" {
		return summary{}, false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return summary{}, false
	}
	if c, ok := summaries.Load(path); ok && c.(cachedSummary).size == fi.Size() && c.(cachedSummary).mod.Equal(fi.ModTime()) {
		return c.(cachedSummary).sum, true
	}
	log, err := parse(path)
	if err != nil {
		return summary{}, false
	}
	sum := log.summary()
	summaries.Store(path, cachedSummary{fi.Size(), fi.ModTime(), sum})
	return sum, true
}

// describe is the session in dir as the machine API tells it.
func describe(home, dir string, s summary, archived map[string]bool, held func(dir string) (held, known bool)) *machinev1.DSHSession {
	out := &machinev1.DSHSession{SessionId: s.header.ID, Title: s.title, Cwd: s.header.Cwd, CreatedAt: timestamppb.New(time.UnixMilli(s.header.CreatedAt)),
		LastActivityAt: timestamppb.New(time.UnixMilli(s.last)), TurnOpen: s.open, Archived: archived[s.header.ID], AgentPreset: s.header.AgentPreset, Error: s.problem}
	if out.Title == "" {
		out.Title = cachedTitle(home, s.header.ID)
	}
	if out.Title == "" {
		out.Title = s.prompt
	}
	h, known := held(dir)
	switch {
	case !known:
	case !h:
		out.State = machinev1.DSHSessionState_DSH_SESSION_STATE_ENDED
	case s.open:
		out.State = machinev1.DSHSessionState_DSH_SESSION_STATE_RUNNING
	default:
		out.State = machinev1.DSHSessionState_DSH_SESSION_STATE_IDLE
	}
	return out
}

// storageKey is what DSH's JSON storage takes as a record's file name.
var storageKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// cachedTitle is the title DSH's projection cache holds for a session, for
// one whose log can't be read: storages/session_projcache/sessions/<id>.json,
// {"version": n, "record": {"rows": {"title": {"val": "..."}}}}.
func cachedTitle(home, id string) string {
	if !storageKey.MatchString(id) {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, "storages", "session_projcache", "sessions", id+".json"))
	if err != nil {
		return ""
	}
	var c struct {
		Record struct {
			Rows struct {
				Title struct{ Val any }
			}
		}
	}
	json.Unmarshal(b, &c)
	t, _ := c.Record.Rows.Title.Val.(string)
	return t
}

// archivedIDs is the sessions archived in DSH Desktop's sidebar: its
// workspace registry, storages/workspace.json, {"global":
// {"archivedSessionIds": [...]}}.
func archivedIDs(home string) map[string]bool {
	b, err := os.ReadFile(filepath.Join(home, "storages", "workspace.json"))
	if err != nil {
		return nil
	}
	var w struct {
		Global struct {
			ArchivedSessionIDs []string `json:"archivedSessionIds"`
		}
	}
	json.Unmarshal(b, &w)
	out := map[string]bool{}
	for _, id := range w.Global.ArchivedSessionIDs {
		out[id] = true
	}
	return out
}

// lockPath is where a session in dir has its write lock: a file DSH
// flock(2)s on POSIX, and on Windows the path its semaphore is named by.
func lockPath(dir string) string { return filepath.Join(dir, "session.lock") }
