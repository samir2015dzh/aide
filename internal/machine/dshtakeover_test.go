package machine

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	machinev1 "github.com/hermeshai/hermesh/gen/go/hermesh/machine/v1"
	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
	"github.com/hermeshai/hermesh/internal/localipc"
	"github.com/hermeshai/hermesh/internal/provider/acp/dshnative"
)

// fakeBridge speaks the Hermesh bridge plugin's protocol (apps/dsh-bridge)
// for a DSH home, as the plugin in DSH Desktop would.
type fakeBridge struct {
	mu       sync.Mutex
	ln       net.Listener
	live     map[string]bool
	prompts  []map[string]string
	cancels  []string
	hang     bool   // never answers a prompt
	refuse   string // answers a prompt with this error code
	protocol int
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
	f := &fakeBridge{ln: ln, live: map[string]bool{}, protocol: 1}
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
			ID     int               `json:"id"`
			Token  string            `json:"token"`
			Method string            `json:"method"`
			Params map[string]string `json:"params"`
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
			reply(map[string]any{"result": map[string]any{"protocol": f.protocol, "version": "0.1.0", "services": map[string]bool{"sessionController": true, "agents": true}}})
		case "state":
			reply(map[string]any{"result": map[string]any{"live": f.live[req.Params["sessionId"]], "status": ""}})
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
			ok := f.live[req.Params["sessionId"]]
			if ok {
				f.cancels = append(f.cancels, req.Params["sessionId"])
			}
			reply(map[string]any{"result": map[string]any{"cancelled": ok}})
		}
		f.mu.Unlock()
	}
}

func codeOf(err error) connect.Code { return connect.CodeOf(err) }

// Taking over a DSH session: off until the owner turns it on; then the
// message goes to the bridge in queue mode under a hermesh- request id;
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
	if err != nil || !strings.HasPrefix(r.RequestId, "hermesh-") {
		t.Fatalf("sent: %v %v", r, err)
	}
	if len(b.prompts) != 1 || b.prompts[0]["text"] != "继续" || b.prompts[0]["requestId"] != r.RequestId || b.prompts[0]["sessionId"] != "owners" {
		t.Fatalf("the bridge got %v", b.prompts)
	}
	// A retry names the same message.
	if r2, err := send("继续", r.RequestId); err != nil || r2.RequestId != r.RequestId {
		t.Fatalf("retry: %v %v", r2, err)
	}
	for _, bad := range []string{"other-1", "hermesh-UPPER", "hermesh-" + strings.Repeat("a", 65)} {
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
	b.refuse = "busy"
	if _, err := send("x", ""); codeOf(err) != connect.CodeFailedPrecondition || !strings.Contains(err.Error(), "not taking messages") {
		t.Errorf("busy: %v", err)
	}
	b.refuse, b.hang = "", true
	dshPromptWait = 300 * time.Millisecond
	t.Cleanup(func() { dshPromptWait = 25 * time.Second })
	t0 := time.Now()
	if _, err := send("x", ""); codeOf(err) != connect.CodeDeadlineExceeded || !strings.Contains(err.Error(), "not sent twice") {
		t.Errorf("hanging: %v", err)
	}
	if time.Since(t0) > 3*time.Second {
		t.Errorf("waited %v", time.Since(t0))
	}
	b.hang = false

	// Interrupt: only a turn running in DSH Desktop.
	if ir, err := h.n.InterruptDSHSession(ctx, &machinev1.InterruptDSHSessionRequest{SessionId: "owners"}); err != nil || ir.Interrupted {
		t.Fatalf("interrupting a session DSH Desktop doesn't run: %v %v", ir, err)
	}
	b.live["owners"] = true
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

// A session Hermesh started is not taken over: it takes messages as any
// of Hermesh's sessions do.
func TestDSHTakeoverNotOfHermeshSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DSH_HOME", home)
	h := newHarness(t, Limits{})
	id := h.start("hello")
	ref := h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE).ProviderSessionRef
	writeDSHSession(t, home, ref, "/somewhere", 1)
	if _, err := h.n.SetDSHTakeover(&machinev1.SetDSHTakeoverRequest{SessionId: ref, On: true}); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("taking over Hermesh's own: %v", err)
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
