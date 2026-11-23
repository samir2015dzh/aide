package dshnative

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// t0 is when the fixtures' sessions start.
var t0 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC).UnixMilli()

// writeSession writes a session's log in home as DSH 0.2.2 does: the
// header in a zstd frame of its own, then a frame per batch of events (a
// raw .jsonl without zstd). It returns the session's directory.
func writeSession(t *testing.T, home string, hdr map[string]any, compressed bool, batches ...[]map[string]any) string {
	t.Helper()
	hdr = with(map[string]any{"type": "session", "version": 4, "createdAt": t0, "isSeeded": false, "delegationDepth": 0}, hdr)
	project := "_no-cwd"
	if cwd, ok := hdr["cwd"].(string); ok {
		project = projectKey(cwd)
	}
	dir := filepath.Join(home, "sessions", project, encodeSegment(hdr["id"].(string)))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var out []byte
	add := func(lines ...any) {
		var b []byte
		for _, l := range lines {
			j, _ := json.Marshal(l)
			b = append(append(b, j...), '\n')
		}
		if compressed {
			b = encoder.EncodeAll(b, nil)
		}
		out = append(out, b...)
	}
	add(hdr)
	for _, batch := range batches {
		var lines []any
		for _, e := range batch {
			lines = append(lines, e)
		}
		add(lines...)
	}
	name := fmt.Sprintf("session.v%d.jsonl", hdr["version"])
	if compressed {
		name += ".zstd"
	}
	if err := os.WriteFile(filepath.Join(dir, name), out, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

var encoder, _ = zstd.NewWriter(nil)

func with(a, b map[string]any) map[string]any {
	for k, v := range b {
		a[k] = v
	}
	return a
}

// projectKey is DSH's name for a project's directory (only as the
// fixtures need it).
func projectKey(cwd string) string {
	return "--" + strings.TrimLeft(separators.ReplaceAllString(cwd, "-"), "-") + "--"
}

var separators = regexp.MustCompile(`[/\\:]+`)

// events numbers evs from seq, a second apart.
func events(seq int, evs ...map[string]any) []map[string]any {
	for i, e := range evs {
		e["seq"], e["time"] = seq+i, t0+int64(seq+i)*1000
	}
	return evs
}

func ev(typ string, data map[string]any, extra ...any) map[string]any {
	e := map[string]any{"type": typ, "data": data}
	for i := 0; i+1 < len(extra); i += 2 {
		e[extra[i].(string)] = extra[i+1]
	}
	return e
}

func text(t string) []any { return []any{map[string]any{"type": "text", "text": t}} }

func userMsg(t string) map[string]any {
	return ev("user/message", map[string]any{"id": "m", "role": "user", "content": text(t), "source": map[string]any{"kind": "user"}}, "surfaceOp", "append")
}

// conversation is one turn: a prompt, a tool call and its result, a reply.
func conversation(seq, turn int, prompt, reply string) []map[string]any {
	return events(seq,
		ev("turn/start", map[string]any{"turn": turn}),
		userMsg(prompt),
		ev("step/start", map[string]any{"turn": turn, "step": 0}),
		ev("assistant/message", map[string]any{"turn": turn, "step": 0, "message": map[string]any{"id": "a", "role": "assistant",
			"content": []any{map[string]any{"type": "reasoning", "text": "thinking"}, map[string]any{"type": "tool-call", "id": "c1", "name": "bash", "arguments": "{}"}},
			"source":  map[string]any{"kind": "model", "provider": "deepseek", "model": "m"}}, "stream": []any{}}, "surfaceOp", "append"),
		ev("tool/call", map[string]any{"turn": turn, "step": 0, "callId": fmt.Sprint("c", turn), "name": "bash", "arguments": `{"command":"ls"}`}),
		ev("tool/result", map[string]any{"turn": turn, "step": 0, "message": map[string]any{"id": "r", "role": "tool", "content": text("README.md"),
			"source": map[string]any{"kind": "tool", "callId": "c1"}, "toolCallId": fmt.Sprint("c", turn)}}, "surfaceOp", "append"),
		ev("assistant/message", map[string]any{"turn": turn, "step": 1, "message": map[string]any{"id": "a2", "role": "assistant", "content": text(reply),
			"source": map[string]any{"kind": "model", "provider": "deepseek", "model": "m"}}, "stream": []any{}}, "surfaceOp", "append"),
		ev("turn/end", map[string]any{"turn": turn, "reason": map[string]any{"kind": "completed"}}),
	)
}

// snapshot is every file under dir, with its size, time and content.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		// os.Lstat, not d.Info: on Windows that is the directory listing's
		// copy, which NTFS updates late after a file was appended to.
		fi, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		out[path] = fmt.Sprintf("%v %d %v %x", d.IsDir(), fi.Size(), fi.ModTime(), sha256.Sum256(b))
		return nil
	})
	return out
}

