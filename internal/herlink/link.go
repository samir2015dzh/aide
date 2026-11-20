package herlink

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	inboxv1 "github.com/samir2015dzh/aide/gen/go/aide/inbox/v1"
	"github.com/samir2015dzh/aide/internal/cli"
	"github.com/samir2015dzh/aide/internal/herhost"
	"github.com/samir2015dzh/aide/internal/machine"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// TakesMessages is how Her's inbox says it takes messages: the news of her
// own sessions, and nothing from agents (docs/her-mesh.md §6.6). Senders
// read it from her registration and don't write to her.
const TakesMessages = "her"

// state is what the link keeps on disk.
type state struct {
	// Registered is how the post office was last told her inbox takes
	// messages.
	Registered string `json:"registered,omitempty"`
	// Names are the names her assistants run commands as, by name: their ids.
	Names map[string]string `json:"names,omitempty"`
}

// Link is a machine's HerLink.
type Link struct {
	cfg  machine.HerLinkConfig
	cmds map[string]cli.Tool

	mu   sync.Mutex
	st   state
	peer *cli.Peer
}

// New is machine.Config.NewHerLink.
func New(cfg machine.HerLinkConfig) machine.HerLink {
	l := &Link{cfg: cfg, cmds: commands(), st: state{Names: map[string]string{}}}
	if b, err := os.ReadFile(l.path()); err == nil {
		json.Unmarshal(b, &l.st)
		if l.st.Names == nil {
			l.st.Names = map[string]string{}
		}
	}
	return l
}

func (l *Link) path() string { return filepath.Join(l.cfg.DataDir, "her-link.json") }

// saveLocked writes the state; a failure only costs what it remembers.
func (l *Link) saveLocked() {
	b, _ := json.Marshal(l.st)
	tmp := l.path() + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, l.path())
	}
}

func (l *Link) Group() *herhost.MeshGroup { return group() }

// options are what a command runs with: her credentials and her kept
// connection.
func (l *Link) options() (cli.ExecOptions, bool) {
	c := l.cfg.Credentials()
	if c == nil {
		return cli.ExecOptions{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.peer == nil {
		l.peer = &cli.Peer{}
	}
	return cli.ExecOptions{
		Identity: cli.Identity{RelayURL: c.RelayURL, OwnerID: c.OwnerID, Key: c.Key, RootChain: c.RootChain, Grant: c.Grant, InboxKey: c.InboxKey,
			TakesMessages: TakesMessages, InboxRegistered: l.st.Registered},
		HTTPClient: l.cfg.HTTPClient, Peer: l.peer,
		Adopted: func(chain *identityv1.RootChain, grant *identityv1.Signed) { l.cfg.Adopted(chain, grant) },
		InboxRegistered: func(takes string) {
			l.mu.Lock()
			l.st.Registered = takes
			l.saveLocked()
			l.mu.Unlock()
		},
	}, true
}

// Close drops her connections; the next command connects again.
func (l *Link) Close() {
	l.mu.Lock()
	p := l.peer
	l.peer = nil
	l.st.Registered = "" // a grant she gets later registers her inbox again
	l.saveLocked()
	l.mu.Unlock()
	if p != nil {
		p.Close()
	}
}

// asName is the name an assistant runs commands as, so that the news of
// its sessions comes back addressed to it: its id as a name.
func (l *Link) asName(bot string) string {
	if bot == "" {
		return ""
	}
	name, err := identity.NormalizeName(bot)
	if err != nil {
		sum := sha256.Sum256([]byte(bot))
		name = "a" + hex.EncodeToString(sum[:10])
	}
	l.mu.Lock()
	if l.st.Names[name] != bot {
		l.st.Names[name] = bot
		l.saveLocked()
	}
	l.mu.Unlock()
	return name
}

func failure(kind, message string) string {
	b, _ := json.Marshal(map[string]any{"ok": false, "error": map[string]any{"kind": kind, "message": message}})
	return string(b)
}

// arguments are the command-line arguments of one call of a tool of hers.
func (l *Link) arguments(t tool, bot, callID string, raw json.RawMessage) ([]string, error) {
	values := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("the arguments are not a JSON object")
		}
	}
	for _, h := range hidden {
		delete(values, h)
	}
	if t.name == "session_send" {
		to, _ := values["session_id"].(string)
		if !isSession(to) {
			return nil, fmt.Errorf("session_id: a session's id (s_…)")
		}
		values = map[string]any{"to": to, "message": values["message"]}
	}
	if t.name == "session_start" {
		if p, _ := values["prompt"].(string); strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("prompt is required")
		}
		// A repository's session is cloned on the computer named.
		if r, _ := values["repo"].(string); r != "" {
			if m, _ := values["machine"].(string); m == "" {
				return nil, fmt.Errorf("machine is required with repo: the computer to clone it on, by its name in project_list")
			}
		}
		if callID != "" {
			values["idempotency_key"] = callID
		}
	}
	if as := l.asName(bot); as != "" {
		values["as"] = as
	}
	return l.cmds[t.cmd].Args(values)
}

