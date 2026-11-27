// Package provider defines the interface between the session kernel and
// coding-agent CLIs (Claude Code, Codex, ACP agents), plus shared process
// plumbing.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
)

// Info describes a provider on this machine.
type Info struct {
	Name      string
	Available bool
	Binary    string
	Version   string
	LoggedIn  bool
	// LoginUnconfirmed: the check could not tell whether it is signed in
	// (LoggedIn false): shown as unconfirmed, not as signed out (工单 #108).
	LoginUnconfirmed bool
	// Claude only: the Claude desktop app's settings are on the machine,
	// and it is signed in. Its sign-in is not the command line's
	// (LoggedIn): the CLI Aide runs keeps its own.
	DesktopInstalled bool
	DesktopSignedIn  bool
	// CheckedAt is when the check that found this ended (set by the
	// machine that ran it).
	CheckedAt time.Time
	// Problem is why it is not available; Detail, what was printed.
	Problem Problem
	Detail  string
	// Models it offers and its own configuration, as far as it tells
	// (Claude Code and Codex), for picking a machine's defaults.
	Models []Model
	Own    Settings
	// Modes are the session modes it offers (ACP agents).
	Modes []Mode
	// ModelsAt is when Models were found, for an agent whose check keeps
	// what an earlier one found (DSH's probe, 工单 #166); zero when this
	// check found them. What a session of the agent offered since is newer
	// (工单 #129 返修).
	ModelsAt time.Time
	// ModelsProbe is what the last probe for them failed at, a step's name
	// and never the agent's words (logged: 工单 #129 返修).
	ModelsProbe string
	// AllModels: Models are every model it takes here (Codex lists its
	// own), so a session naming another can be refused before it starts.
	AllModels bool
	// SomeEfforts: a model's Efforts are known only once it ran here (an
	// ACP agent tells those of a session's model only); a model without
	// any may yet take some.
	SomeEfforts bool
	// ModelsSkipped: this check did not start the agent to ask for its
	// models (not asked on purpose, 工单 #112); what an earlier check that
	// did found stands for it.
	ModelsSkipped bool
	// Capabilities declares what this provider truly supports on this machine
	// (工单 #110 / #112). When nil, machine fills truthful provider defaults.
	Capabilities *Capabilities
	// ProtocolVersion is the negotiated ACP protocol version (e.g. 2 or 1), 0 for non-ACP.
	ProtocolVersion uint32
	// PlanUsage is what the agent said of its account's plan usage when
	// this check asked (AskUsage, planusage.go; Claude Code and Codex,
	// 工单 #143); nil when it was not asked.
	PlanUsage *PlanUsage
}

// Capabilities declares what a provider truly supports so Aide hides or disables
// unsupported entries instead of failing only after the owner clicks (工单 #110 / #112).
type Capabilities struct {
	SelectModel          bool `json:"select_model,omitempty"`
	SwitchModelInSession bool `json:"switch_model_in_session,omitempty"`
	SelectEffort         bool `json:"select_effort,omitempty"`
	SelectMode           bool `json:"select_mode,omitempty"`
	LoginInAide          bool `json:"login_in_aide,omitempty"`
	Usage                bool `json:"usage,omitempty"`
	Fork                 bool `json:"fork,omitempty"`
	Rollback             bool `json:"rollback,omitempty"`
	Steer                bool `json:"steer,omitempty"`
	Resume               bool `json:"resume,omitempty"`
	Images               bool `json:"images,omitempty"`
	Audio                bool `json:"audio,omitempty"`
}

// CapabilitiesProvider optionally exposes live negotiated capabilities discovered
// from the provider (for example, from an ACP initialize handshake).
type CapabilitiesProvider interface {
	ObservedCapabilities() (*Capabilities, uint32)
}

// ModelCatalog is an observation, not a machine-wide entitlement guarantee.
// Scope is an opaque session address, or "isolated" for a workspace-free probe.
// State distinguishes a known empty model selection from missing information.
type ModelCatalog struct {
	Scope, Source, State       string
	Models                     []Model
	DefaultModel, CurrentModel string
	ObservedAt, ExpiresAt      time.Time
}

// ModelCatalogProvider exposes in-memory, scoped observations without starting
// processes. Providers without this optional interface retain their old behavior.
type ModelCatalogProvider interface {
	ModelCatalogs() []ModelCatalog
}

