package machine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/localipc"
	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
)

// fakeBridge speaks the Aide bridge plugin's protocol (apps/dsh-bridge)
// for a DSH home, as the plugin in DSH Desktop would.
type fakeBridge struct {
	mu       sync.Mutex
	ln       net.Listener
	live     map[string]bool
	prompts  []map[string]any
	cancels  []string
	hang     bool   // never answers a prompt; set under mu
	refuse   string // answers a prompt with this error code; set under mu
	protocol int
	features []string // as the plugin's hello lists them; nil: an older plugin
}

func startFakeBridge(t *testing.T, home string) *fakeBridge {
	t.Helper()
	dir := dshnative.BridgeDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A Unix socket, or on Windows a named pipe, as the plugin listens on.
	at := filepath.Join(dir, "bridge.sock")
	ln, err := localipc.Listen(at)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := at
	if runtime.GOOS == "windows" { // the file holds the pipe's name
		b, _ := os.ReadFile(at)
		endpoint = strings.TrimSpace(string(b))
	}
	f := &fakeBridge{ln: ln, live: map[string]bool{}, protocol: 1, features: []string{"options", "selections"}}
	t.Cleanup(func() { ln.Close() })
	rv, _ := json.Marshal(map[string]any{"protocol": 1, "endpoint": endpoint, "token": "tok", "pid": 1, "version": "0.1.0"})
	if err := os.WriteFile(filepath.Join(dir, "bridge.json"), rv, 0o600); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeBridge) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req struct {
			ID     int            `json:"id"`
			Token  string         `json:"token"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		json.Unmarshal(line, &req)
		reply := func(body map[string]any) {
			body["id"] = req.ID
			b, _ := json.Marshal(body)
			c.Write(append(b, '\n'))
		}
		if req.Token != "tok" {
			reply(map[string]any{"error": map[string]string{"code": "unauthorized", "message": "wrong token"}})
			return
		}
		f.mu.Lock()
		switch req.Method {
		case "hello":
			reply(map[string]any{"result": map[string]any{"protocol": f.protocol, "version": "0.1.0", "services": map[string]bool{"sessionController": true, "agents": true}, "features": f.features}})
		case "state":
			reply(map[string]any{"result": map[string]any{"live": f.live[req.Params["sessionId"].(string)], "status": ""}})
		case "options":
			reply(map[string]any{"result": map[string]any{
				"models":      []any{map[string]any{"provider": "deepseek", "providerName": "DeepSeek", "model": "v4", "modelName": "V4", "efforts": []string{"high"}}},
				"permissions": []any{map[string]any{"id": "ask", "name": "Ask", "description": ""}, map[string]any{"id": "auto", "name": "Auto", "description": "reviewed"}},
				"current":     map[string]any{"model": map[string]any{"provider": "deepseek", "model": "v4"}, "permission": "ask"},
			}})
		case "prompt":
			switch {
			case f.hang:
			case f.refuse != "":
				reply(map[string]any{"error": map[string]string{"code": f.refuse, "message": "session/agent-busy"}})
			default:
				f.prompts = append(f.prompts, req.Params)
				reply(map[string]any{"result": map[string]any{"accepted": true}})
			}
		case "cancel":
			id, _ := req.Params["sessionId"].(string)
			ok := f.live[id]
			if ok {
				f.cancels = append(f.cancels, id)
			}
			reply(map[string]any{"result": map[string]any{"cancelled": ok}})
		}
		f.mu.Unlock()
	}
}

func codeOf(err error) connect.Code { return connect.CodeOf(err) }

// Taking over a DSH session: off until the owner turns it on; then the
// message goes to the bridge in queue mode under an aide- request id;
// every way it can't go says so, with nothing sent.
func TestDSHTakeover(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	h := newHarness(t, Limits{})
	writeDSHSession(t, home, "owners", "/home/alex/app", 1)
	ctx := context.Background()
	send := func(text, reqID string) (*machinev1.SendDSHMessageResponse, error) {
		return h.n.SendDSHMessage(ctx, &machinev1.SendDSHMessageRequest{SessionId: "owners", Text: text, RequestId: reqID})
	}

	// Off by default, for each session, and in the list.
	if _, err := send("hi", ""); codeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "turn on takeover") {
		t.Fatalf("before takeover: %v", err)
	}
	list, err := h.n.DSHSessions(ctx)
	if err != nil || list.Sessions[0].Takeover {
		t.Fatalf("listed: %v %v", list, err)
	}
	if list.Bridge.GetState() != machinev1.DSHBridgeState_DSH_BRIDGE_STATE_NOT_INSTALLED {
		t.Fatalf("bridge before it ran: %v", list.Bridge)
	}
	res, err := h.n.SetDSHTakeover(&machinev1.SetDSHTakeoverRequest{SessionId: "owners", On: true})
	if err != nil || !res.Session.Takeover {
		t.Fatalf("turning on: %v %v", res, err)
	}
	if _, err := h.n.SetDSHTakeover(&machinev1.SetDSHTakeoverRequest{SessionId: "nope", On: true}); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("an unknown session: %v", err)
	}

	// No bridge yet: what to do.
	if _, err := send("hi", ""); codeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "install it") {
		t.Fatalf("not installed: %v", err)
	}

	b := startFakeBridge(t, home)
	if l, _ := h.n.DSHSessions(ctx); l.Bridge.GetState() != machinev1.DSHBridgeState_DSH_BRIDGE_STATE_RUNNING || l.Bridge.Version != "0.1.0" || !l.Sessions[0].Takeover {
		t.Fatalf("listed with the bridge: %v", l)
	}
	r, err := send("继续", "")
	if err != nil || !strings.HasPrefix(r.RequestId, "aide-") {
		t.Fatalf("sent: %v %v", r, err)
	}
	if len(b.prompts) != 1 || b.prompts[0]["text"] != "继续" || b.prompts[0]["requestId"] != r.RequestId || b.prompts[0]["sessionId"] != "owners" || b.prompts[0]["model"] != nil {
		t.Fatalf("the bridge got %v", b.prompts)
	}
	// A retry names the same message.
	if r2, err := send("继续", r.RequestId); err != nil || r2.RequestId != r.RequestId {
		t.Fatalf("retry: %v %v", r2, err)
	}
	for _, bad := range []string{"other-1", "aide-UPPER", "aide-" + strings.Repeat("a", 65)} {
		if _, err := send("x", bad); codeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("request id %q: %v", bad, err)
		}
	}
	if _, err := send("  ", ""); codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("empty: %v", err)
	}
	if _, err := send(strings.Repeat("x", 100<<10+1), ""); codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("too long: %v", err)
	}

	// DSH's refusal, and a DSH that never takes it: said, not left pending.
	b.mu.Lock()
	b.refuse = "busy"
	b.mu.Unlock()
	if _, err := send("x", ""); codeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "not taking messages") {
		t.Errorf("busy: %v", err)
	}
	b.mu.Lock()
	b.refuse, b.hang = "", true
	b.mu.Unlock()
	dshPromptWait = 300 * time.Millisecond
	t.Cleanup(func() { dshPromptWait = 25 * time.Second })
	t0 := time.Now()
	if _, err := send("x", ""); codeOf(err) != connect.CodeDeadlineExceeded || !strings.Contains(err.Error(), "not sent twice") {
		t.Errorf("hanging: %v", err)
	}
	if time.Since(t0) > 3*time.Second {
		t.Errorf("waited %v", time.Since(t0))
	}
	b.mu.Lock()
	b.hang = false
	b.mu.Unlock()

	// Interrupt: only a turn running in DSH Desktop.
	if ir, err := h.n.InterruptDSHSession(ctx, &machinev1.InterruptDSHSessionRequest{SessionId: "owners"}); err != nil || ir.Interrupted {
		t.Fatalf("interrupting a session DSH Desktop doesn't run: %v %v", ir, err)
	}
	b.mu.Lock()
	b.live["owners"] = true
	b.mu.Unlock()
	if ir, err := h.n.InterruptDSHSession(ctx, &machinev1.InterruptDSHSessionRequest{SessionId: "owners"}); err != nil || !ir.Interrupted || len(b.cancels) != 1 {
		t.Fatalf("interrupting: %v %v %v", ir, err, b.cancels)
	}

	// Off again: refused, the bridge untouched.
	h.n.SetDSHTakeover(&machinev1.SetDSHTakeoverRequest{SessionId: "owners", On: false})
	before := len(b.prompts)
	if _, err := send("x", ""); codeOf(err) != connect.CodeFailedPrecondition || len(b.prompts) != before {
		t.Fatalf("after turning off: %v", err)
	}

	// The bridge closed (DSH Desktop quit): not running.
	b.ln.Close()
	if l, _ := h.n.DSHSessions(ctx); l.Bridge.GetState() != machinev1.DSHBridgeState_DSH_BRIDGE_STATE_NOT_RUNNING {
		t.Fatalf("after the bridge went: %v", l.Bridge)
	}
}

// A session Aide started is not taken over: it takes messages as any
// of Aide's sessions do.
func TestDSHTakeoverNotOfAideSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	h := newHarness(t, Limits{})
	id := h.start("hello")
	ref := h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE).ProviderSessionRef
	writeDSHSession(t, home, ref, "/somewhere", 1)
	if _, err := h.n.SetDSHTakeover(&machinev1.SetDSHTakeoverRequest{SessionId: ref, On: true}); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("taking over Aide's own: %v", err)
	}
}

// Only the owner takes over.
func TestDSHTakeoverOwnerOnly(t *testing.T) {
	ctx := context.WithValue(context.Background(), principalKey{}, &Principal{Kind: PrincipalClient, ClientID: "c_bot"})
	s := &adminService{}
	if _, err := s.SetDSHTakeover(ctx, &machinev1.SetDSHTakeoverRequest{SessionId: "x", On: true}); codeOf(err) != connect.CodePermissionDenied {
		t.Errorf("takeover by an agent: %v", err)
	}
	if _, err := s.SendDSHMessage(ctx, &machinev1.SendDSHMessageRequest{SessionId: "x", Text: "x"}); codeOf(err) != connect.CodePermissionDenied {
		t.Errorf("send by an agent: %v", err)
	}
	if _, err := s.InterruptDSHSession(ctx, &machinev1.InterruptDSHSessionRequest{SessionId: "x"}); codeOf(err) != connect.CodePermissionDenied {
		t.Errorf("interrupt by an agent: %v", err)
	}
}

// An agent sends to a DSH session only one it may read that the owner took
// over; its message is recorded as its own, and read back so. A message
// may set the session's model and permission preset first (the bridge's
// options say which), and an older plugin that can't is refused before
// anything goes.
func TestDSHTakeoverByAgentAndSelections(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	h := newHarness(t, Limits{})
	writeDSHSession(t, home, "owners", "/home/alex/app", 1)
	b := startFakeBridge(t, home)
	ctx := context.Background()
	agent := func(vis identityv1.TaskVisibility) (context.Context, *machineService) {
		p := &Principal{Kind: PrincipalClient, ClientID: "c_muse", Grant: &identityv1.Grant{ClientId: "c_muse", ClientLabel: "Muse", TaskVisibility: vis, ProjectIds: []string{"*"}}}
		return context.WithValue(ctx, principalKey{}, p), &machineService{n: h.n}
	}
	actx, svc := agent(identityv1.TaskVisibility_TASK_VISIBILITY_ALL)
	send := func(req *machinev1.SendDSHMessageRequest) (*machinev1.SendDSHMessageResponse, error) {
		req.SessionId = "owners"
		return svc.SendDSHMessage(actx, req)
	}

	// Not taken over: read only, for the agent too.
	if _, err := send(&machinev1.SendDSHMessageRequest{Text: "hi"}); codeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "has not taken over") {
		t.Fatalf("before takeover: %v", err)
	}
	if _, err := h.n.SetDSHTakeover(&machinev1.SetDSHTakeoverRequest{SessionId: "owners", On: true}); err != nil {
		t.Fatal(err)
	}
	// An agent that sees only its own sessions sees no DSH session.
	octx, own := agent(identityv1.TaskVisibility_TASK_VISIBILITY_OWN)
	if _, err := own.SendDSHMessage(octx, &machinev1.SendDSHMessageRequest{SessionId: "owners", Text: "hi"}); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("an agent that can't read it: %v", err)
	}

	// Its options, then a message that sets the model and permission first.
	o, err := svc.DSHSessionOptions(actx, &machinev1.DSHSessionOptionsRequest{SessionId: "owners"})
	if err != nil || len(o.Models) != 1 || o.Models[0].Model != "v4" || len(o.Permissions) != 2 || o.CurrentPermission != "ask" || o.CurrentModel.GetModel() != "v4" {
		t.Fatalf("options: %v %v", o, err)
	}
	r, err := send(&machinev1.SendDSHMessageRequest{Text: "接着做", Model: &machinev1.DSHModel{Provider: "deepseek", Model: "v4", Effort: "high"}, Permission: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	got := b.prompts[len(b.prompts)-1]
	if got["requestId"] != r.RequestId || got["permission"] != "auto" || fmt.Sprint(got["model"]) != "map[effort:high model:v4 provider:deepseek]" {
		t.Fatalf("the bridge got %v", got)
	}
	// Half a model: refused here.
	if _, err := send(&machinev1.SendDSHMessageRequest{Text: "x", Model: &machinev1.DSHModel{Model: "v4"}}); codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("a model without its provider: %v", err)
	}
	// Another agent can't reuse its request id.
	other := &Principal{Kind: PrincipalClient, ClientID: "c_other", Grant: &identityv1.Grant{ClientId: "c_other", TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_ALL, ProjectIds: []string{"*"}}}
	if _, err := svc.SendDSHMessage(context.WithValue(ctx, principalKey{}, other), &machinev1.SendDSHMessageRequest{SessionId: "owners", Text: "x", RequestId: r.RequestId}); codeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("another's request id: %v", err)
	}

	// DSH writes the messages with their request ids: the agent's reads as its own, the owner's as the owner's.
	owner, err := h.n.SendDSHMessage(ctx, &machinev1.SendDSHMessageRequest{SessionId: "owners", Text: "我也说一句"})
	if err != nil {
		t.Fatal(err)
	}
	appendDSHPrompts(t, home, "owners", "/home/alex/app", [][2]string{{r.RequestId, "接着做"}, {owner.RequestId, "我也说一句"}, {"", "在 DSH 里打的"}})
	res, err := h.n.ReadDSHSession(&machinev1.ReadDSHSessionRequest{SessionId: "owners", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var who []string
	for _, e := range res.Events {
		if um := e.GetUserMessage(); um != nil {
			who = append(who, um.Text+" ← "+um.From+"/"+um.FromName)
		}
	}
	if want := "接着做 ← c_muse/Muse; 我也说一句 ← owner/; 在 DSH 里打的 ← /"; strings.Join(who, "; ") != want {
		t.Fatalf("senders: %s, want %s", strings.Join(who, "; "), want)
	}

	// An older plugin: options and selections refused, nothing sent; a plain message still goes.
	b.features = nil
	before := len(b.prompts)
	if _, err := send(&machinev1.SendDSHMessageRequest{Text: "x", Permission: "auto"}); codeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "older") || len(b.prompts) != before {
		t.Fatalf("selections to an older plugin: %v", err)
	}
	if _, err := svc.DSHSessionOptions(actx, &machinev1.DSHSessionOptionsRequest{SessionId: "owners"}); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("options from an older plugin: %v", err)
	}
	if _, err := send(&machinev1.SendDSHMessageRequest{Text: "plain"}); err != nil {
		t.Fatalf("a plain message to an older plugin: %v", err)
	}
}

// appendDSHPrompts adds a turn per prompt ({request id, text}) to a
// session's log, as DSH logs them: the request id as the source's rpcId
// ("" for one typed in DSH).
func appendDSHPrompts(t *testing.T, home, id, cwd string, prompts [][2]string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", "--"+strings.Trim(strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(cwd), "-")+"--", id)
	f, err := os.OpenFile(filepath.Join(dir, "session.v4.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	seq := 100
	for _, p := range prompts {
		src := map[string]any{"kind": "user"}
		if p[0] != "" {
			src["rpcId"] = p[0]
		}
		for _, l := range []map[string]any{
			{"type": "turn/start", "seq": seq, "time": 10, "data": map[string]any{"turn": seq}},
			{"type": "user/message", "seq": seq + 1, "time": 10, "surfaceOp": "append", "data": map[string]any{"id": fmt.Sprint("m", seq), "role": "user", "content": []any{map[string]any{"type": "text", "text": p[1]}}, "source": src}},
			{"type": "turn/end", "seq": seq + 2, "time": 10, "data": map[string]any{"turn": seq, "reason": map[string]any{"kind": "completed"}}},
		} {
			j, _ := json.Marshal(l)
			f.Write(append(j, '\n'))
		}
		seq += 3
	}
}
