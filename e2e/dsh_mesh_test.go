package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
)

// dshHostIO is the stand-in DSH Host (testdata/dsh-bridge-host.mjs) with
// its lines: what DSH was asked, and the tools' answers.
type dshHostIO struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan map[string]any
	done  chan struct{}
}

func startDSHHostIO(t *testing.T, home, log string) *dshHostIO {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	entry, _ := filepath.Abs(filepath.Join("apps", "dsh-bridge", "src", "index.ts"))
	script, _ := filepath.Abs(filepath.Join("e2e", "testdata", "dsh-bridge-host.mjs"))
	os.Remove(filepath.Join(dshnative.BridgeDir(home), "bridge.json")) // the last one's
	h := &dshHostIO{cmd: exec.Command(node, script, entry), lines: make(chan map[string]any, 64), done: make(chan struct{})}
	h.cmd.Env = append(os.Environ(), "DSH_HOME="+home, "FAKE_DSH_LOG="+log)
	h.cmd.Stderr = os.Stderr
	h.stdin, _ = h.cmd.StdinPipe()
	stdout, _ := h.cmd.StdoutPipe()
	if err := h.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(h.done)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) == nil {
				h.lines <- m
			}
		}
		h.cmd.Wait()
	}()
	t.Cleanup(h.stop)
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(dshnative.BridgeDir(home), "bridge.json")); err == nil {
			return h
		}
	}
	t.Fatal("the bridge plugin did not start")
	return nil
}

// stop closes DSH Desktop: the plugin is disposed.
func (h *dshHostIO) stop() {
	h.stdin.Close()
	select {
	case <-h.done:
	case <-time.After(10 * time.Second):
		h.cmd.Process.Kill()
		<-h.done
	}
}

// next is the next line for which keep says true (others are dropped).
func (h *dshHostIO) next(t *testing.T, what string, keep func(map[string]any) bool) map[string]any {
	t.Helper()
	timeout := time.After(time.Minute)
	for {
		select {
		case m := <-h.lines:
			if keep(m) {
				return m
			}
		case <-timeout:
			t.Fatalf("DSH was not asked: %s", what)
		}
	}
}

// prompt is the prompt DSH got under requestID.
func (h *dshHostIO) prompt(t *testing.T, requestID string) map[string]any {
	t.Helper()
	m := h.next(t, "a prompt "+requestID, func(m map[string]any) bool {
		p, _ := m["prompt"].(map[string]any)
		return p != nil && p["requestId"] == requestID
	})
	return m["prompt"].(map[string]any)
}

// call has the session's model call aide_message_send with args: its
// result, or its error's message.
func (h *dshHostIO) call(t *testing.T, sessionID string, args map[string]any) (map[string]any, string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"call": map[string]any{"tool": "aide_message_send", "sessionId": sessionID, "args": args}})
	if _, err := h.stdin.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
	m := h.next(t, "the tool's answer", func(m map[string]any) bool { return m["toolResult"] != nil || m["toolError"] != nil })
	if e, ok := m["toolError"].(map[string]any); ok {
		msg, _ := e["message"].(string)
		return nil, msg
	}
	return m["toolResult"].(map[string]any), ""
}

