package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	machinev1 "github.com/hermeshai/hermesh/gen/go/hermesh/machine/v1"
	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
)

// TestDSHNativeSessions: the owner sees the DeepSeek Harness sessions
// Hermesh did not start, on each machine (DSH_HOME, as DSH 0.2.2 keeps
// them), and opens one: its title, when it was last at work and its
// conversation, read from the remote machine end to end, from this Mac's
// own in process. The session of DSH's that a Hermesh session runs, and a
// DSH subagent's, are not among them. A Mac that is only a machine sees
// its own. Nothing in either DSH home changes.
func TestDSHNativeSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	macHome, workHome := t.TempDir(), t.TempDir()
	w := newOwnerMac(t, []string{"DSH_HOME=" + macHome, "HERMESH_DEEPSEEK_BIN=" + filepath.Join(binaries(t), "fakeacp"+exe), "FAKE_ACP_STATE=" + t.TempDir()},
		&localv1.GrantSpec{Agents: []string{"deepseek"}})
	ctx := context.Background()

	// A session Hermesh starts: DSH writes its log under the id Hermesh
	// keeps (the scripted agent here does not, so the test does).
	s, r := w.startSession(time.Minute, "--agent", "deepseek", "--prompt", "hello")
	if r.code != 0 {
		t.Fatalf("start: %+v", r)
	}
	eventually(t, time.Minute, "its turn done", func() bool { s = w.get(s.SessionID); return s.Status == "idle" && s.Ref != "" })
	writeDSHLog(t, macHome, map[string]any{"id": s.Ref, "cwd": w.repo}, dshTurn(0, 1, "hello", "hi"))
	// The owner's own in DSH Desktop on this Mac, its last frame cut short
	// by a write under way, and a subagent's.
	desktop := writeDSHLog(t, macHome, map[string]any{"id": "desktop-1", "cwd": w.repo}, dshTurn(0, 1, "Explain the repo", "It is a demo."),
		dshEvents(6, dshEvent("session/title", map[string]any{"title": "Explain the repo"})))
	torn := dshZstd.EncodeAll([]byte(`{"type":"turn/start","seq":7,"time":1,"data":{"turn":2}}`+"\n"), nil)
	f, _ := os.OpenFile(filepath.Join(desktop, "session.v4.jsonl.zstd"), os.O_APPEND|os.O_WRONLY, 0)
	f.Write(torn[:len(torn)-5])
	f.Close()
	writeDSHLog(t, macHome, map[string]any{"id": "sub-1", "cwd": w.repo, "origin": "subagent", "parentSession": "desktop-1"}, dshTurn(0, 1, "x", "y"))

	// Another computer, a machine only, where another tool's DSH session
	// had its last turn later.
	home, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	work, _ := w.otherMac("work-mac", []string{"DSH_HOME=" + workHome})
	w.logInMachineOnly(work, home, "work-mac")
	writeDSHLog(t, workHome, map[string]any{"id": "tool-7", "cwd": "/srv/site"}, dshTurn(100, 1, "Deploy the site", "Deployed."))

	before := map[string]map[string]string{macHome: dshSnapshot(t, macHome), workHome: dshSnapshot(t, workHome)}
	var list *localv1.ListDSHSessionsResponse
	eventually(t, time.Minute, "both machines listed", func() bool {
		list, err = w.app.client.ListDSHSessions(ctx, &localv1.ListDSHSessionsRequest{})
		return err == nil && len(list.Unreachable) == 0 && len(list.Sessions) == 2
	})
	var got []string
	for _, e := range list.Sessions {
		got = append(got, fmt.Sprint(e.MachineName, " ", e.Session.SessionId, " ", e.Session.Title, " ", e.Session.LastActivityAt.AsTime().UnixMilli()-dshT0, " ", e.Session.TurnOpen))
	}
	if want := "work-mac tool-7 Deploy the site 105000 false\nowner-mac desktop-1 Explain the repo 6000 false"; strings.Join(got, "\n") != want {
		t.Fatalf("listed:\n%s\nwant:\n%s", strings.Join(got, "\n"), want)
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		for _, e := range list.Sessions {
			if e.Session.State != machinev1.DSHSessionState_DSH_SESSION_STATE_ENDED {
				t.Errorf("%s: %v, while DSH does not have it open", e.Session.SessionId, e.Session.State)
			}
		}
	}

	read := func(machine, id string) string {
		t.Helper()
		res, err := w.app.client.ReadDSHSession(ctx, &localv1.ReadDSHSessionRequest{MachineId: machine, SessionId: id, View: sessionv1.EventView_EVENT_VIEW_BASIC})
		if err != nil {
			t.Fatalf("read %s on %s: %v", id, machine, err)
		}
		var out []string
		for _, e := range res.Events {
			switch k := e.Kind.(type) {
			case *sessionv1.SessionEvent_UserMessage:
				out = append(out, "user: "+k.UserMessage.Text)
			case *sessionv1.SessionEvent_AgentMessage:
				out = append(out, "dsh: "+k.AgentMessage.Text)
			case *sessionv1.SessionEvent_ToolCall:
				out = append(out, "tool: "+k.ToolCall.Name)
			case *sessionv1.SessionEvent_TurnCompleted:
				out = append(out, fmt.Sprint("turn ", k.TurnCompleted.Turn, " done"))
			}
		}
		return res.Session.Title + ": " + strings.Join(out, "; ")
	}
	if got := read("work-mac", "tool-7"); got != "Deploy the site: user: Deploy the site; tool: bash; dsh: Deployed.; turn 1 done" {
		t.Fatalf("work-mac's: %s", got)
	}
	if got := read("owner-mac", "desktop-1"); got != "Explain the repo: user: Explain the repo; tool: bash; dsh: It is a demo.; turn 1 done" {
		t.Fatalf("this Mac's: %s", got)
	}

	// The machine-only Mac's own app sees its own.
	own, err := work.ListDSHSessions(ctx, &localv1.ListDSHSessionsRequest{})
	if err != nil || len(own.Sessions) != 1 || own.Sessions[0].Session.SessionId != "tool-7" || own.Sessions[0].MachineName != "work-mac" {
		t.Fatalf("on work-mac: %v %v", own, err)
	}
	if res, err := work.ReadDSHSession(ctx, &localv1.ReadDSHSessionRequest{SessionId: "tool-7"}); err != nil || len(res.Events) == 0 {
		t.Fatalf("read on work-mac: %v %v", res, err)
	}

	for dir, snap := range before {
		if after := dshSnapshot(t, dir); fmt.Sprint(after) != fmt.Sprint(snap) {
			for p, a := range after {
				if snap[p] != a {
					t.Errorf("%s: %q, was %q", p, a, snap[p])
				}
			}
			for p := range snap {
				if _, ok := after[p]; !ok {
					t.Errorf("%s: gone", p)
				}
			}
			t.Fatalf("DSH's home %s changed", dir)
		}
	}
}

