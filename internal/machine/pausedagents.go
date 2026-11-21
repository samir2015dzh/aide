package machine

import (
	"encoding/json"
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// Paused coding agents (SetAgentPaused): the owner's way to rest an agent on
// this machine without removing it, typically while its plan's usage is used
// up. While paused no new session starts with it here; its sessions under way
// run on, and its settings and history stay. It resumes when the owner says,
// or by itself at resume_at when the owner chose that (a usage reset the
// machine read); a resume by itself is kept, for the apps to show.

const (
	kPausedAgents  = "paused_agents"
	kAgentsResumed = "agents_resumed"
)

// pausedAgent is an agent the owner paused here.
type pausedAgent struct {
	Since    time.Time `json:"since"`
	ResumeAt time.Time `json:"resume_at,omitzero"`
}

func (n *Machine) loadPausedAgents() {
	n.pausedAgents = map[string]pausedAgent{}
	n.resumed = map[string]time.Time{}
	n.resumeTimers = map[string]*time.Timer{}
	if b, err := n.st.Get(kPausedAgents); err == nil && b != nil {
		json.Unmarshal(b, &n.pausedAgents)
	}
	if b, err := n.st.Get(kAgentsResumed); err == nil && b != nil {
		json.Unmarshal(b, &n.resumed)
	}
	n.mu.Lock()
	for name, p := range n.pausedAgents {
		n.armResumeLocked(name, p)
	}
	n.mu.Unlock()
}

// savePausedAgentsLocked keeps the paused agents and the last resumes by
// themselves; n.mu is held.
func (n *Machine) savePausedAgentsLocked() error {
	b, err := json.Marshal(n.pausedAgents)
	if err != nil {
		return err
	}
	if err := n.st.Set(kPausedAgents, b); err != nil {
		return err
	}
	if b, err = json.Marshal(n.resumed); err != nil {
		return err
	}
	return n.st.Set(kAgentsResumed, b)
}

// setAgentPaused pauses a coding agent the machine knows, or resumes it.
func (n *Machine) setAgentPaused(c *identityv1.SetAgentPaused) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if c.GetAgent() == "" || n.provider(c.Agent) == nil {
		return errors.New("no such coding agent")
	}
	n.stopResumeLocked(c.Agent)
	if !c.Paused {
		delete(n.pausedAgents, c.Agent)
		return n.savePausedAgentsLocked()
	}
	p := pausedAgent{Since: time.Now().UTC()}
	if c.ResumeAt != nil {
		if p.ResumeAt = c.ResumeAt.AsTime().UTC(); !p.ResumeAt.After(p.Since) {
			return errors.New("it would resume before it is paused: resume_at must be in the future")
		}
	}
	n.pausedAgents[c.Agent] = p
	n.armResumeLocked(c.Agent, p)
	return n.savePausedAgentsLocked()
}

// armResumeLocked has an agent paused until a time resume by itself then
// (at once when that time has passed, as after the machine was off); n.mu
// is held.
func (n *Machine) armResumeLocked(name string, p pausedAgent) {
	if p.ResumeAt.IsZero() {
		return
	}
	n.resumeTimers[name] = time.AfterFunc(max(time.Until(p.ResumeAt), 0), func() { n.resumeByItself(name, p.ResumeAt) })
}

func (n *Machine) stopResumeLocked(name string) {
	if t := n.resumeTimers[name]; t != nil {
		t.Stop()
		delete(n.resumeTimers, name)
	}
}

// resumeByItself resumes an agent whose pause, until at, has come to its
// end, unless the owner paused it again since.
func (n *Machine) resumeByItself(name string, at time.Time) {
	n.mu.Lock()
	p, ok := n.pausedAgents[name]
	if !ok || !p.ResumeAt.Equal(at) {
		n.mu.Unlock()
		return
	}
	delete(n.pausedAgents, name)
	delete(n.resumeTimers, name)
	n.resumed[name] = time.Now().UTC()
	err := n.savePausedAgentsLocked()
	n.mu.Unlock()
	if err != nil {
		n.log.Warn("keeping the paused coding agents", "err", err)
	}
	n.log.Info("coding agent resumed by itself", "agent", name, "paused_until", at)
	n.kickStatus()
}

// stopResumeTimers stops the resumes by themselves: the machine closes.
func (n *Machine) stopResumeTimers() {
	n.mu.Lock()
	for name := range n.resumeTimers {
		n.stopResumeLocked(name)
	}
	n.mu.Unlock()
}

// pauseOfLocked is an agent's pause as its status says it, and when it last
// resumed by itself; n.mu is held.
func (n *Machine) pauseOfLocked(name string) (*machinev1.AgentPause, *timestamppb.Timestamp) {
	var resumed *timestamppb.Timestamp
	if t, ok := n.resumed[name]; ok {
		resumed = timestamppb.New(t)
	}
	p, ok := n.pausedAgents[name]
	if !ok {
		return nil, resumed
	}
	a := &machinev1.AgentPause{Since: timestamppb.New(p.Since)}
	if !p.ResumeAt.IsZero() {
		a.ResumeAt = timestamppb.New(p.ResumeAt)
	}
	return a, resumed
}

// agentPaused says whether no new session may start with the agent here,
// and until when, if it resumes by itself.
func (n *Machine) agentPaused(name string) (bool, time.Time) {
	n.mu.Lock()
	defer n.mu.Unlock()
	p, ok := n.pausedAgents[name]
	return ok, p.ResumeAt
}
