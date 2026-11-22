package machine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"google.golang.org/protobuf/types/known/timestamppb"

	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
	"github.com/samir2015dzh/aide/internal/shellenv"
)

// Messages between the owner's agents and the DSH sessions the owner took
// over (docs/agent-messages.md §15.3). An agent writes to m_…/dsh_… as to
// any session: the message waits at the post office, sealed, until this
// machine takes it, and goes into the session through the Aide plugin in
// DSH Desktop; while DSH Desktop does not run it stays there (taken, not
// lost) and goes once the plugin answers again. The session answers with
// the aide_message_send tool the plugin registers: the plugin hands the
// send to this machine, which sends it as the machine, from m_…/dsh_…,
// to an agent only. Takeover stays the gate both ways.

const (
	// dshOutPerHour: what one DSH session may send through Aide in an hour,
	// as an Aide session (docs/agent-messages.md §5).
	dshOutPerHour = 30
	// dshHandledWatch: how long a message handed to DSH is watched for its
	// turn's end (then it is handled).
	dshHandledWatch = 2 * time.Hour
)

// dshAddress is the session part of the address of DSH's session dshID on
// the machine machineID (as `aide session list --native` lists it).
func dshAddress(machineID, dshID string) string {
	m := strings.TrimPrefix(machineID, "m_")
	return "dsh_" + m[:min(8, len(m))] + "_" + dshID
}

// dshOf is DSH's id of the session a message to this machine names, "" when
// it names no DSH session here.
func (n *Machine) dshOf(session string) string {
	parts := strings.SplitN(session, "_", 3)
	if len(parts) < 3 || parts[0] != "dsh" || dshAddress(n.machineID, parts[2]) != session {
		return ""
	}
	return parts[2]
}

func dshLastKey(id string) string { return "dsh-last/" + id }

// later: a message the machine keeps at the post office, to go when DSH
// Desktop's plugin answers again.
func later(format string, a ...any) error { return cerr(connect.CodeUnavailable, format, a...) }

// deliverDSHLetter hands a checked message from an agent to the DSH session
// it is for, through the plugin: "handed" once DSH queued it.
func (n *Machine) deliverDSHLetter(ctx context.Context, p *Principal, m *inboxv1.Message, name string, atts []*sessionv1.Attachment) (string, error) {
	id := n.dshOf(m.To.Session)
	switch {
	case id == "":
		return "", cerr(connect.CodeNotFound, "no DSH session %s on this machine", m.To.Session)
	case len(atts) > 0:
		return "", cerr(connect.CodeInvalidArgument, "a DSH session takes text only, no files")
	case m.Fyi:
		return "", cerr(connect.CodeInvalidArgument, "a DSH session takes no message for information only: every message is a turn there")
	case len(m.Text) > maxDSHMessage:
		return "", cerr(connect.CodeInvalidArgument, "the message is %d bytes; a DSH session takes at most %d", len(m.Text), maxDSHMessage)
	}
	if err := n.dshAgentMay(p, id); err != nil {
		return "", err
	}
	home, err := dshnative.Home(shellenv.Environ())
	if err != nil {
		return "", err
	}
	dctx, cancel := context.WithTimeout(ctx, dshBridgeDial)
	b, err := dshnative.DialBridge(dctx, home)
	cancel()
	var ie *dshnative.IncompatibleError
	switch {
	case errors.As(err, &ie):
		return "", cerr(connect.CodeFailedPrecondition, "%s", ie.Reason)
	case err != nil:
		n.dshWaiting()
		return "", later("DSH Desktop is not running: delivered once its Aide plugin answers")
	}
	defer b.Close()
	reqID := dshnative.RequestPrefix + strings.ToLower(strings.TrimPrefix(m.MessageId, "msg_"))
	rec, _ := json.Marshal(dshSent{From: p.ID(), Name: name, Address: inboxAddress(m.From), MessageID: m.MessageId})
	if err := n.st.Set(dshSentKey(reqID), rec); err != nil {
		return "", err
	}
	sctx, cancel := context.WithTimeout(ctx, dshBridgeDial)
	live, _, err := b.State(sctx, id)
	cancel()
	if err != nil {
		n.dshWaiting()
		return "", later("DSH Desktop's Aide plugin did not answer: %v", err)
	}
	if !live {
		if held, _, err := dshnative.Held(home, id); err == nil && held {
			n.dshWaiting()
			return "", later("another DSH program (not DSH Desktop) has this session open: delivered once it is closed there")
		}
	}
	pctx, cancel := context.WithTimeout(ctx, dshPromptWait)
	defer cancel()
	detail := "request=" + reqID + " message=" + m.MessageId + " message_sha=" + sha(m.Text)
	if err := b.PromptFrom(pctx, id, reqID, m.Text, dshnative.From{Address: inboxAddress(m.From), Name: name}, m.MessageId); err != nil {
		n.audit.Log(AuditRecord{Kind: "dsh.message", Subject: p.ID(), Session: id, Decision: "error", Detail: detail + " error=" + err.Error()})
		var be *dshnative.BridgeError
		if errors.As(err, &be) && (be.Code == "timeout" || be.Code == "busy" || be.Code == "held" || be.Code == "unavailable") {
			n.dshWaiting()
			return "", later("DSH did not take it now (%s): tried again later, not twice", be.Message) // DSH drops a second one with the same id
		}
		return "", dshBridgeErr(err)
	}
	n.audit.Log(AuditRecord{Kind: "dsh.message", Subject: p.ID(), Session: id, Decision: "allow", Detail: detail})
	n.st.Set(dshLastKey(id), []byte(reqID))
	n.tookChain(m)
	return "handed", nil
}