// TestDSHNativeCLI: an agent whose grant sees the sessions it did not
// start lists a machine's DeepSeek Harness sessions with the hermesh CLI,
// as a dispatcher does (session list --native), and opens one (session
// get, session events), read only: sending to it, or anything else that
// acts on it, is refused, plainly. One that sees its own sessions sees
// none; one that sees all in a project, those in its folder. Nothing in
// DSH's home changes.
func TestDSHNativeCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	home := t.TempDir()
	all := &localv1.GrantSpec{TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_ALL}
	w := newOwnerMac(t, []string{"DSH_HOME=" + home}, all)
	own := w.addAgent("own", &localv1.GrantSpec{})
	inDemo := w.addAgent("in-demo", &localv1.GrantSpec{TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_ALL, ProjectIds: []string{"demo"}})
	writeDSHLog(t, home, map[string]any{"id": "desktop_1", "cwd": filepath.Join(w.repo, "web")}, dshTurn(0, 1, "Explain the repo", "It is a demo."),
		dshEvents(6, dshEvent("session/title", map[string]any{"title": "Explain the repo"})))
	writeDSHLog(t, home, map[string]any{"id": "tool-7", "cwd": "/srv/site"}, dshTurn(100, 1, "Deploy the site", "Deployed."))
	before := dshSnapshot(t, home)

	type native struct {
		Source, Title, Machine, State, Cwd string
		SessionID                          string  `json:"session_id"`
		MachineID                          string  `json:"machine_id"`
		DSHSessionID                       string  `json:"dsh_session_id"`
		ReadOnly                           bool    `json:"read_only"`
		LastActivityAt                     *string `json:"last_activity_at"`
	}
	list := func(env []string, args ...string) []native {
		t.Helper()
		r := run(t, env, time.Minute, filepath.Join(w.bin, "hermesh"), append([]string{"session", "list", "--native", "-o", "json"}, args...)...)
		var doc struct {
			OK       bool
			Sessions []json.RawMessage
			Native   []native `json:"native_sessions"`
			Offline  []json.RawMessage
		}
		if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || !doc.OK || r.code != 0 || len(doc.Offline) != 0 || len(doc.Sessions) != 0 {
			t.Fatalf("session list --native: %v %+v", err, r)
		}
		return doc.Native
	}
	got := list(w.cliEnv)
	if len(got) != 2 {
		t.Fatalf("listed %+v", got)
	}
	site, desktop := got[0], got[1]
	if site.DSHSessionID != "tool-7" || site.Title != "Deploy the site" || site.Cwd != "/srv/site" || site.Machine != "owner-mac" || site.Source != "dsh_native" || !site.ReadOnly ||
		*site.LastActivityAt != time.UnixMilli(dshT0+105000).UTC().Format("2006-01-02T15:04:05.000Z07:00") || !strings.HasPrefix(site.SessionID, "dsh_") ||
		desktop.DSHSessionID != "desktop_1" || desktop.Title != "Explain the repo" {
		t.Fatalf("listed %+v", got)
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		if site.State != "ended" {
			t.Errorf("state %q, while DSH does not have it open", site.State)
		}
	}
	if again := list(w.cliEnv, "--machine", "owner-mac", "--since", "1000000h"); len(again) != 2 {
		t.Fatalf("on owner-mac: %+v", again)
	}
	if r := w.cli(time.Minute, "session", "list", "--native"); r.code != 0 || !strings.Contains(r.stdout, "DSH NATIVE SESSION (READ-ONLY)") ||
		!strings.Contains(r.stdout, desktop.SessionID) || !strings.Contains(r.stdout, "Deploy the site") {
		t.Fatalf("session list --native: %+v", r)
	}
	if r := w.cli(time.Minute, "session", "list"); r.code != 0 || strings.Contains(r.stdout, "dsh_") {
		t.Fatalf("session list without --native: %+v", r)
	}

	// Opened by the id the list gave, the session and its conversation.
	r := w.cli(time.Minute, "session", "get", desktop.SessionID, "-o", "json")
	var doc struct {
		Native native `json:"native_session"`
	}
	if json.Unmarshal([]byte(r.stdout), &doc); r.code != 0 || doc.Native.SessionID != desktop.SessionID || doc.Native.Title != "Explain the repo" || !doc.Native.ReadOnly {
		t.Fatalf("session get: %+v", r)
	}
	var keys struct {
		Native map[string]any `json:"native_session"`
	}
	json.Unmarshal([]byte(r.stdout), &keys)
	documented := []string{"session_id", "source", "read_only", "dsh_session_id", "title", "machine", "machine_id", "state", "turn_open", "archived", "cwd", "agent_preset",
		"error", "created_at", "last_activity_at"} // docs/cli-output.md, NativeSession
	if got := slices.Sorted(maps.Keys(keys.Native)); !slices.Equal(got, slices.Sorted(slices.Values(documented))) {
		t.Fatalf("native_session's fields: %v", got)
	}
	if r := w.cli(time.Minute, "session", "get", desktop.SessionID); r.code != 0 || !strings.Contains(r.stdout, "DSH native, read-only") || !strings.Contains(r.stdout, "title: Explain the repo") {
		t.Fatalf("session get (text): %+v", r)
	}
	r = w.cli(time.Minute, "session", "events", desktop.SessionID, "-o", "json")
	var evs struct {
		Events []struct{ Type, Text string }
		Page   struct {
			HasMoreBefore bool `json:"has_more_before"`
		}
	}
	json.Unmarshal([]byte(r.stdout), &evs)
	var said []string
	for _, e := range evs.Events {
		if e.Type == "user_message" || e.Type == "agent_message" || e.Type == "turn_completed" {
			said = append(said, e.Type+" "+e.Text)
		}
	}
	if r.code != 0 || strings.Join(said, "; ") != "user_message Explain the repo; agent_message It is a demo.; turn_completed " || evs.Page.HasMoreBefore {
		t.Fatalf("session events: %q %+v", said, r)
	}
	if r := w.cli(time.Minute, "session", "events", desktop.SessionID, "--limit", "1"); r.code != 0 || !strings.Contains(r.stderr, "earlier: --before") {
		t.Fatalf("session events --limit 1: %+v", r)
	}

	// Nothing goes to it, and it can't be taken over.
	refused := func(args ...string) {
		t.Helper()
		r := w.cli(time.Minute, append(args, "-o", "json")...)
		var doc struct {
			OK    bool
			Error struct{ Kind, Message, Hint string }
		}
		json.Unmarshal([]byte(r.stdout), &doc)
		if r.code != 2 || doc.OK || doc.Error.Kind != "invalid_state" || !strings.Contains(doc.Error.Message, "agents can only read it") || !strings.Contains(doc.Error.Hint, "session events") {
			t.Fatalf("%v: %+v", args, r)
		}
	}
	refused("message", "send", "--to", desktop.SessionID, "--message", "carry on")
	refused("message", "send", "--to", site.MachineID+"/"+site.SessionID, "--message", "carry on")
	refused("session", "send", desktop.SessionID, "--message", "carry on")
	refused("session", "interrupt", desktop.SessionID)
	refused("session", "archive", desktop.SessionID)

	// An agent that sees its own sessions sees none of DSH's, nor reads one.
	if got := list(own); len(got) != 0 {
		t.Fatalf("listed to an agent that sees its own: %+v", got)
	}
	if r := run(t, own, time.Minute, filepath.Join(w.bin, "hermesh"), "session", "get", desktop.SessionID, "-o", "json"); r.code != 2 || !strings.Contains(r.stdout, `"not_found"`) {
		t.Fatalf("read by an agent that sees its own: %+v", r)
	}
	// One that sees all in demo, those in demo's folder.
	if got := list(inDemo); len(got) != 1 || got[0].SessionID != desktop.SessionID {
		t.Fatalf("listed to an agent of demo's: %+v", got)
	}
	if r := run(t, inDemo, time.Minute, filepath.Join(w.bin, "hermesh"), "session", "events", site.SessionID); r.code != 2 {
		t.Fatalf("read outside demo: %+v", r)
	}

	if after := dshSnapshot(t, home); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("DSH's home changed")
	}
}

