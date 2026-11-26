package acp

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/samir2015dzh/aide/internal/provider"
)

// Antigravity's background tasks (工单 #158, after hermesh 3580ee41). A
// command that runs longer than it waits for (WaitMsBeforeAsync), or a
// daemon, goes on in the background: its tool call reports "completed" at
// once, saying "Tool is running as a background task with task id: <id>",
// where its log is, and what the model is to do next. Aide shows the call
// as running in the background, then its log as it grows, then how it
// ended: as Antigravity records it (a message in .system_generated/messages
// that it "finished with result:", with its exit code), the agent stopping
// it (manage_task kill), or the agent's process ending, after which nothing
// follows it. Each is a ToolResult for the call (Event.Background), the
// latest standing for it. An end is never inferred from the agent waking up
// by itself: a wake may only say the task still runs, or launch another.

var (
	bgLaunchRE = regexp.MustCompile(`^Tool is running as a background task with task id:\s*(\S+)`)
	bgLogRE    = regexp.MustCompile(`(?m)^Task logs are available at:\s*(file://[^\r\n]+)`)
	bgExitRE   = regexp.MustCompile(`(?m)^Command exited with code (-?\d+)\.`)
)

// bgPoll is how often running background tasks are looked at; wakeQuiet,
// how long the agent's reply, when it wakes up by itself, is waited on;
// bgLinger, how long after it was last at work by itself (or a task of
// its ended) it is taken to be still at it.
var bgPoll, wakeQuiet, bgLinger = 2 * time.Second, 2 * time.Second, time.Minute

const (
	bgShownTail = 4 << 10 // of a running task's log
	bgMaxMsg    = 1 << 20 // a message file read
)

// bgTask is a call running in the background: its task id and log.
type bgTask struct {
	callID, taskID, log string
	shown               string // the log's tail as last shown
}

// Background reports whether a task the agent left running is still
// followed, or the agent was at work by itself (woken, as when a task
// ends) a moment ago (provider.Backgrounder).
func (s *session) Background() bool {
	return s.bgRunning.Load() || time.Since(time.Unix(0, s.woke.Load())) < bgLinger
}

// launched takes a completed call whose output says it goes on in the
// background (Antigravity's), and shows it running. false: it does not.
func (s *session) launched(callID, out string) bool {
	if s.name != "antigravity" {
		return false
	}
	m := bgLaunchRE.FindStringSubmatch(strings.TrimSpace(out))
	if m == nil {
		return false
	}
	t := &bgTask{callID: callID, taskID: m[1]}
	if l := bgLogRE.FindStringSubmatch(out); l != nil {
		t.log = systemFile(l[1], "tasks", ".log")
	}
	// What follows "Log output:" is the log; what the model is told to do
	// next is not shown.
	shown := out
	if i := strings.Index(shown, "\nYOU MUST TAKE ONE OF THE FOLLOWING TWO ACTIONS:"); i >= 0 {
		shown = shown[:i]
	}
	if _, after, ok := strings.Cut(shown, "\nLog output:\n"); ok {
		shown = after
	} else {
		shown = ""
	}
	t.shown = strings.TrimSpace(shown)
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	if s.bg == nil {
		s.bg = map[string]*bgTask{}
	}
	s.bg[callID] = t
	s.bgRunning.Store(true)
	if t.log != "" && s.bgMessages == "" {
		s.bgMessages = filepath.Join(filepath.Dir(filepath.Dir(t.log)), "messages")
	}
	s.emit(t.running())
	if !s.bgPolling {
		s.bgPolling = true
		go s.pollBackground()
	}
	return true
}

func (t *bgTask) running() provider.Event {
	return bgResult(t, "[Aide] Running in the background (Antigravity task "+t.taskID+").", t.shown, false)
}

// bgResult is a background task's state as its call's result.
func bgResult(t *bgTask, head, log string, isError bool) provider.Event {
	text := head
	if log = strings.TrimSpace(log); log != "" {
		text += "\n\n" + log
	}
	sum, trunc := provider.KeepOutput(text)
	return provider.Event{Kind: provider.EventToolResult, CallID: t.callID, Summary: sum, IsError: isError, Truncated: trunc, Background: true}
}

func (s *session) pollBackground() {
	tick := time.NewTicker(bgPoll)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
		case <-s.proc.Done():
			return
		}
		if !s.followBackground() {
			return
		}
	}
}

// followBackground shows how the running tasks' logs grew and the ends
// Antigravity recorded. false: none is left (the poller stops, bgPolling
// cleared).
func (s *session) followBackground() bool {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	for _, e := range s.finishedTasksLocked() {
		s.emit(e)
	}
	for _, t := range s.bg {
		if t.log == "" {
			continue
		}
		if tail, err := readTail(t.log, bgShownTail); err == nil && strings.TrimSpace(tail) != "" && strings.TrimSpace(tail) != t.shown {
			t.shown = strings.TrimSpace(tail)
			s.emit(t.running())
		}
	}
	s.bgRunning.Store(len(s.bg) > 0)
	if len(s.bg) == 0 {
		s.bgPolling = false
		return false
	}
	return true
}

