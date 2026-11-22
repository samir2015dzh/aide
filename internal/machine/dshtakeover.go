package machine

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
	"github.com/samir2015dzh/aide/internal/shellenv"
)

// Taking over a DSH session (docs/dsh-takeover.md): the owner turns it on
// for one of DSH's own sessions, then sends it messages and stops its turn
// through the Aide bridge plugin in DSH Desktop (dshnative.Bridge),
// which hands them to the DSH Host that owns the session. Nothing here
// writes DSH's files or takes a session's lock. Every call to the bridge
// has a deadline, and every refusal says what to do: nothing is left
// silently pending.

const maxDSHMessage = 100 << 10

var (
	// dshBridgeDial bounds finding and greeting the bridge, and each quick call.
	dshBridgeDial = 3 * time.Second
	// dshPromptWait bounds DSH taking a message (the bridge gives up at 20 s).
	dshPromptWait = 25 * time.Second
)

var dshRequestID = regexp.MustCompile(`^` + dshnative.RequestPrefix + `[0-9a-z]{1,64}$`)

func dshTakeoverKey(id string) string { return "dsh-takeover/" + id }

type dshTakeover struct {
	On bool      `json:"on"`
	At time.Time `json:"at"`
}

// dshTakenOver reports whether the owner took the session over.
func (n *Machine) dshTakenOver(id string) bool {
	b, err := n.st.Get(dshTakeoverKey(id))
	if err != nil || b == nil {
		return false
	}
	var t dshTakeover
	return json.Unmarshal(b, &t) == nil && t.On
}

func ownerOnly(ctx context.Context) error {
	if principalFrom(ctx).Kind != PrincipalOwner {
		return connect.NewError(connect.CodePermissionDenied, errors.New("owner only"))
	}
	return nil
}

func (s *adminService) SetDSHTakeover(ctx context.Context, req *machinev1.SetDSHTakeoverRequest) (*machinev1.SetDSHTakeoverResponse, error) {
	if err := ownerOnly(ctx); err != nil {
		return nil, err
	}
	return s.n.SetDSHTakeover(req)
}

func (s *adminService) SendDSHMessage(ctx context.Context, req *machinev1.SendDSHMessageRequest) (*machinev1.SendDSHMessageResponse, error) {
	if err := ownerOnly(ctx); err != nil {
		return nil, err
	}
	return s.n.SendDSHMessage(ctx, req)
}

func (s *adminService) DSHSessionOptions(ctx context.Context, req *machinev1.DSHSessionOptionsRequest) (*machinev1.DSHSessionOptionsResponse, error) {
	if err := ownerOnly(ctx); err != nil {
		return nil, err
	}
	return s.n.DSHSessionOptions(ctx, req.SessionId)
}

// An agent sends to a DSH session only one it may read (dshVisible) that
// the owner took over; its messages show as its own. It can't turn
// takeover on, nor stop a turn.

func (s *machineService) SendDSHMessage(ctx context.Context, req *machinev1.SendDSHMessageRequest) (*machinev1.SendDSHMessageResponse, error) {
	p := principalFrom(ctx)
	if err := s.n.dshAgentMay(p, req.SessionId); err != nil {
		return nil, err
	}
	return s.n.sendDSHMessage(ctx, req, p)
}

func (s *machineService) DSHSessionOptions(ctx context.Context, req *machinev1.DSHSessionOptionsRequest) (*machinev1.DSHSessionOptionsResponse, error) {
	if err := s.n.dshAgentMay(principalFrom(ctx), req.SessionId); err != nil {
		return nil, err
	}
	return s.n.DSHSessionOptions(ctx, req.SessionId)
}

// dshAgentMay: p may read DSH session id here, and the owner took it over.
func (n *Machine) dshAgentMay(p *Principal, id string) error {
	visible, err := n.dshVisible(p)
	if err != nil {
		return err
	}
	home, err := dshnative.Home(shellenv.Environ())
	if err != nil {
		return err
	}
	s, err := n.dshOwnSession(home, id)
	if connect.CodeOf(err) == connect.CodeNotFound || err == nil && (visible == nil || !visible(s.Cwd)) {
		return cerr(connect.CodeNotFound, "DSH has no session %q here", id)
	}
	if err != nil {
		return err
	}
	if !n.dshTakenOver(id) {
		return cerr(connect.CodeFailedPrecondition, "the owner has not taken over this DSH session: it is read only until they turn on 接管 for it in the Aide app")
	}
	return nil
}

func dshSentKey(reqID string) string { return "dsh-sent/" + reqID }