// ScopedModels: what the agent's sessions offer stays with each session
// (its ModelCatalogs), never the machine's for the agent as a whole.
type ScopedModels interface {
	ScopedModels() bool
}

// Model is one model an agent offers. Resolved is the specific model an
// alias ID runs here (Claude Code's, 工单 #99); "" when ID is the model.
// Source is where the agent offers it from (DSH: its provider, or each
// provider line it comes by, " · " between; 工单 #98 补充).
type Model struct {
	ID, Name string
	Efforts  []string
	Resolved string `json:",omitempty"`
	Source   string `json:",omitempty"`
}

// Mode is one session mode an agent offers (an ACP agent's permission or
// approval preset).
type Mode struct {
	ID, Name string
}

// Offers are what an ACP agent's new session offers to choose from, and
// what it runs with unless told: such an agent tells them only in a
// session.
type Offers struct {
	Models []Model
	Modes  []Mode
	Own    Settings
}

// Settings are what a session runs with; empty ones follow the agent's own
// configuration.
type Settings struct {
	Model, Effort string
	Speed         identityv1.AgentSpeed
	Mode          string // ACP agents
}

type ToolKind string

const (
	ToolRead    ToolKind = "read"
	ToolSearch  ToolKind = "search"
	ToolEdit    ToolKind = "edit"
	ToolShell   ToolKind = "shell"
	ToolNetwork ToolKind = "network"
	ToolMCP     ToolKind = "mcp"
	ToolOther   ToolKind = "other"
)

// ToolAction is a normalized tool call the gate decides on.
type ToolAction struct {
	CallID  string
	Kind    ToolKind
	Name    string
	Paths   []string // as given by the agent (absolute or relative)
	Command string
	Summary string
	Raw     json.RawMessage
}

type Decision struct {
	Allow  bool
	Reason string
}

// Gate decides on tool calls. It must be safe for concurrent use.
type Gate func(ctx context.Context, a ToolAction) Decision

type EventKind int

const (
	EventRef EventKind = iota + 1 // provider session reference became known
	EventAgentMessage
	EventToolCall
	EventToolResult
	EventTurnCompleted
	EventError
	EventQuestion // the agent asks the user (Question); answer with Session.Answer
	// EventAgentDraft is reply text the agent is still writing (Text: the
	// next piece). The EventAgentMessage that follows carries all of it.
	EventAgentDraft
)

// Question is a question the agent asks mid-turn.
type Question struct {
	// ID is the provider's handle for Answer.
	ID    string
	Items []QuestionItem
}

type QuestionItem struct {
	ID, Header, Text string
	Options          []Option
	MultiSelect      bool
	AllowOther       bool // free text accepted (always, without options)
	Secret           bool
	Optional         bool // may be left unanswered
}

type Option struct{ Label, Description string }

// Answer answers one QuestionItem.
type Answer struct {
	ItemID   string
	Selected []string // option labels
	Text     string
}

// Reply resolves a question: Answers, or (Declined) none, with Message
// telling the agent why and what to do.
type Reply struct {
	Answers  []Answer
	Declined bool
	Message  string
}

// Value is an answer as one string (工单 #179): the chosen labels in the
// order they were chosen, then the typed text, joined with ", ". It is the
// one encoding for an answer sent as text, and SplitAnswer reads it back.
func (a Answer) Value() string {
	v := append([]string{}, a.Selected...)
	if t := strings.TrimSpace(a.Text); t != "" {
		v = append(v, t)
	}
	return strings.Join(v, ", ")
}

// SplitAnswer reads an answer in Value's encoding back against the item's
// option labels (工单 #179): it takes exact labels from the front, the
// longest first, so a label that contains ", " wins over one it begins
// with; what is left is the typed text.
//
// Adapted from paseo's splitMultiSelectAnswer
// (packages/server/src/server/agent/providers/question-card-answer.ts,
// https://github.com/getpaseo/paseo), Apache License 2.0.
func SplitAnswer(answer string, labels []string) (selected []string, text string) {
	byLength := slices.Clone(labels)
	slices.SortStableFunc(byLength, func(a, b string) int { return len(b) - len(a) })
	rest := answer
	for rest != "" {
		i := slices.IndexFunc(byLength, func(l string) bool {
			return l != "" && !slices.Contains(selected, l) && (rest == l || strings.HasPrefix(rest, l+", "))
		})
		if i < 0 {
			break
		}
		l := byLength[i]
		selected = append(selected, l)
		rest = strings.TrimPrefix(strings.TrimPrefix(rest, l), ", ")
	}
	return selected, rest
}

