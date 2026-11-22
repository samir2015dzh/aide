// Package convreport keeps which Claude Code conversations on this computer
// the CLI agent's hooks have seen (docs/agent-messages.md §15.4): the
// SessionStart hook (`aide message hook claude`) records the conversation's
// key, under the agent's address, in the CLI's home; aided, listing the
// Claude app's sessions, gives a session whose conversation is recorded its
// address (c_…/<key>). Nothing is worked out from the Claude app's data:
// only a conversation the hooks reported takes messages.
package convreport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// keep: how long a conversation stays recorded after its last start;
// most: how many are kept.
const (
	keep = 90 * 24 * time.Hour
	most = 5000
)

// Report is what the hooks recorded: the agent and its conversations,
// each with when it last started.
type Report struct {
	Agent         string               `json:"agent"`
	Conversations map[string]time.Time `json:"conversations"`
}

func file(home string) string { return filepath.Join(home, "conversations.json") }

// Load reads the report in the CLI home; an empty one when there is none.
func Load(home string) Report {
	var r Report
	if b, err := os.ReadFile(file(home)); err == nil {
		json.Unmarshal(b, &r)
	}
	if r.Conversations == nil {
		r.Conversations = map[string]time.Time{}
	}
	return r
}

// Record notes that conversation key started under agent at now, forgetting
// those not seen for 90 days (and the oldest beyond 5000), and those of
// another agent (the home logged in again as another).
func Record(home, agent, key string, now time.Time) error {
	r := Load(home)
	if r.Agent != agent {
		r = Report{Agent: agent, Conversations: map[string]time.Time{}}
	}
	r.Conversations[key] = now.UTC()
	for k, at := range r.Conversations {
		if now.Sub(at) > keep {
			delete(r.Conversations, k)
		}
	}
	for len(r.Conversations) > most {
		oldest, at := "", now
		for k, t := range r.Conversations {
			if t.Before(at) {
				oldest, at = k, t
			}
		}
		delete(r.Conversations, oldest)
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	tmp := file(home) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file(home))
}

// Address is the address of conversation key under the agent that reported
// it; "" when it was not reported.
func (r Report) Address(key string) string {
	if r.Agent == "" || key == "" {
		return ""
	}
	if _, ok := r.Conversations[key]; !ok {
		return ""
	}
	return r.Agent + "/" + key
}