// dshSent is who sent a message Aide sent into a DSH session, when not
// the owner.
type dshSent struct {
	From string `json:"from"`
	Name string `json:"name"`
	// A message from the post office: the agent's address (c_…,
	// c_…/<conversation>) and the message's id, which the session's answer
	// goes to (dshmail.go).
	Address   string `json:"address,omitempty"`
	MessageID string `json:"message_id,omitempty"`
}

// dshSender names the agent that sent the message under rpcID ("" for the
// owner).
func (n *Machine) dshSender(rpcID string) (from, name string) {
	b, err := n.st.Get(dshSentKey(rpcID))
	var s dshSent
	if err != nil || b == nil || json.Unmarshal(b, &s) != nil {
		return "", ""
	}
	return s.From, s.Name
}

// DSHSessionOptions are the models and permission presets DSH offers a
// taken-over session, through the bridge.
func (n *Machine) DSHSessionOptions(ctx context.Context, id string) (*machinev1.DSHSessionOptionsResponse, error) {
	_, b, err := n.dshTakeoverBridge(ctx, id)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	octx, cancel := context.WithTimeout(ctx, dshPromptWait)
	defer cancel()
	o, err := b.Options(octx, id)
	if err != nil {
		return nil, dshBridgeErr(err)
	}
	res := &machinev1.DSHSessionOptionsResponse{CurrentPermission: o.Current.Permission}
	for _, m := range o.Models {
		res.Models = append(res.Models, &machinev1.DSHModelOption{Provider: m.Provider, ProviderName: m.ProviderName, Model: m.Model, ModelName: m.ModelName, Efforts: m.Efforts})
	}
	for _, p := range o.Permissions {
		res.Permissions = append(res.Permissions, &machinev1.DSHPermissionOption{Id: p.ID, Name: p.Name, Description: p.Description})
	}
	if m := o.Current.Model; m != nil {
		res.CurrentModel = &machinev1.DSHModel{Provider: m.Provider, Model: m.Model, Effort: m.Effort}
	}
	return res, nil
}

func (s *adminService) InterruptDSHSession(ctx context.Context, req *machinev1.InterruptDSHSessionRequest) (*machinev1.InterruptDSHSessionResponse, error) {
	if err := ownerOnly(ctx); err != nil {
		return nil, err
	}
	return s.n.InterruptDSHSession(ctx, req)
}

// dshOwnSession is a DSH session of the owner's that Aide did not start
// (those Aide started take messages as its own sessions do), in home.
func (n *Machine) dshOwnSession(home, id string) (*machinev1.DSHSession, error) {
	s, _, err := dshnative.Read(home, id)
	if err == nil {
		refs, rerr := n.st.ProviderRefs()
		if rerr != nil {
			return nil, rerr
		}
		if refs[s.SessionId] || within(withReal(n.sessions.ws.root), s.Cwd) {
			return nil, cerr(connect.CodeFailedPrecondition, "Aide started DSH session %q: send it messages as any Aide session", id)
		}
	}
	if errors.Is(err, dshnative.ErrNotFound) {
		return nil, cerr(connect.CodeNotFound, "DSH has no session %q here", id)
	}
	return s, err
}

// SetDSHTakeover turns takeover of a DSH session on or off: Aide's own
// setting; DSH is not told.
func (n *Machine) SetDSHTakeover(req *machinev1.SetDSHTakeoverRequest) (*machinev1.SetDSHTakeoverResponse, error) {
	home, err := dshnative.Home(shellenv.Environ())
	if err != nil {
		return nil, err
	}
	s, err := n.dshOwnSession(home, req.SessionId)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(dshTakeover{On: req.On, At: time.Now().UTC()})
	if err := n.st.Set(dshTakeoverKey(req.SessionId), b); err != nil {
		return nil, err
	}
	n.audit.Log(AuditRecord{Kind: "dsh.takeover", Subject: "owner", Session: req.SessionId, Decision: "allow", Detail: "on=" + map[bool]string{true: "true", false: "false"}[req.On]})
	s.Takeover = req.On
	return &machinev1.SetDSHTakeoverResponse{Session: s}, nil
}

// SendDSHMessage sends the owner's message into a taken-over DSH session
// through the bridge: it waits for the next turn there when one runs.
func (n *Machine) SendDSHMessage(ctx context.Context, req *machinev1.SendDSHMessageRequest) (*machinev1.SendDSHMessageResponse, error) {
	return n.sendDSHMessage(ctx, req, &Principal{Kind: PrincipalOwner})
}

