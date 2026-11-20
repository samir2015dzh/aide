package dshnative

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hermeshai/hermesh/internal/legacy"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hermeshai/hermesh/internal/localipc"
)

// The Hermesh bridge plugin in DSH Desktop (apps/dsh-bridge,
// docs/dsh-takeover.md): it hands the owner's messages to the DSH Host that
// owns a session, in-process, so Hermesh never writes a DSH session itself.
// It publishes where it listens, and a token, in BridgeDir(home)/bridge.json.

// BridgeProtocol is the protocol version this client speaks.
const BridgeProtocol = 1

// BridgeDir is where the bridge of the DSH home meets aided.
func BridgeDir(home string) string { return filepath.Join(home, "aide-bridge") }

// rendezvousFile is the bridge's bridge.json: the plugin's, or while only
// that one exists, the one of the plugin installed before the rename
// (internal/legacy), which runs until DSH Desktop has the new one.
func rendezvousFile(home string) string {
	f := filepath.Join(BridgeDir(home), "bridge.json")
	if _, err := os.Stat(f); err != nil {
		if old := filepath.Join(home, legacy.DSHBridgeDir, "bridge.json"); fileExists(old) {
			return old
		}
	}
	return f
}

func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }

var (
	// ErrBridgeNotInstalled: no bridge has ever run in this DSH home.
	ErrBridgeNotInstalled = errors.New("the Aide plugin has not run in DSH Desktop here: install it, then restart DSH Desktop")
	// ErrBridgeNotRunning: one ran, and does not answer now.
	ErrBridgeNotRunning = errors.New("the Aide plugin in DSH Desktop does not answer: open DSH Desktop (and check the plugin is on in its Plugins page)")
)

// BridgeError is a refusal the bridge (or DSH behind it) answered with:
// Code is "timeout", "busy", "held", "not-found", "unavailable",
// "bad-request" or "failed".
type BridgeError struct{ Code, Message string }

func (e *BridgeError) Error() string { return e.Message }

// IncompatibleError: the bridge answers in a way this client can't use.
type IncompatibleError struct{ Reason string }

func (e *IncompatibleError) Error() string { return e.Reason }