type Usage struct {
	InputTokens  uint64
	OutputTokens uint64
	CostUSD      float64
}

// Event is a normalized provider event.
type Event struct {
	Kind EventKind

	Ref string // EventRef
	// EventRef: the model and reasoning effort the agent says it runs
	// with, when it says (Claude Code tells only the model).
	Model, Effort string
	// EventRef of a new ACP session: what it offers.
	Offers *Offers

	Text string // AgentMessage, AgentDraft, TurnCompleted (result), Error

	CallID   string // ToolCall, ToolResult
	ToolKind ToolKind
	ToolName string
	Summary  string
	Decision *Decision // ToolCall: set when the gate decided

	IsError   bool
	Truncated bool
	Usage     Usage    // TurnCompleted
	Failure   *Failure // TurnCompleted with IsError, Error: why (nil: classify Text)
	Question  *Question

	// Background: a ToolResult that is the state of a call the agent left
	// running after it reported it (Antigravity's background tasks); a later
	// one for the same CallID replaces it. It may come from a process the
	// session has since let go: what became of the call is still so.
	Background bool
}

// Image is an attached image file to show the agent.
type Image struct {
	Path      string
	MediaType string
}

// File is an attached file the agent is not shown as an image: the text
// lists it by path; an agent whose protocol links files (ACP) gets it as a
// link too, or inline when it takes that kind (ACP audio).
type File struct {
	Path, Name, MediaType string
	Size                  int64
}

// ShownAsImage is how the text listing attached files marks an image the
// agent is also shown; an agent that turns out to take no images drops it.
const ShownAsImage = ", shown as an image"

// Input is a user message: its text (which lists attached files by path),
// the attached images and the other attached files.
type Input struct {
	Text   string
	Images []Image
	Files  []File
}

// StartSpec describes one provider process.
type StartSpec struct {
	WorkDir string
	// ModelScope names this session for scoped model observations, without
	// exposing its filesystem path to clients listing coding agents.
	ModelScope string
	TempDir    string
	Prompt     string
	Images     []Image // with the first prompt
	Files      []File  // with the first prompt
	// ReadDirs are directories outside WorkDir the agent must be able to
	// read (the session's attached files).
	ReadDirs []string
	Model    string
	// Effort and Speed, like Model, override the agent's own configuration
	// for this session only (Claude Code and Codex; Speed: Codex).
	Effort string
	Speed  identityv1.AgentSpeed
	// Mode, like Model, is the session mode (ACP agents).
	Mode string
	// ResumeRef resumes a previous provider session (Claude session id or
	// Codex thread id) instead of starting a new one.
	ResumeRef string
	// NewRef is the session id to use for a new Claude session.
	NewRef string
	// Title is the session's title, for agents whose own apps list their
	// sessions by name (Codex).
	Title string
	Env   []string
	Gate  Gate
	// MCPServers are tools Aide gives the agent, as stdio MCP servers:
	// the session's messages (docs/agent-messages.md §7.4).
	MCPServers []MCPServer
	// Instructions are added to the agent's system prompt: a managed
	// workspace's section (docs/workspace-requests.md). Claude Code and
	// Codex take them; ACP has no place for them.
	Instructions string
	// Project and Requester (its client's name; "" for the owner's own
	// devices) say what a new session is for, for agents whose own apps
	// list it apart from Aide (the Claude desktop app).
	Project, Requester string
}

// MCPServer is a stdio MCP server the agent starts.
type MCPServer struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
}

// Session is a running provider process. The first turn starts with
// StartSpec.Prompt; Send starts the next turn once the previous completed.
type Session interface {
	Events() <-chan Event
	Send(ctx context.Context, in Input) error
	// Steer adds a message to the running turn. The turn's EventTurnCompleted
	// then comes only after the agent has handled it. An error means the
	// message was not taken (no turn is running, or the agent cannot take
	// input mid-turn); the caller delivers it as the next turn instead.
	Steer(ctx context.Context, in Input) error
	// Answer resolves a question from EventQuestion; the agent goes on.
	Answer(ctx context.Context, questionID string, r Reply) error
	Interrupt(ctx context.Context) error
	// Close stops the process (gracefully, then by killing its group).
	Close() error
	// Done is closed when the process has exited; Err then reports why.
	Done() <-chan struct{}
	Err() error
	Pgid() int
	// LastActivity is when the agent last produced any output, including
	// output that becomes no event (zero: none yet). The kernel's liveness
	// checks use it.
	LastActivity() time.Time
	// Diagnose says what the agent's process is doing now, for the report
	// of a stall.
	Diagnose() Diagnosis
}