// sendDSHMessage is SendDSHMessage from p: an agent's message is recorded
// as its own before it goes (a retry under the same id keeps it).
func (n *Machine) sendDSHMessage(ctx context.Context, req *machinev1.SendDSHMessageRequest, p *Principal) (*machinev1.SendDSHMessageResponse, error) {
	text := req.Text
	switch {
	case strings.TrimSpace(text) == "":
		return nil, cerr(connect.CodeInvalidArgument, "the message is empty")
	case len(text) > maxDSHMessage || !utf8.ValidString(text):
		return nil, cerr(connect.CodeInvalidArgument, "the message is %d bytes; at most %d", len(text), maxDSHMessage)
	}
	reqID := req.RequestId
	if reqID == "" {
		reqID = dshnative.RequestPrefix + strings.ToLower(ulid.Make().String())
	} else if !dshRequestID.MatchString(reqID) {
		return nil, cerr(connect.CodeInvalidArgument, "request_id must be %q and up to 64 lowercase letters or digits", dshnative.RequestPrefix)
	}
	var sel dshnative.Selections
	if m := req.Model; m != nil && (m.Provider != "" || m.Model != "") {
		if m.Provider == "" || m.Model == "" {
			return nil, cerr(connect.CodeInvalidArgument, "a model needs its provider and its name (DSHSessionOptions lists them)")
		}
		sel.Model = &dshnative.Model{Provider: m.Provider, Model: m.Model, Effort: m.Effort}
	}
	sel.Permission = req.Permission
	home, b, err := n.dshTakeoverBridge(ctx, req.SessionId)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	if p.Kind != PrincipalOwner {
		if old, _ := n.st.Get(dshSentKey(reqID)); old != nil {
			if from, _ := n.dshSender(reqID); from != p.ID() {
				return nil, cerr(connect.CodeInvalidArgument, "request_id %q names another sender's message", reqID)
			}
		}
		b, _ := json.Marshal(dshSent{From: p.ID(), Name: p.Name()})
		if err := n.st.Set(dshSentKey(reqID), b); err != nil {
			return nil, err
		}
	}
	// The session's Agent runs in DSH Desktop, or no DSH process has it:
	// DSH Desktop takes the message. Held by another (a DSH run by a
	// command, another DSH window), it would refuse, or hang: say so now.
	sctx, cancel := context.WithTimeout(ctx, dshBridgeDial)
	live, _, err := b.State(sctx, req.SessionId)
	cancel()
	if err != nil {
		return nil, dshBridgeErr(err)
	}
	if !live {
		if held, _, err := dshnative.Held(home, req.SessionId); err == nil && held {
			return nil, cerr(connect.CodeFailedPrecondition, "another DSH program (not DSH Desktop) has this session open; close it there, then send again")
		}
	}
	pctx, cancel := context.WithTimeout(ctx, dshPromptWait)
	defer cancel()
	detail := "request=" + reqID + " message_sha=" + sha(text)
	if sel.Model != nil {
		detail += " model=" + sel.Model.Provider + "/" + sel.Model.Model + "/" + sel.Model.Effort
	}
	if sel.Permission != "" {
		detail += " permission=" + sel.Permission
	}
	if err := b.Prompt(pctx, req.SessionId, reqID, text, sel); err != nil {
		n.audit.Log(AuditRecord{Kind: "dsh.send", Subject: p.ID(), Session: req.SessionId, Decision: "error", Detail: detail + " error=" + err.Error()})
		return nil, dshBridgeErr(err)
	}
	n.audit.Log(AuditRecord{Kind: "dsh.send", Subject: p.ID(), Session: req.SessionId, Decision: "allow", Detail: detail})
	return &machinev1.SendDSHMessageResponse{RequestId: reqID}, nil
}

// InterruptDSHSession stops the current turn of a taken-over DSH session
// that runs in DSH Desktop.
func (n *Machine) InterruptDSHSession(ctx context.Context, req *machinev1.InterruptDSHSessionRequest) (*machinev1.InterruptDSHSessionResponse, error) {
	_, b, err := n.dshTakeoverBridge(ctx, req.SessionId)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	cctx, cancel := context.WithTimeout(ctx, dshBridgeDial)
	defer cancel()
	ok, err := b.Cancel(cctx, req.SessionId)
	if err != nil {
		return nil, dshBridgeErr(err)
	}
	n.audit.Log(AuditRecord{Kind: "dsh.interrupt", Subject: "owner", Session: req.SessionId, Decision: "allow", Detail: map[bool]string{true: "interrupted", false: "not running"}[ok]})
	return &machinev1.InterruptDSHSessionResponse{Interrupted: ok}, nil
}