// finishedTasksLocked reads the messages Antigravity left since last time
// (each once) and ends the tasks they say finished, with their exit code
// and the end of their log.
func (s *session) finishedTasksLocked() []provider.Event {
	if s.bgMessages == "" || len(s.bg) == 0 {
		return nil
	}
	entries, err := os.ReadDir(s.bgMessages)
	if err != nil {
		return nil
	}
	if s.bgSeen == nil {
		s.bgSeen = map[string]bool{}
	}
	var out []provider.Event
	for _, ent := range entries {
		name := filepath.Join(s.bgMessages, ent.Name())
		if !strings.HasSuffix(name, ".json") || s.bgSeen[name] {
			continue
		}
		b, err := readTail(name, bgMaxMsg)
		var m struct {
			Sender  string `json:"sender"`
			Content string `json:"content"`
		}
		if err != nil || json.Unmarshal([]byte(b), &m) != nil {
			continue // not (yet) a whole message: read again next time
		}
		s.bgSeen[name] = true
		var t *bgTask
		for _, x := range s.bg {
			if x.taskID == m.Sender {
				t = x
			}
		}
		if t == nil || !strings.Contains(m.Content, "finished with result:") {
			continue // another's, or "still running"
		}
		delete(s.bg, t.callID)
		s.woke.Store(time.Now().UnixNano()) // the agent wakes up to it
		head, isError := "[Aide] The background task finished.", false
		if c := bgExitRE.FindStringSubmatch(m.Content); c != nil {
			head, isError = "[Aide] The background task finished with exit code "+c[1]+".", c[1] != "0"
		}
		log := m.Content
		if t.log != "" {
			if tail, err := readTail(t.log, provider.KeptOutput); err == nil {
				log = tail
			}
		}
		out = append(out, bgResult(t, head, log, isError))
	}
	return out
}

// stopped ends the task a completed manage_task call killed.
func (s *session) stopped(tc toolCall) {
	var in struct {
		Action      string `json:"Action"`
		ActionLower string `json:"action"`
		TaskID      string `json:"TaskId"`
		TaskIDSnake string `json:"task_id"`
	}
	if s.name != "antigravity" || json.Unmarshal(tc.RawInput, &in) != nil {
		return
	}
	switch strings.ToLower(firstNonEmpty(in.Action, in.ActionLower)) {
	case "kill", "stop", "cancel":
	default:
		return
	}
	id := firstNonEmpty(in.TaskID, in.TaskIDSnake)
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	for _, t := range s.bg {
		if id != "" && t.taskID == id {
			delete(s.bg, t.callID)
			s.emit(bgResult(t, "[Aide] The agent stopped this background task.", t.shown, false))
		}
	}
	s.bgRunning.Store(len(s.bg) > 0)
}

// endBackground ends the tasks still followed: with the agent's process,
// nothing follows them any more (they most likely stopped with it).
func (s *session) endBackground(send func(provider.Event)) {
	s.bgMu.Lock()
	defer s.bgMu.Unlock()
	for _, t := range s.bg {
		send(bgResult(t, "[Aide] The agent's process ended, so this background task is no longer followed; it most likely stopped with it.", t.shown, true))
	}
	s.bg = nil
	s.bgRunning.Store(false)
}

// systemFile is the path of a file:// URI naming a file of Antigravity's
// (.system_generated/<dir>/*<ext>); "" for any other.
func systemFile(uri, dir, ext string) string {
	u, err := url.Parse(strings.TrimSpace(uri))
	if err != nil || u.Scheme != "file" || u.Path == "" {
		return ""
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		p = strings.TrimPrefix(p, "/") // /C:/x
	}
	p = filepath.Clean(filepath.FromSlash(p))
	if !filepath.IsAbs(p) || !isSystemFile(p, dir, ext) {
		return ""
	}
	return p
}

func isSystemFile(p, dir, ext string) bool {
	return strings.HasSuffix(p, ext) && filepath.Base(filepath.Dir(p)) == dir && filepath.Base(filepath.Dir(filepath.Dir(p))) == ".system_generated"
}

// readTail reads the last max bytes of the regular file at path, which,
// symlinks resolved, must still be one of Antigravity's
// (.system_generated/tasks or messages): a path the agent names is not
// followed anywhere else.
func readTail(path string, n int64) (string, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !isSystemFile(real, "tasks", ".log") && !isSystemFile(real, "messages", ".json") {
		return "", errors.New("not a file of the agent's background tasks")
	}
	st, err := os.Lstat(real)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	f, err := os.Open(real)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if fst, err := f.Stat(); err != nil || !os.SameFile(st, fst) {
		return "", errors.New("the file changed while opened")
	}
	off := max(st.Size()-n, 0)
	b, err := io.ReadAll(io.NewSectionReader(f, off, n))
	if err != nil {
		return "", err
	}
	if off > 0 {
		for len(b) > 0 && !utf8.RuneStart(b[0]) {
			b = b[1:]
		}
	}
	return string(b), nil
}
