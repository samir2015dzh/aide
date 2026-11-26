package machine

import (
	"encoding/json"
	"errors"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// Hidden coding agents (SetAgentHidden): the owner's say over a listing they
// don't trust, such as an agent they uninstalled that something left behind
// still answers for. Hiding takes the entry out of the apps' lists and grants
// or takes away nothing: the checks go on, and a session can still start
// with it. A routine check never shows it again; the owner's rescan does,
// when the agent then runs where it did not when it was hidden, or is found
// elsewhere, as an agent installed again would be.

const kHiddenAgents = "hidden_agents"

// hiddenAgent is what was seen of an agent when the owner hid it.
type hiddenAgent struct {
	Binary    string    `json:"binary"`
	Available bool      `json:"available"`
	At        time.Time `json:"at"`
}

// showsAgain: after the owner's rescan, an agent hidden as h is shown again
// when it now runs (i.Available) and either did not when hidden, or runs a
// program other than the one it ran then.
func showsAgain(h hiddenAgent, i provider.Info) bool {
	return i.Available && (!h.Available || (i.Binary != "" && i.Binary != h.Binary))
}

func (n *Machine) loadHiddenAgents() {
	n.hidden = map[string]hiddenAgent{}
	if b, err := n.st.Get(kHiddenAgents); err == nil && b != nil {
		json.Unmarshal(b, &n.hidden)
	}
}

// saveHiddenAgentsLocked keeps the hidden agents; n.mu is held.
func (n *Machine) saveHiddenAgentsLocked() error {
	b, err := json.Marshal(n.hidden)
	if err != nil {
		return err
	}
	return n.st.Set(kHiddenAgents, b)
}

// setAgentHidden hides a coding agent the machine knows, or shows it again.
func (n *Machine) setAgentHidden(c *identityv1.SetAgentHidden) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if c.GetAgent() == "" || n.provider(c.Agent) == nil {
		return errors.New("no such coding agent")
	}
	if c.Hidden {
		i := n.infos[c.Agent]
		n.hidden[c.Agent] = hiddenAgent{Binary: i.Binary, Available: i.Available, At: time.Now().UTC()}
	} else {
		delete(n.hidden, c.Agent)
	}
	return n.saveHiddenAgentsLocked()
}

// isHiddenLocked: the owner hid the agent; n.mu is held.
func (n *Machine) isHiddenLocked(name string) bool {
	_, ok := n.hidden[name]
	return ok
}

// rescanHidden follows the owner's rescan (Check Again, with rescan_hidden):
// each hidden agent that shows again by showsAgain is listed once more.
func (n *Machine) rescanHidden() {
	n.mu.Lock()
	changed := false
	for name, h := range n.hidden {
		if i, ok := n.infos[name]; ok && showsAgain(h, i) {
			delete(n.hidden, name)
			changed = true
		}
	}
	if changed {
		if err := n.saveHiddenAgentsLocked(); err != nil {
			n.log.Warn("keeping the hidden coding agents", "err", err)
		}
	}
	n.mu.Unlock()
}

// RescanAgents is the owner's rescan of this device's own machine: its coding
// agents checked again, then the hidden ones that show again by showsAgain
// listed once more; in the background.
func (l *Local) RescanAgents() {
	go func() {
		<-l.n.CheckAgentsOnPurpose()
		l.n.rescanHidden()
	}()
}