// dshTakeoverBridge checks the session is the owner's, taken over, and
// returns the DSH home and a connection to its bridge.
func (n *Machine) dshTakeoverBridge(ctx context.Context, id string) (string, *dshnative.Bridge, error) {
	home, err := dshnative.Home(shellenv.Environ())
	if err != nil {
		return "", nil, err
	}
	if _, err := n.dshOwnSession(home, id); err != nil {
		return "", nil, err
	}
	if !n.dshTakenOver(id) {
		return "", nil, cerr(connect.CodeFailedPrecondition, "turn on takeover for this DSH session first")
	}
	dctx, cancel := context.WithTimeout(ctx, dshBridgeDial)
	defer cancel()
	b, err := dshnative.DialBridge(dctx, home)
	if err != nil {
		return "", nil, dshBridgeErr(err)
	}
	return home, b, nil
}

// dshBridgeErr is err as the caller is told it.
func dshBridgeErr(err error) error {
	var be *dshnative.BridgeError
	var ie *dshnative.IncompatibleError
	switch {
	case errors.Is(err, dshnative.ErrBridgeNotInstalled), errors.Is(err, dshnative.ErrBridgeNotRunning), errors.Is(err, dshnative.ErrBridgeTooOld):
		return cerr(connect.CodeFailedPrecondition, "%s", err.Error())
	case errors.As(err, &ie):
		return cerr(connect.CodeFailedPrecondition, "%s", ie.Reason)
	case errors.As(err, &be):
		switch be.Code {
		case "timeout":
			return cerr(connect.CodeDeadlineExceeded, "DSH did not take the message in time; send it again (it is not sent twice): %s", be.Message)
		case "not-found":
			return cerr(connect.CodeNotFound, "DSH Desktop has no such session: %s", be.Message)
		case "bad-request":
			return cerr(connect.CodeInvalidArgument, "%s", be.Message)
		case "busy":
			return cerr(connect.CodeFailedPrecondition, "DSH is not taking messages for this session now: %s", be.Message)
		case "held":
			return cerr(connect.CodeFailedPrecondition, "another DSH program has this session open; close it there, then send again (%s)", be.Message)
		default:
			return cerr(connect.CodeFailedPrecondition, "DSH refused: %s", be.Message)
		}
	}
	return err
}

func (s *adminService) PrepareDSHBridge(ctx context.Context, _ *machinev1.PrepareDSHBridgeRequest) (*machinev1.PrepareDSHBridgeResponse, error) {
	if err := ownerOnly(ctx); err != nil {
		return nil, err
	}
	return s.n.PrepareDSHBridge()
}

// PrepareDSHBridge backs up DSH's configuration and writes the bridge
// plugin's package into the DSH home, for DSH Desktop to install
// (dshnative.PrepareBridge).
func (n *Machine) PrepareDSHBridge() (*machinev1.PrepareDSHBridgeResponse, error) {
	home, err := dshnative.Home(shellenv.Environ())
	if err != nil {
		return nil, err
	}
	p, err := dshnative.PrepareBridge(home, time.Now())
	if err != nil {
		return nil, cerr(connect.CodeFailedPrecondition, "%s", err.Error())
	}
	n.audit.Log(AuditRecord{Kind: "dsh.bridge.prepare", Subject: "owner", Decision: "allow", Detail: "package=" + p.Package + " backup=" + p.Backup})
	return &machinev1.PrepareDSHBridgeResponse{PackagePath: p.Package, Version: p.Version, BackupDir: p.Backup, BackedUp: p.BackedUp, DesktopProfile: p.DesktopProfile}, nil
}

// dshBridge is the bridge's state in the DSH home, as ListDSHSessions
// shows it.
func dshBridge(ctx context.Context, home string) *machinev1.DSHBridge {
	ctx, cancel := context.WithTimeout(ctx, dshBridgeDial)
	defer cancel()
	b, err := dshnative.DialBridge(ctx, home)
	var ie *dshnative.IncompatibleError
	switch {
	case err == nil:
		b.Close()
		return &machinev1.DSHBridge{State: machinev1.DSHBridgeState_DSH_BRIDGE_STATE_RUNNING, Version: b.Hello.Version}
	case errors.Is(err, dshnative.ErrBridgeNotInstalled):
		return &machinev1.DSHBridge{State: machinev1.DSHBridgeState_DSH_BRIDGE_STATE_NOT_INSTALLED, Detail: err.Error()}
	case errors.As(err, &ie):
		return &machinev1.DSHBridge{State: machinev1.DSHBridgeState_DSH_BRIDGE_STATE_INCOMPATIBLE, Detail: ie.Reason}
	default:
		return &machinev1.DSHBridge{State: machinev1.DSHBridgeState_DSH_BRIDGE_STATE_NOT_RUNNING, Detail: dshnative.ErrBridgeNotRunning.Error()}
	}
}