// Hello is the bridge's answer to hello.
type Hello struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`
	PID      int    `json:"pid"`
	Services struct {
		SessionController bool `json:"sessionController"`
		Agents            bool `json:"agents"`
	} `json:"services"`
}

// Bridge is one connection to the bridge. Not for concurrent use.
type Bridge struct {
	conn  net.Conn
	token string
	r     *bufio.Reader
	mu    sync.Mutex
	n     int
	Hello Hello
}

type rendezvous struct {
	Protocol int    `json:"protocol"`
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Version  string `json:"version"`
}

// DialBridge connects to the bridge of the DSH home and greets it, within
// ctx (give it a deadline: a bridge that does not answer is reported as not
// running). The bridge must have a session controller to be of use.
func DialBridge(ctx context.Context, home string) (*Bridge, error) {
	b, err := os.ReadFile(rendezvousFile(home))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrBridgeNotInstalled
	}
	if err != nil {
		return nil, err
	}
	var rv rendezvous
	if err := json.Unmarshal(b, &rv); err != nil || rv.Endpoint == "" || rv.Token == "" {
		return nil, &IncompatibleError{Reason: "the Aide plugin's bridge.json can't be read; reinstall the plugin"}
	}
	if rv.Protocol != BridgeProtocol {
		return nil, &IncompatibleError{Reason: fmt.Sprintf("the Aide plugin in DSH Desktop speaks protocol %d, this Aide %d: update the plugin", rv.Protocol, BridgeProtocol)}
	}
	conn, err := localipc.Dial(ctx, rv.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrBridgeNotRunning, err)
	}
	c := &Bridge{conn: conn, token: rv.Token, r: bufio.NewReaderSize(conn, 64<<10)}
	if err := c.call(ctx, "hello", nil, &c.Hello); err != nil {
		conn.Close()
		var be *BridgeError
		if errors.As(err, &be) {
			return nil, &IncompatibleError{Reason: "the Aide plugin refused: " + be.Message}
		}
		return nil, fmt.Errorf("%w (%v)", ErrBridgeNotRunning, err)
	}
	if c.Hello.Protocol != BridgeProtocol {
		conn.Close()
		return nil, &IncompatibleError{Reason: fmt.Sprintf("the Aide plugin speaks protocol %d, this Aide %d: update the plugin", c.Hello.Protocol, BridgeProtocol)}
	}
	if !c.Hello.Services.SessionController {
		conn.Close()
		return nil, &IncompatibleError{Reason: "the Aide plugin runs in a DSH profile without a session controller: install it in DSH Desktop's profile"}
	}
	return c, nil
}

func (c *Bridge) Close() error { return c.conn.Close() }

// State is the DSH Host's own view of the session: live, its Agent runs
// there (DSH Desktop has it open), with status its word for it ("running",
// "idle").
func (c *Bridge) State(ctx context.Context, sessionID string) (live bool, status string, err error) {
	var r struct {
		Live   bool   `json:"live"`
		Status string `json:"status"`
	}
	err = c.call(ctx, "state", map[string]string{"sessionId": sessionID}, &r)
	return r.Live, r.Status, err
}

// Prompt queues text as the owner's next message in the session, named by
// requestID (DSH drops a second one with the same id).
func (c *Bridge) Prompt(ctx context.Context, sessionID, requestID, text string) error {
	return c.call(ctx, "prompt", map[string]string{"sessionId": sessionID, "requestId": requestID, "text": text}, nil)
}

// Cancel stops the session's current turn; false when no Agent runs it in
// the Host.
func (c *Bridge) Cancel(ctx context.Context, sessionID string) (bool, error) {
	var r struct {
		Cancelled bool `json:"cancelled"`
	}
	err := c.call(ctx, "cancel", map[string]string{"sessionId": sessionID}, &r)
	return r.Cancelled, err
}

func (c *Bridge) call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	req, err := json.Marshal(map[string]any{"id": c.n, "token": c.token, "method": method, "params": params})
	if err != nil {
		return err
	}
	if d, ok := ctx.Deadline(); ok {
		c.conn.SetDeadline(d)
	} else {
		c.conn.SetDeadline(time.Time{})
	}
	stop := context.AfterFunc(ctx, func() { c.conn.SetDeadline(time.Now()) })
	defer stop()
	if _, err := c.conn.Write(append(req, '\n')); err != nil {
		return c.ctxErr(ctx, err)
	}
	for {
		line, err := c.r.ReadBytes('\n')
		if err != nil {
			return c.ctxErr(ctx, err)
		}
		var res struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(line, &res); err != nil {
			return fmt.Errorf("the Aide plugin answered something unreadable: %w", err)
		}
		if res.ID != c.n {
			continue // an answer to a call given up on earlier
		}
		if res.Error != nil {
			return &BridgeError{Code: res.Error.Code, Message: res.Error.Message}
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(res.Result, result)
	}
}

// ctxErr is err, or a timeout BridgeError when ctx ran out meanwhile.
func (c *Bridge) ctxErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return &BridgeError{Code: "timeout", Message: "the Aide plugin in DSH Desktop did not answer in time"}
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return &BridgeError{Code: "timeout", Message: "the Aide plugin in DSH Desktop did not answer in time"}
	}
	return err
}

// Held reports whether a DSH process holds the session's write lock (it
// has the session open), as List does, without taking it; known is false
// where this system can't tell. ErrNotFound when the home has no such
// session.
func Held(home, id string) (held, known bool, err error) {
	dir, err := sessionDir(home, id)
	if err != nil {
		return false, false, err
	}
	held, known = lockProbe()(dir)
	return held, known, nil
}

// sessionDir is the folder of the session id in the home.
func sessionDir(home, id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return "", ErrNotFound
	}
	projects, err := os.ReadDir(filepath.Join(home, "sessions"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	for _, p := range projects {
		dir := filepath.Join(home, "sessions", p.Name(), encodeSegment(id))
		if path, err := logFile(dir); err == nil && path != "" {
			return dir, nil
		}
	}
	return "", ErrNotFound
}
