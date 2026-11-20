package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samir2015dzh/aide/internal/provider"
)

// The Claude desktop app lists on its Code page only the Claude Code
// sessions it has a record of, one file per session in an index of its own;
// a session Claude Code ran without it (here: every Aide session) has a
// transcript in ~/.claude/projects but no record. Aide writes one for
// each new session, so the owner finds it there too.
//
// Everything assumed about that index is in this file. The format is the
// desktop app's private one, undocumented: it was derived, without a real
// desktop app at hand, from the record the app writes itself when it
// imports a CLI session (/desktop, claude://resume) as reported in
// anthropics/claude-code#83051 (Claude Code 2.1.176 on macOS, 2.1.226 on
// Windows) and #80773 (importCliSession keys it "local_"+cliSessionId,
// desktop 2.9939.2.0 on Windows), and from the tools that write such
// records: xushenjie/claude-session-sync 0.5.1 (desktop 5.1231.2) and
// BasedGPT/claude-code-session-recovery (Windows). Reportedly the app reads
// the index when it starts, so a record shows after its next start, and
// its Microsoft Store (MSIX) build ignores records written from outside.
//
// Claude Code lets a second process resume a session another one runs
// (anthropics/claude-code#75761; #69364: from 2.1.280 a resume is refused
// only while a non-interactive process holds it), so the app and Aide
// could both write one transcript: the owner goes on with a listed session
// in the app only once Aide is done with it, and its title says who
// runs it.

// desktopIndexes are where the desktop app may keep its index:
// <userData>/claude-code-sessions/<account>/<organization>/local_<id>.json,
// userData being Claude (or Claude-3p) under ~/Library/Application Support
// on macOS; on Windows, a claude-code-sessions folder found as its
// claude-code is (beside it, at most two folders down one of windowsRoots).
func desktopIndexes(home, appData, localAppData string) []string {
	dirs := []string{filepath.Join(home, "Library", "Application Support", "Claude", "claude-code-sessions"),
		filepath.Join(home, "Library", "Application Support", "Claude-3p", "claude-code-sessions")}
	for _, root := range windowsRoots(appData, localAppData) {
		dirs = append(dirs, foldersNamed(root, "claude-code-sessions")...)
	}
	return dirs
}

// desktopAccountDir is the <account>/<organization> folder new records go
// to: the one of the record the app wrote last, which is the account it is
// signed in to (an account switch leaves the old folders behind).
func desktopAccountDir(indexes []string) (string, error) {
	var dir string
	var newest time.Time
	for _, idx := range indexes {
		recs, _ := filepath.Glob(filepath.Join(idx, "*", "*", "local_*.json"))
		for _, r := range recs {
			if st, err := os.Stat(r); err == nil && st.ModTime().After(newest) {
				dir, newest = filepath.Dir(r), st.ModTime()
			}
		}
	}
	if dir == "" {
		return "", errors.New("the Claude desktop app has no Claude Code session yet (none in " + strings.Join(indexes, ", ") + ")")
	}
	return dir, nil
}

// desktopRecord is a record as the desktop app writes it on import; its
// timestamps are Unix milliseconds (a null one empties the whole list).
type desktopRecord struct {
	SessionID      string `json:"sessionId"`
	CLISessionID   string `json:"cliSessionId"`
	Cwd            string `json:"cwd"`
	OriginCwd      string `json:"originCwd"`
	LastFocusedAt  int64  `json:"lastFocusedAt"`
	CreatedAt      int64  `json:"createdAt"`
	LastActivityAt int64  `json:"lastActivityAt"`
	IsArchived     bool   `json:"isArchived"`
	// Title with source "user", as a rename in the app leaves it, so the
	// app does not name the session itself.
	Title       string `json:"title"`
	TitleSource string `json:"titleSource"`
	// The app's own permission mode: the owner's settings apply when he
	// goes on with the session there, not Aide's.
	PermissionMode           string `json:"permissionMode"`
	RemoteMcpServersConfig   []any  `json:"remoteMcpServersConfig"`
	AlwaysAllowedReasons     []any  `json:"alwaysAllowedReasons"`
	SessionPermissionUpdates []any  `json:"sessionPermissionUpdates"`
}

func newDesktopRecord(cliSessionID, cwd, title string, now time.Time) desktopRecord {
	ms := now.UnixMilli()
	return desktopRecord{SessionID: "local_" + cliSessionID, CLISessionID: cliSessionID, Cwd: cwd, OriginCwd: cwd,
		LastFocusedAt: ms, CreatedAt: ms, LastActivityAt: ms, Title: title, TitleSource: "user", PermissionMode: "default",
		RemoteMcpServersConfig: []any{}, AlwaysAllowedReasons: []any{}, SessionPermissionUpdates: []any{}}
}

// desktopTitle names a session so the owner tells it from his own:
// project · who started it via Aide · the session's title.
func desktopTitle(project, requester, title string) string {
	who := "Aide"
	if requester != "" {
		who = requester + " via Aide"
	}
	parts := []string{who}
	if project != "" {
		parts = append([]string{project}, parts...)
	}
	if title != "" {
		parts = append(parts, title)
	}
	t, _ := provider.Truncate(strings.Join(parts, " · "), 120)
	return t
}

// writeDesktopRecord adds r to dir as a new file, never replacing one: a
// record already there (the app's own, after an import) stays as it is.
// It is written whole under another name first, as the app reads every
// local_*.json in the folder.
func writeDesktopRecord(dir string, r desktopRecord) (string, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, r.SessionID+".json")
	tmp, err := os.CreateTemp(dir, ".aide-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(b)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		return "", fmt.Errorf("adding %s: %w", path, err)
	}
	return path, nil
}

// desktopEntry is how a new session is to be listed: as the session id
// Aide gave it, in its directory, under title.
type desktopEntry struct{ id, workDir, title string }

// listInDesktop records a new Claude Code session in the desktop app's
// index once Claude Code says it started it, if the app is there; why not
// is logged.
func (s *session) listInDesktop(cliSessionID string) {
	d := s.desktop
	if d == nil || d.id != cliSessionID {
		return
	}
	s.desktop = nil
	home, _ := os.UserHomeDir()
	indexes := desktopIndexes(home, os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA"))
	dir, err := desktopAccountDir(indexes)
	if err != nil {
		lvl := slog.LevelDebug // no desktop app: nothing to say
		for _, idx := range indexes {
			if _, e := os.Stat(filepath.Dir(idx)); e == nil {
				lvl = slog.LevelInfo
			}
		}
		s.log.Log(context.Background(), lvl, "session not listed in the Claude desktop app", "claude_session", cliSessionID, "why", err)
		return
	}
	cwd := d.workDir
	if p, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = p // the directory Claude Code names the transcript's folder after
	}
	path, err := writeDesktopRecord(dir, newDesktopRecord(cliSessionID, cwd, d.title, time.Now()))
	if err != nil {
		s.log.Warn("could not list the session in the Claude desktop app", "claude_session", cliSessionID, "err", err)
		return
	}
	s.log.Info("session listed in the Claude desktop app (shown after the app restarts; go on with it there only once Aide is done with it)",
		"claude_session", cliSessionID, "record", path)
}