var dshT0 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC).UnixMilli()

var dshZstd, _ = zstd.NewWriter(nil)

// writeDSHLog writes a session's log in a DSH home as DSH 0.2.2 does: its
// header in a zstd frame, then a frame per batch of events.
func writeDSHLog(t *testing.T, home string, header map[string]any, batches ...[]map[string]any) string {
	t.Helper()
	header["type"], header["version"], header["createdAt"], header["isSeeded"], header["delegationDepth"] = "session", 4, dshT0, false, 0
	key := strings.TrimLeft(strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(header["cwd"].(string)), "-")
	dir := filepath.Join(home, "sessions", "--"+key+"--", header["id"].(string))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	frame := func(lines ...any) []byte {
		var b []byte
		for _, l := range lines {
			j, _ := json.Marshal(l)
			b = append(append(b, j...), '\n')
		}
		return dshZstd.EncodeAll(b, nil)
	}
	out := frame(header)
	for _, batch := range batches {
		var lines []any
		for _, e := range batch {
			lines = append(lines, e)
		}
		out = append(out, frame(lines...)...)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.v4.jsonl.zstd"), out, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func dshEvent(typ string, data map[string]any, extra ...any) map[string]any {
	e := map[string]any{"type": typ, "data": data}
	for i := 0; i+1 < len(extra); i += 2 {
		e[extra[i].(string)] = extra[i+1]
	}
	return e
}

// dshEvents numbers evs from seq, a second apart.
func dshEvents(seq int, evs ...map[string]any) []map[string]any {
	for i, e := range evs {
		e["seq"], e["time"] = seq+i, dshT0+int64(seq+i)*1000
	}
	return evs
}

// dshTurn is a turn of DSH's: a prompt, a tool call and its result, a reply.
func dshTurn(seq, turn int, prompt, reply string) []map[string]any {
	msg := func(role, kind, text string) map[string]any {
		return map[string]any{"id": "m", "role": role, "content": []any{map[string]any{"type": "text", "text": text}}, "source": map[string]any{"kind": kind}, "toolCallId": "c"}
	}
	return dshEvents(seq,
		dshEvent("turn/start", map[string]any{"turn": turn}),
		dshEvent("user/message", msg("user", "user", prompt), "surfaceOp", "append"),
		dshEvent("tool/call", map[string]any{"turn": turn, "step": 0, "callId": "c", "name": "bash", "arguments": `{"command":"ls"}`}),
		dshEvent("tool/result", map[string]any{"turn": turn, "step": 0, "message": msg("tool", "tool", "README.md")}, "surfaceOp", "append"),
		dshEvent("assistant/message", map[string]any{"turn": turn, "step": 1, "message": msg("assistant", "model", reply), "stream": []any{}}, "surfaceOp", "append"),
		dshEvent("turn/end", map[string]any{"turn": turn, "reason": map[string]any{"kind": "completed"}}),
	)
}

// dshSnapshot is every file under dir, with its size, time and content.
func dshSnapshot(t *testing.T, dir string) map[string]string {
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