// isSession reports whether to names a session: s_… or m_…/s_….
func isSession(to string) bool {
	_, s, ok := strings.Cut(to, "/")
	if !ok {
		s = to
	}
	return strings.HasPrefix(s, "s_")
}

// Call runs one of her tools and returns the command's JSON document, and
// what the apps read of it (docs/her-team.md §6.2).
func (l *Link) Call(ctx context.Context, bot, callID, name string, raw json.RawMessage) (string, any, bool) {
	t, ok := find(name)
	if !ok {
		return failure("usage", "no tool "+name), nil, true
	}
	args, err := l.arguments(t, bot, callID, raw)
	if err != nil {
		return failure("usage", err.Error()), nil, true
	}
	opts, ok := l.options()
	if !ok {
		return failure("revoked", "Her has no access to the user's computers now"), nil, true
	}
	doc, code := cli.Exec(ctx, opts, args)
	if code != 0 && errorKind(doc) == "revoked" {
		l.cfg.Revoked()
	}
	if code != 0 {
		return string(doc), nil, true
	}
	return string(doc), details(t.name, doc), false
}

// details are what the apps read of a call's document: the session a
// session_start started, for Her's card of it; the counts of work_summary,
// for the card of the team's work.
func details(tool string, doc []byte) any {
	switch tool {
	case "work_summary":
		var d struct {
			Work json.RawMessage `json:"work"`
		}
		if json.Unmarshal(doc, &d) != nil || len(d.Work) == 0 || string(d.Work) == "null" {
			return nil
		}
		return map[string]any{"work": d.Work}
	case "session_start":
		var d struct {
			Session struct {
				SessionID string `json:"session_id"`
				Title     string `json:"title"`
				Agent     string `json:"agent"`
				Machine   string `json:"machine"`
				MachineID string `json:"machine_id"`
				Project   string `json:"project"`
			} `json:"session"`
		}
		if json.Unmarshal(doc, &d) != nil || d.Session.SessionID == "" {
			return nil
		}
		return map[string]any{"session": d.Session}
	}
	return nil
}

func errorKind(doc []byte) string {
	var d struct {
		Error struct {
			Kind string `json:"kind"`
		} `json:"error"`
	}
	json.Unmarshal(doc, &d)
	return d.Error.Kind
}

// Render is the news of one of her sessions as her reads it, and the
// assistant it is for.
func (l *Link) Render(m *inboxv1.Message) (bot, from, text string, ok bool) {
	if m.GetSessionNews() == nil {
		return "", "", "", false
	}
	as := m.GetTo().GetSession()
	l.mu.Lock()
	bot = l.st.Names[as]
	l.mu.Unlock()
	if bot == "" {
		bot = as
	}
	from, text = newsText(m, time.Now())
	return bot, from, text, true
}

// Run follows her inbox until ctx ends or her grant is revoked: the news
// of her sessions goes to the machine, and anything else is refused (she
// takes no messages from agents yet, docs/her-mesh.md §6.6).
func (l *Link) Run(ctx context.Context) {
	wait := time.Second
	for ctx.Err() == nil {
		opts, ok := l.options()
		if !ok {
			return
		}
		start := time.Now()
		err := cli.Follow(ctx, opts, func(m *inboxv1.Message, from *identity.ContactInfo) string {
			if from.Kind != "machine" || m.GetSessionNews() == nil {
				return "refused"
			}
			if !l.cfg.News(m) {
				return ""
			}
			return "handled"
		})
		if ctx.Err() != nil {
			return
		}
		if cli.ErrRevoked(err) {
			l.cfg.Revoked()
			return
		}
		if time.Since(start) > time.Minute {
			wait = time.Second
		}
		l.cfg.Logger.Debug("Her's inbox", "err", err)
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
		wait = min(2*wait, time.Minute)
	}
}