// Scenario two of the agent mesh (docs/agent-messages.md §15.3): an agent
// writes to a DSH session the owner took over and the session answers it.
// Handed at once while DSH Desktop runs, read by the model with who wrote
// it; the session's answer (aide_message_send, no `to`) goes back to that
// agent, as the machine, answering its message, which is then replied. It
// writes to agents only. With DSH Desktop closed the message waits and goes
// once it is open again; one for a session the owner no longer takes over
// is refused, and the agent told why.
func TestDSHTwoWay(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	home := dshDesktopHome(t)
	log := dshDesktopSession(t, home)
	w := newOwnerMac(t, []string{"DSH_HOME=" + home}, &localv1.GrantSpec{TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_ALL})
	muse := w.addAgent("muse", &localv1.GrantSpec{TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_ALL})
	host := startDSHHostIO(t, home, log)
	ctx := context.Background()
	aide := func(args ...string) result {
		return run(t, muse, time.Minute, filepath.Join(w.bin, "aide"), args...)
	}

	var id string
	eventually(t, time.Minute, "the DSH session listed", func() bool {
		var doc struct {
			Native []struct {
				SessionID string `json:"session_id"`
			} `json:"native_sessions"`
		}
		json.Unmarshal([]byte(aide("session", "list", "--native", "-o", "json").stdout), &doc)
		if len(doc.Native) == 1 {
			id = doc.Native[0].SessionID
		}
		return id != ""
	})
	l, err := w.app.client.ListDSHSessions(ctx, &localv1.ListDSHSessionsRequest{})
	if err != nil || len(l.Sessions) != 1 {
		t.Fatalf("listed: %v %v", l, err)
	}
	machineID := l.Sessions[0].MachineId
	takeover := func(on bool) {
		t.Helper()
		if _, err := w.app.client.SetDSHTakeover(ctx, &localv1.SetDSHTakeoverRequest{MachineId: machineID, SessionId: "dsh-owners", On: on}); err != nil {
			t.Fatal(err)
		}
	}
	takeover(true)

	type sent struct {
		Message struct {
			ID, Delivered string
			Delivery      []struct{ Result string }
		}
	}
	send := func(text string) sent {
		t.Helper()
		r := aide("message", "send", "-o", "json", "--to", id, "--message", text)
		var s sent
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &s) != nil || s.Message.ID == "" {
			t.Fatalf("send %q: %+v", text, r)
		}
		return s
	}
	state := func(mid string) string {
		var st struct{ States []struct{ ID, State string } }
		json.Unmarshal([]byte(aide("message", "status", mid, "-o", "json").stdout), &st)
		if len(st.States) != 1 {
			return ""
		}
		return st.States[0].State
	}
	type inboxMessage struct {
		ID        string
		From      struct{ Inbox, Session string }
		InReplyTo string `json:"in_reply_to"`
		Subject   string
		Text      string
	}
	arrived := func(what string, match func(inboxMessage) bool) inboxMessage {
		t.Helper()
		var got inboxMessage
		eventually(t, time.Minute, what, func() bool {
			var doc struct{ Messages []inboxMessage }
			json.Unmarshal([]byte(aide("message", "list", "-o", "json").stdout), &doc)
			for _, m := range doc.Messages {
				if match(m) {
					got = m
					return true
				}
			}
			return false
		})
		if r := aide("message", "ack", got.ID); r.code != 0 {
			t.Fatalf("ack: %+v", r)
		}
		return got
	}
	reqID := func(mid string) string { return dshnative.RequestPrefix + strings.TrimPrefix(mid, "msg_") }

	// 1. Muse writes: handed at once; the model reads who wrote it.
	d1 := send("做到哪了？")
	if d1.Message.Delivered != "handed" || len(d1.Message.Delivery) != 1 || d1.Message.Delivery[0].Result != "handed" {
		t.Fatalf("with DSH Desktop open: %+v", d1)
	}
	p := host.prompt(t, reqID(d1.Message.ID))
	content := p["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.HasPrefix(content, "[Aide] A message ("+d1.Message.ID+") from the agent muse (c_") || !strings.HasSuffix(content, "aide_message_send.\n\n做到哪了？") {
		t.Fatalf("the model read %q", content)
	}
	// The owner reads it as Muse's, as written.
	res, err := w.app.client.ReadDSHSession(ctx, &localv1.ReadDSHSessionRequest{MachineId: machineID, SessionId: "dsh-owners", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var last struct{ text, from, name string }
	for _, e := range res.Events {
		if um := e.GetUserMessage(); um != nil {
			last.text, last.from, last.name = um.Text, um.From, um.FromName
		}
	}
	if last.text != "做到哪了？" || !strings.HasPrefix(last.from, "c_") || last.name != "muse" {
		t.Fatalf("the owner reads %+v", last)
	}

	// 2. The session answers, naming no one: it goes to Muse, as an answer
	// to its message, from the session's address on the machine.
	out, why := host.call(t, "dsh-owners", map[string]any{"text": "做完 3 项，还差 1 项"})
	if why != "" || !strings.HasPrefix(out["to"].(string), "c_") || !strings.HasPrefix(out["message_id"].(string), "msg_") {
		t.Fatalf("the answer: %v %q", out, why)
	}
	got := arrived("the session's answer", func(m inboxMessage) bool { return m.ID == out["message_id"] })
	if got.InReplyTo != d1.Message.ID || got.From.Inbox != machineID || got.From.Session != id || got.Text != "做完 3 项，还差 1 项" {
		t.Fatalf("Muse got %+v", got)
	}
	// 3. Muse's message is replied, for Muse.
	if st := state(d1.Message.ID); st != "replied" {
		t.Fatalf("state of the message answered: %q", st)
	}
	// It writes to agents only.
	if _, why := host.call(t, "dsh-owners", map[string]any{"text": "x", "to": machineID + "/s_x"}); !strings.Contains(why, "not to sessions") {
		t.Fatalf("to a session: %q", why)
	}

	// 4. DSH Desktop closed: the message waits at the post office, and goes
	// once it is open again.
	host.stop()
	d2 := send("顺便看下 CI")
	if d2.Message.Delivered != "queued" {
		t.Fatalf("with DSH Desktop closed: %+v", d2)
	}
	host = startDSHHostIO(t, home, log)
	host.prompt(t, reqID(d2.Message.ID))
	eventually(t, time.Minute, "the waiting message handed, then handled", func() bool { return state(d2.Message.ID) == "handled" })

	// 5. The owner turns takeover off while one waits: refused, and Muse
	// is told why.
	host.stop()
	d3 := send("还在吗？")
	if d3.Message.Delivered != "queued" {
		t.Fatalf("with DSH Desktop closed: %+v", d3)
	}
	takeover(false)
	host = startDSHHostIO(t, home, log)
	eventually(t, 2*time.Minute, "the message refused", func() bool { return state(d3.Message.ID) == "refused" })
	told := arrived("why it was refused", func(m inboxMessage) bool { return m.InReplyTo == d3.Message.ID })
	if told.Subject != "Not delivered" || !strings.Contains(told.Text, "has not taken over") {
		t.Fatalf("Muse was told %+v", told)
	}
	// Nor does the session write through Aide any more.
	if _, why := host.call(t, "dsh-owners", map[string]any{"text": "x"}); !strings.Contains(why, "not taken over") {
		t.Fatalf("takeover off: %q", why)
	}
}