type Provider interface {
	Name() string
	Discover(ctx context.Context) Info
	Start(ctx context.Context, spec StartSpec) (Session, error)
}

// ModeSetter is a Session whose mode (an ACP agent's permission or approval
// preset) can change while it runs (工单 #170): its next tool call is
// decided by the new one; a call already decided is not. ErrModeFixed: this
// agent's mode is set when its process starts, and stays.
type ModeSetter interface {
	SetMode(ctx context.Context, mode string) error
}

// ErrModeFixed is a SetMode on an agent whose mode can't change once it runs.
var ErrModeFixed = errors.New("this agent's mode is chosen when its session starts and can't change while it runs")

// FixedModes is a Provider whose sessions keep the mode they started with
// (DeepSeek Harness's presets, 工单 #170), resumed ones too.
type FixedModes interface {
	ModesFixed() bool
}

// Titler is a Session that can rename its conversation in the agent's own
// apps while it runs (Codex).
type Titler interface {
	SetTitle(ctx context.Context, title string) error
}

// Backgrounder is a Session whose agent can leave work running after a
// turn (Antigravity's background tasks). Background reports whether some
// still runs: its idle process is kept then, as stopping it would stop
// the work too.
type Backgrounder interface {
	Background() bool
}

// Archiver is a Provider whose own apps list its conversations: archiving
// a session there too keeps those lists to the sessions still in use
// (Codex). ref is the session's provider reference.
type Archiver interface {
	Archive(ctx context.Context, ref string, archived bool) error
}

// ArchiveReader is an Archiver whose apps keep whether each conversation is
// archived, which the owner can change there: Aide follows it (工单 #67,
// archived on one side is archived on the other). Archived answers for the
// refs its apps know, leaving out those they don't; nil when its apps are
// not there.
type ArchiveReader interface {
	Archived(refs []string) map[string]bool
}

// ConversationRollbacker declares whether a provider can roll back (or
// cleanly reset and re-anchor) its conversation state together with a
// workspace checkpoint rollback (工单 #113). Providers returning false cause
// Aide to refuse workspace file rollback so files and conversation never diverge.
type ConversationRollbacker interface {
	CanRollbackConversation() (bool, string)
}

// Markdown that CleanTitle drops from a title: a line that is only a code
// fence or a rule, block markers at the start (heading, quote, list item),
// links (keeping their text), ** * ~~ emphasis and code marks. Underscores
// stay (no _emphasis_), so names like __init__.py and snake_case keep them.
var (
	mdSkip   = regexp.MustCompile("^(```|~~~|(-\\s*){3,}$|(\\*\\s*){3,}$|(_\\s*){3,}$)")
	mdBlock  = regexp.MustCompile(`^(#{1,6}\s+|>\s*|[-*+]\s+|\d+[.)]\s+|\[[ xX]\]\s+)+`)
	mdLink   = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdStrong = regexp.MustCompile(`(\*\*|~~)(\S(?:.*?\S)?)(\*\*|~~)`)
	mdEm     = regexp.MustCompile(`(^|[^\w*])\*(\S(?:[^*]*?\S)?)\*`)
	mdSpace  = regexp.MustCompile(`\s+`)
)

// CleanTitle makes text one short line for a list: its first line with
// words in it, without Markdown, wrapping quotes or a final period, cut to
// 40 characters.
func CleanTitle(s string) string {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if mdSkip.MatchString(t) {
			continue
		}
		t = mdBlock.ReplaceAllString(t, "")
		t = mdLink.ReplaceAllString(t, "$1")
		t = mdStrong.ReplaceAllString(t, "$2")
		t = mdEm.ReplaceAllString(t, "$1$2")
		t = strings.ReplaceAll(t, "`", "")
		t = mdSpace.ReplaceAllString(strings.TrimSpace(t), " ")
		t = strings.TrimRight(t, "。.")
		t = strings.TrimSpace(strings.Trim(t, "\"'“”‘’「」"))
		t = strings.TrimSpace(strings.TrimRight(t, "。."))
		if t == "" {
			continue
		}
		if r := []rune(t); len(r) > 40 {
			return strings.TrimSpace(string(r[:40])) + "…"
		}
		return t
	}
	return ""
}
