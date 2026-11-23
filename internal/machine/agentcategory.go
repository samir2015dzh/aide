package machine

import (
	"encoding/json"
	"errors"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
)

// The kind the owner filed a coding agent as (SetAgentCategory, 工单 #52,
// #93): kept here, with the agent, so every screen of the owner's reads the
// same say from its status. Unset, the screens tell it from what it is.

const kAgentCategories = "agent_categories"

func (n *Machine) loadAgentCategories() {
	n.categories = map[string]string{}
	if b, err := n.st.Get(kAgentCategories); err == nil && b != nil {
		json.Unmarshal(b, &n.categories)
	}
}

// setAgentCategory files a coding agent the machine knows as a kind, or
// back to told from what it is ("").
func (n *Machine) setAgentCategory(c *identityv1.SetAgentCategory) error {
	switch c.GetCategory() {
	case "", "local", "personal":
	default:
		return errors.New("a kind is local, personal or none")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if c.GetAgent() == "" || n.provider(c.Agent) == nil {
		return errors.New("no such coding agent")
	}
	if c.Category == "" {
		delete(n.categories, c.Agent)
	} else {
		n.categories[c.Agent] = c.Category
	}
	b, err := json.Marshal(n.categories)
	if err != nil {
		return err
	}
	return n.st.Set(kAgentCategories, b)
}