// dshWaiting notes a message waits for DSH Desktop: serveDSH wakes the
// inbox once the plugin answers.
func (n *Machine) dshWaiting() {
	n.mu.Lock()
	n.dshPending = true
	n.mu.Unlock()
}

// watchDSHTurn acknowledges handled the message from the post office DSH
// took for session id
// once the turn it is in ends there: DSH Desktop's own word for the
// session is no longer "running" (it ran, or a few seconds passed).
func (n *Machine) watchDSHTurn(id, address, messageID string) {
	n.handedToTurn(address, messageID)
	go func() {
		home, err := dshnative.Home(shellenv.Environ())
		if err != nil {
			return
		}
		start := time.Now()
		ran := false
		for time.Since(start) < dshHandledWatch {
			time.Sleep(2 * time.Second)
			ctx, cancel := context.WithTimeout(context.Background(), dshBridgeDial)
			b, err := dshnative.DialBridge(ctx, home)
			if err != nil {
				cancel()
				continue
			}
			live, status, err := b.State(ctx, id)
			b.Close()
			cancel()
			if err != nil {
				continue
			}
			if live && status == "running" {
				ran = true
				continue
			}
			if ran || time.Since(start) > 6*time.Second {
				n.turnEnded(address)
				return
			}
		}
	}()
}

