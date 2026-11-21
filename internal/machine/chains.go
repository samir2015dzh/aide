package machine

import (
	"context"
	"time"

	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/internal/msgchain"
)

// Chains of messages and what became of the ones the sessions took
// (docs/agent-messages.md §15).

// chainAt is a chain, and the hop a message in it was at.
type chainAt struct {
	trace string
	hop   uint32
}

// chainCheck says why to hold a message for the owner, "" to take it: its
// chain has gone too far, or comes round to its session too often.
func (n *Machine) chainCheck(m *inboxv1.Message) string {
	return n.chains.Check(n.machineID+"/"+m.GetTo().GetSession(), m.TraceId, m.Hop, time.Now())
}

// tookChain keeps the chain of a message its session took: what the
// session writes next goes on with it.
func (n *Machine) tookChain(m *inboxv1.Message) {
	if m.TraceId == "" {
		return
	}
	n.mu.Lock()
	if n.lastChain == nil {
		n.lastChain = map[string]chainAt{}
	}
	n.lastChain[m.GetTo().GetSession()] = chainAt{m.TraceId, m.Hop}
	n.mu.Unlock()
}

// chainFrom is the chain a message a session writes goes on with: the one
// it last took, one hop more, or a new one.
func (n *Machine) chainFrom(session string) (string, uint32) {
	n.mu.Lock()
	c, ok := n.lastChain[session]
	n.mu.Unlock()
	if !ok {
		return msgchain.New()
	}
	return msgchain.Next(c.trace, c.hop)
}

// handedToTurn keeps a message from the post office that a session's turn
// took ("handed"): acknowledged handled once the turn ends.
func (n *Machine) handedToTurn(session, id string) {
	n.mu.Lock()
	if n.handedTurns == nil {
		n.handedTurns = map[string][]string{}
	}
	n.handedTurns[session] = append(n.handedTurns[session], id)
	n.mu.Unlock()
}

// turnEnded acknowledges handled the messages the session's turn took.
func (n *Machine) turnEnded(session string) {
	n.mu.Lock()
	ids := n.handedTurns[session]
	delete(n.handedTurns, session)
	conn := n.conn
	n.mu.Unlock()
	if len(ids) == 0 || conn == nil {
		return
	}
	go func() {
		ms, err := n.messageService(conn)
		if err != nil {
			return
		}
		var acks []*relayv1.MessageAck
		for _, id := range ids {
			acks = append(acks, &relayv1.MessageAck{MessageId: id, Outcome: "handled"})
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := ms.Ack(ctx, &relayv1.AckMessagesRequest{Acks: acks}); err != nil {
			n.log.Warn("acknowledging the messages a turn took", "session", session, "err", err)
		}
	}()
}