func TestListAndRead(t *testing.T) {
	home := t.TempDir()
	// The owner's session in DSH Desktop: two turns, a title, a compaction,
	// an event of a plugin DSH may skip, an older format's log beside it,
	// and a last frame cut short.
	owner := writeSession(t, home, map[string]any{"id": "0f3b-owner", "cwd": "/home/alex/app", "agentPreset": "coder"}, true,
		conversation(0, 1, "List the files\nplease", "There is a README."),
		events(8, ev("session/title", map[string]any{"title": "List files", "messageSeqs": []int{1}, "source": map[string]any{"kind": "fallback"}}),
			ev("plugin:x/y", map[string]any{"a": 1}, "ignorable", true)),
		append(conversation(10, 2, "Compact", "Done."), events(18,
			ev("compaction/start", map[string]any{"compactionId": "k", "turn": 2}),
			ev("compaction/summary", map[string]any{"compactionId": "k", "summary": "s"}),
			ev("user/message", map[string]any{"id": "c", "role": "user", "content": text("summary for the model"), "source": map[string]any{"kind": "user"}},
				"surfaceOp", map[string]any{"op": "replace", "startSeq": 1, "endSeq": 16}),
			ev("compaction/end", map[string]any{"compactionId": "k", "turn": 2}))...),
	)
	os.WriteFile(filepath.Join(owner, "session.v3.jsonl.zstd"), []byte("an older format"), 0o600)
	torn := encoder.EncodeAll([]byte(`{"type":"turn/start","seq":22,"time":1,"data":{"turn":3}}`+"\n"), nil)
	f, _ := os.OpenFile(filepath.Join(owner, "session.v4.jsonl.zstd"), os.O_APPEND|os.O_WRONLY, 0)
	f.Write(torn[:len(torn)-3])
	f.Close()

	// A subagent's session, and one Aide started: not listed.
	writeSession(t, home, map[string]any{"id": "sub", "cwd": "/home/alex/app", "origin": "subagent", "parentSession": "0f3b-owner"}, true, conversation(0, 1, "x", "y"))
	writeSession(t, home, map[string]any{"id": "aide-ref", "cwd": "/home/alex/app"}, true, conversation(0, 1, "x", "y"))
	// Archived, its title only in DSH's projection cache, a turn left open.
	writeSession(t, home, map[string]any{"id": "archived_1"}, true, events(0, ev("turn/start", map[string]any{"turn": 1}), userMsg("Old work")))
	os.MkdirAll(filepath.Join(home, "storages", "session_projcache", "sessions"), 0o700)
	os.WriteFile(filepath.Join(home, "storages", "session_projcache", "sessions", "archived_1.json"),
		[]byte(`{"version": 3, "record": {"identity": {"createdAt": 1}, "rows": {"title": {"ver": 1, "seq": 1, "val": "Cached title"}}}}`), 0o600)
	os.WriteFile(filepath.Join(home, "storages", "workspace.json"),
		[]byte(`{"unit": {"name": "workspace", "version": 2}, "global": {"initialized": true, "workspaceIds": [], "archivedSessionIds": ["archived_1"]}, "tables": {}}`), 0o600)
	// A log a newer DSH wrote, and one with an event DSH requires readers
	// to know: listed, but not read.
	writeSession(t, home, map[string]any{"id": "newer", "version": 5, "cwd": "/srv/x"}, true)
	writeSession(t, home, map[string]any{"id": "unknown/event"}, true, events(0, userMsg("Before"), ev("team/new-thing", map[string]any{})))
	// An uncompressed log, its last line not finished, titled by its prompt.
	raw := writeSession(t, home, map[string]any{"id": "raw", "cwd": `C:\Users\alex\site`}, false, conversation(0, 1, "Fix the site", "Fixed."))
	f, _ = os.OpenFile(filepath.Join(raw, "session.v4.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"turn/start","seq":8,"ti`)
	f.Close()

	before := snapshot(t, home)
	list, err := List(home, func(id, cwd string) bool { return id == "aide-ref" })
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range list {
		got = append(got, fmt.Sprintf("%s %q %s %v %v archived=%v %q %q", s.SessionId, s.Title, s.Cwd, s.LastActivityAt.AsTime().UnixMilli()-t0, s.TurnOpen, s.Archived, s.AgentPreset, s.Error))
	}
	want := []string{
		`0f3b-owner "List files" /home/alex/app 21000 false archived=false "coder" ""`,
		`raw "Fix the site" C:\Users\alex\site 7000 false archived=false "" ""`,
		`archived_1 "Cached title"  1000 true archived=true "" ""`,
		`newer "" /srv/x 0 false archived=false "" "unsupported DSH log format version 5"`,
		`unknown/event "Before"  0 false archived=false "" "unsupported DSH log format: event \"team/new-thing\""`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("listed:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		for _, s := range list {
			if s.State != machinev1.DSHSessionState_DSH_SESSION_STATE_ENDED {
				t.Errorf("%s: %v, while no process has it open", s.SessionId, s.State)
			}
		}
	}

	s, evs, err := Read(home, "0f3b-owner")
	if err != nil || s.Title != "List files" {
		t.Fatalf("read: %v %v", s, err)
	}
	var conv []string
	for _, e := range evs {
		conv = append(conv, fmt.Sprintf("%d %s", e.Seq, describeEvent(e)))
	}
	wantConv := []string{
		"1 turn 1", "2 user List the files\nplease", "5 tool c1 bash {\"command\":\"ls\"}", "6 result c1 README.md false", "7 agent There is a README.", "8 end 1 false FAILURE_KIND_UNSPECIFIED",
		"11 turn 2", "12 user Compact", "15 tool c2 bash {\"command\":\"ls\"}", "16 result c2 README.md false", "17 agent Done.", "18 end 2 false FAILURE_KIND_UNSPECIFIED",
		"22 status compacted",
	}
	if strings.Join(conv, "\n") != strings.Join(wantConv, "\n") {
		t.Fatalf("conversation:\n%s\nwant:\n%s", strings.Join(conv, "\n"), strings.Join(wantConv, "\n"))
	}
	if evs[0].Ts.AsTime().UnixMilli() != t0 {
		t.Errorf("first event at %v", evs[0].Ts.AsTime())
	}
	if s, evs, err := Read(home, "newer"); err != nil || evs != nil || s.Error == "" {
		t.Errorf("a newer format: %v %v %v", s, evs, err)
	}
	if _, evs, err := Read(home, "raw"); err != nil || len(evs) != 6 {
		t.Errorf("raw: %d events, %v", len(evs), err)
	}
	for _, id := range []string{"nope", "", "../sessions", "."} {
		if _, _, err := Read(home, id); err != ErrNotFound {
			t.Errorf("%q: %v", id, err)
		}
	}
	if after := snapshot(t, home); fmt.Sprint(after) != fmt.Sprint(before) {
		for p, a := range after {
			if before[p] != a {
				t.Errorf("%s: %q, was %q", p, a, before[p])
			}
		}
		for p := range before {
			if _, ok := after[p]; !ok {
				t.Errorf("%s: gone", p)
			}
		}
		t.Fatal("DSH's home changed")
	}
}

func describeEvent(e *sessionv1.SessionEvent) string {
	switch k := e.Kind.(type) {
	case *sessionv1.SessionEvent_TurnStarted:
		return fmt.Sprint("turn ", k.TurnStarted.Turn)
	case *sessionv1.SessionEvent_UserMessage:
		return "user " + k.UserMessage.Text
	case *sessionv1.SessionEvent_AgentMessage:
		return "agent " + k.AgentMessage.Text
	case *sessionv1.SessionEvent_ToolCall:
		return fmt.Sprint("tool ", k.ToolCall.CallId, " ", k.ToolCall.Name, " ", k.ToolCall.Summary)
	case *sessionv1.SessionEvent_ToolResult:
		return fmt.Sprint("result ", k.ToolResult.CallId, " ", k.ToolResult.Summary, " ", k.ToolResult.IsError)
	case *sessionv1.SessionEvent_TurnCompleted:
		return fmt.Sprint("end ", k.TurnCompleted.Turn, " ", k.TurnCompleted.IsError, " ", k.TurnCompleted.GetFailure().GetKind())
	case *sessionv1.SessionEvent_Status:
		return "status " + k.Status.Reason
	}
	return fmt.Sprintf("%T", e.Kind)
}

// A log read again once DSH appended to it says what it says now, not what
// List kept of it.
func TestListSeesAppends(t *testing.T) {
	home := t.TempDir()
	dir := writeSession(t, home, map[string]any{"id": "s"}, true, events(0, ev("turn/start", map[string]any{"turn": 1}), userMsg("Go")))
	list, _ := List(home, nil)
	if len(list) != 1 || !list[0].TurnOpen {
		t.Fatalf("%v", list)
	}
	f, _ := os.OpenFile(filepath.Join(dir, "session.v4.jsonl.zstd"), os.O_APPEND|os.O_WRONLY, 0)
	j, _ := json.Marshal(events(2, ev("turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "aborted", "reason": map[string]any{"kind": "user"}}}))[0])
	f.Write(encoder.EncodeAll(append(j, '\n'), nil))
	f.Close()
	list, _ = List(home, nil)
	if len(list) != 1 || list[0].TurnOpen || list[0].LastActivityAt.AsTime().UnixMilli() != t0+2000 {
		t.Fatalf("after DSH appended: %v", list)
	}
	_, evs, _ := Read(home, "s")
	if got := describeEvent(evs[len(evs)-1]); got != "end 1 true FAILURE_KIND_INTERRUPTED" {
		t.Fatalf("an interrupted turn: %s", got)
	}
}

func TestEncodeSegment(t *testing.T) {
	for id, want := range map[string]string{
		"0f3b-AZ_z.9": "0f3b-AZ_z.9", "a/b": "a~002Fb", "~": "~007E", ".": "~002E", "..": "~002E~002E", "é😀": "~00E9~D83D~DE00", "...": "...",
	} {
		if got := encodeSegment(id); got != want {
			t.Errorf("%q: %q, want %q", id, got, want)
		}
	}
}

func TestHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	for env, want := range map[string]string{
		"":                 filepath.Join(home, ".dsh"),
		"DSH_HOME=  ":      filepath.Join(home, ".dsh"),
		"DSH_HOME=~":       home,
		"DSH_HOME=~/dsh":   filepath.Join(home, "dsh"),
		"DSH_HOME=" + home: home,
	} {
		if got, err := Home([]string{"PATH=x", env}); err != nil || got != want {
			t.Errorf("%q: %q %v, want %q", env, got, err, want)
		}
	}
}

func TestZstdFrames(t *testing.T) {
	a, b := encoder.EncodeAll([]byte("one\n"), nil), encoder.EncodeAll([]byte(strings.Repeat("two", 1000)+"\n"), nil)
	all := append(append([]byte{}, a...), b...)
	for cut := len(a); cut <= len(all); cut++ {
		frames, err := zstdFrames(all[:cut])
		want := 1
		if cut == len(all) {
			want = 2
		}
		if err != nil || len(frames) != want {
			t.Fatalf("cut at %d: %v %v", cut, frames, err)
		}
	}
	if _, err := zstdFrames([]byte("not zstd at all")); err == nil {
		t.Fatal("not zstd")
	}
}

// A turn DSH ended on an error carries what kind it was and the provider
// it names, as Aide's own sessions' failures do (工单 #105).
func TestTurnErrorClassified(t *testing.T) {
	for msg, want := range map[string]string{
		"llm-deepseek: no API key for provider route \"deepseek-official\"": "FAILURE_KIND_MISSING_CREDENTIALS DeepSeek",
		"tool registry corrupted": "FAILURE_KIND_UNKNOWN ",
	} {
		f := turnEnd(1, "error", msg).Failure
		if got := f.Kind.String() + " " + f.Provider; got != want || f.Message != msg {
			t.Errorf("%q: %s (%q), want %s", msg, got, f.Message, want)
		}
	}
}