// serveDSH follows the Aide plugin in DSH Desktop while the machine runs:
// it takes what the taken-over sessions send through Aide (the plugin's
// outbox), and wakes the inbox for messages that wait for DSH Desktop once
// the plugin answers again.
func (n *Machine) serveDSH(ctx context.Context) {
	for ctx.Err() == nil {
		home, err := dshnative.Home(shellenv.Environ())
		if err == nil {
			dctx, cancel := context.WithTimeout(ctx, dshBridgeDial)
			var b *dshnative.Bridge
			b, err = dshnative.DialBridge(dctx, home)
			cancel()
			if err == nil {
				n.mu.Lock()
				wake := n.dshPending
				n.dshPending = false
				n.mu.Unlock()
				if wake {
					select {
					case n.inboxWake <- struct{}{}:
					default:
					}
				}
				if b.Hello.Has("outbox") {
					n.followDSHOutbox(ctx, b)
				}
				b.Close()
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

// followDSHOutbox takes the sessions' sends from the plugin until it stops
// answering.
func (n *Machine) followDSHOutbox(ctx context.Context, b *dshnative.Bridge) {
	for ctx.Err() == nil {
		octx, cancel := context.WithTimeout(ctx, 30*time.Second)
		sends, err := b.Outbox(octx, 20*time.Second)
		cancel()
		if err != nil {
			return
		}
		for _, s := range sends {
			id, to, refused := n.dshSend(s)
			dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, err := b.OutboxDone(dctx, s.ID, id, to, refused)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// dshSend sends what a taken-over DSH session asked to, as the machine:
// to an agent of the owner's only, by default the one that last wrote to
// the session through Aide, answering its last message. It says the
// message's id and address, or why not.
func (n *Machine) dshSend(s dshnative.Send) (messageID, to string, refused *dshnative.BridgeError) {
	refuse := func(format string, a ...any) (string, string, *dshnative.BridgeError) {
		why := fmt.Sprintf(format, a...)
		n.audit.Log(AuditRecord{Kind: "dsh.message.out", Subject: "dsh:" + s.SessionID, Session: s.SessionID, Decision: "deny", Detail: why})
		return "", "", &dshnative.BridgeError{Code: "refused", Message: why}
	}
	home, err := dshnative.Home(shellenv.Environ())
	if err != nil {
		return refuse("%v", err)
	}
	if _, err := n.dshOwnSession(home, s.SessionID); err != nil {
		return refuse("%s", connectMessage(err))
	}
	if !n.dshTakenOver(s.SessionID) {
		return refuse("the owner has not taken over this session in the Aide app: it can't write through Aide")
	}
	if strings.TrimSpace(s.Text) == "" || len(s.Text) > maxDSHMessage {
		return refuse("the message is empty or longer than %d bytes", maxDSHMessage)
	}
	if n.dshSentLastHour(s.SessionID, time.Now()) >= dshOutPerHour {
		n.log.Warn("a DSH session sent too much through Aide: held for the owner", "session", s.SessionID)
		return refuse("held for the owner: this session sent %d messages through Aide in the last hour", dshOutPerHour)
	}
	var last dshSent
	if b, _ := n.st.Get(dshLastKey(s.SessionID)); b != nil {
		if r, _ := n.st.Get(dshSentKey(string(b))); r != nil {
			json.Unmarshal(r, &last)
		}
	}
	dest := s.To
	if dest == "" {
		if last.Address == "" {
			return refuse("no agent has written to this session through Aide yet: name one with `to`")
		}
		dest = last.Address
	}
	addr, why := n.dshRecipient(dest)
	if why != "" {
		return refuse("%s", why)
	}
	// Unless it says which, it answers the last message from the agent it
	// writes to.
	replyTo := s.ReplyTo
	if lastInbox, _, _ := strings.Cut(last.Address, "/"); replyTo == "" && lastInbox == addr.Inbox {
		replyTo = last.MessageID
	}
	from := dshAddress(n.machineID, s.SessionID)
	m := &inboxv1.Message{MessageId: "msg_" + strings.ToLower(ulid.Make().String()), From: &inboxv1.Address{Inbox: n.machineID, Session: from},
		To: addr, SentAt: timestamppb.Now(), InReplyTo: replyTo, Text: s.Text}
	if err := n.sendMessage(m); err != nil {
		return "", "", &dshnative.BridgeError{Code: "unavailable", Message: "Aide could not reach the post office now; try again: " + err.Error()}
	}
	n.mu.Lock()
	if n.dshOut == nil {
		n.dshOut = map[string][]time.Time{}
	}
	n.dshOut[s.SessionID] = append(n.dshOut[s.SessionID], time.Now())
	n.mu.Unlock()
	n.audit.Log(AuditRecord{Kind: "dsh.message.out", Subject: "dsh:" + s.SessionID, Session: s.SessionID, Decision: "allow",
		Detail: "message=" + m.MessageId + " to=" + inboxAddress(addr) + " reply_to=" + replyTo + " message_sha=" + sha(s.Text)})
	return m.MessageId, inboxAddress(addr), nil
}

// dshSentLastHour is how many messages DSH session id sent through Aide
// in the hour before now (the older are forgotten).
func (n *Machine) dshSentLastHour(id string, now time.Time) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	var recent []time.Time
	for _, at := range n.dshOut[id] {
		if now.Sub(at) < time.Hour {
			recent = append(recent, at)
		}
	}
	if len(recent) == 0 {
		delete(n.dshOut, id)
	} else {
		n.dshOut[id] = recent
	}
	return len(recent)
}

// dshRecipient is the agent a DSH session may write to under to: its
// address (c_…, c_…/<conversation>) or its name, an agent this machine's
// grants name. Not a session, nor anyone else.
func (n *Machine) dshRecipient(to string) (*inboxv1.Address, string) {
	inbox, conv, _ := strings.Cut(strings.TrimPrefix(to, "@"), "/")
	switch {
	case strings.HasPrefix(inbox, "m_"), strings.HasPrefix(inbox, "s_"), strings.HasPrefix(inbox, "dsh_"):
		return nil, "a DSH session writes to the owner's agents through Aide, not to sessions"
	}
	for _, g := range n.Local().Grants() {
		if g.ClientId == inbox || !strings.HasPrefix(inbox, "c_") && conv == "" && strings.EqualFold(g.ClientLabel, inbox) {
			return &inboxv1.Address{Inbox: g.ClientId, Session: conv}, ""
		}
	}
	return nil, fmt.Sprintf("%s is not an agent of the owner's this machine knows", to)
}

// connectMessage is err's message without its code.
func connectMessage(err error) string {
	var ce *connect.Error
	if errors.As(err, &ce) {
		return ce.Message()
	}
	return err.Error()
}
