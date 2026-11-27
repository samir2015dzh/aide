// Package machine implements the execution-machine role of aided: it pairs
// with the owner, keeps an outbound connection to the relay, terminates
// end-to-end tunnels and runs coding-agent sessions.
package machine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/acpregistry"
	"github.com/samir2015dzh/aide/internal/flock"
	"github.com/samir2015dzh/aide/internal/legacy"
	"github.com/samir2015dzh/aide/internal/modelcatalog"
	"github.com/samir2015dzh/aide/internal/msgchain"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/acp"
	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
	"github.com/samir2015dzh/aide/internal/provider/claude"
	"github.com/samir2015dzh/aide/internal/provider/codex"
	"github.com/samir2015dzh/aide/internal/resident"
	"github.com/samir2015dzh/aide/internal/shellenv"
	"github.com/samir2015dzh/aide/pkg/identity"
	"github.com/samir2015dzh/aide/pkg/transport"
)

// Version is set at build time.
var Version = "0.1.0-dev"

type Config struct {
	DataDir    string
	Logger     *slog.Logger
	Providers  map[string]provider.Provider
	Limits     Limits
	HTTPClient *http.Client
	// WorkspaceMode "none" disables git worktrees (benchmarking only).
	WorkspaceMode string
	// SelfErase: a machine on its own (not a trusted device's) stops with
	// ErrErased when its owner erased everything, to be erased (Erase).
	SelfErase bool
	// ClaudeUsage turns on reading the Claude plan usage on this machine
	// (claudeusage.go): the account's OAuth token is read locally to fetch
	// the five-hour and weekly windows, with auto-renew. Off by default so
	// tests make no network calls; aided turns it on.
	ClaudeUsage bool
	// ModelCatalogSync enables background 24h sync of open model metadata
	// (models.dev + litellm) in Machine.Run. Off by default so tests make no
	// network calls; aided turns it on.
	ModelCatalogSync bool
	// ModelCatalog overrides the default open model catalog (for tests).
	ModelCatalog *modelcatalog.Catalog
	// UpdateFailed is the version this machine updated itself to and went
	// back from (internal/update, 工单 #162): a problem in its status.
	UpdateFailed string
}

func DefaultProviders() map[string]provider.Provider {
	// Each looks on the login shell's PATH as it is when it looks.
	ps := map[string]provider.Provider{
		"claude": &claude.Provider{},
		"codex":  &codex.Provider{},
	}
	for _, p := range acp.Providers("") {
		ps[p.Name()] = p
	}
	return ps
}

// AgentNames lists the agents in display order: Claude Code and Codex,
// then the ACP agents.
func AgentNames() []string {
	names := []string{"claude", "codex"}
	for _, a := range acp.Agents {
		names = append(names, a.Name)
	}
	return names
}

type Machine struct {
	// Rooms this machine hosts (rooms.go), read on first use.
	roomsOnce sync.Once
	roomsBook *roomBook
	cfg       Config
	log       *slog.Logger
	st        *Store
	key       *identity.SoftwareKey
	machineID string
	audit     *Audit
	sessions  *Sessions
	catalog   *modelcatalog.Catalog
	acpRegMu  sync.Mutex
	acpReg    *acpregistry.Registry

	mu          sync.Mutex
	trust       *identity.Trust
	ownerID     string
	relayURL    string
	machineCert *identityv1.Signed
	machineName string
	revocations *identityv1.RevocationList
	grants      []*identityv1.Grant         // the grants that cover this machine (grants.go)
	health      []*machinev1.MachineProblem // what keeps it from working (health.go)
	healthAt    time.Time                   // when it was read
	usage       *machinev1.ClaudeUsage      // last Claude plan usage reading (claudeusage.go)
	usageAt     time.Time                   // when a reading was last attempted
	usageFetch  bool                        // a usage fetch is under way
	usageSrc    usageSource                 // reads plan usage from the endpoint; nil with usageCLI nil disables it
	usageCLI    cliUsageSource              // reads it through Claude Code here (工单 #86)
	usageCache  cacheUsageSource            // reads what Claude Code here cached of it (工单 #104)
	usageNoCLI  map[string]bool             // Claude Code binaries@versions without /usage outside a terminal
	agentUsage  agentUsages                 // other agents' plan usage, as their checks answered it (工单 #143)
	releases    map[string]releaseRead      // what agents' release channels said (工单 #178, agentupdate.go)
	updating    map[string]bool             // agents whose CLI updater runs now
	paused      bool
	infos       map[string]provider.Info // what the last check of each coding agent found
	dshBridge   *machinev1.DSHBridge     // the DSH bridge plugin, as the last check of DSH found it
	logins      map[string]*agentLogin   // sign-ins started from Aide (agentlogin.go)
	hidden      map[string]hiddenAgent   // the coding agents the owner hid (hiddenagents.go)
	// The coding agents the owner paused, when each last resumed by itself,
	// and the timers that resume them (pausedagents.go).
	pausedAgents map[string]pausedAgent
	resumed      map[string]time.Time
	resumeTimers map[string]*time.Timer
	categories   map[string]string     // the kind the owner filed each coding agent as (agentcategory.go)
	checked      time.Time             // when the last check of all of them ended
	checking     chan struct{}         // closed when the check under way ends; nil when none is
	checkingOn   bool                  // the check under way was asked for on purpose (工单 #112)
	pending      map[string]bool       // the agents the check under way has not finished yet
	kept         map[string]keptModels // what a check on purpose found of each agent's models (passive.go)
	defaults     map[string]*identityv1.AgentDefaults
	tunnels      map[*tunnel]struct{}
	connected    bool
	refused      bool       // the relay refused admission when last connecting
	erased       bool       // the owner's erase order came (SelfErase)
	goneSaid     bool       // the log said the owner is gone and how to pair again
	clientsMu    sync.Mutex // rememberClient
	adminSeen    map[string]time.Time
	// grantSeen caches the highest version of each grant recorded in the
	// store, so only first sight and upgrades touch the database.
	grantSeen  map[string]uint64
	statusKick chan struct{}
	lock       *flock.Lock
	// conn is the relay connection while there is one (notices go over it).
	conn    *transport.RelayConn
	notices notices
	// contacts caches the owner's inboxes, for the news it writes (news.go).
	contacts contactBook
	// lettersSeen are the messages delivered to sessions, or being
	// delivered, since it started, and when (letters.go): one the relay
	// hands over again is not delivered twice.
	lettersSeen     map[string]time.Time
	lettersPrunedAt time.Time
	// The chains of messages that reached the sessions, the last one each
	// session took (what it writes goes on with), and the messages handed
	// to each session's turn, acknowledged handled once it ends (chains.go).
	chains      msgchain.Guard
	lastChain   map[string]chainAt
	handedTurns map[string][]string
	// inboxWake tells the inbox's follower to try the messages it holds
	// again: the machine resumed.
	inboxWake chan struct{}
	// dshPending: a message waits for DSH Desktop (dshmail.go); dshOut:
	// when each DSH session sent through Aide, the last hour.
	dshPending bool
	// heldMu: one change to the messages held for the owner at a time (held.go).
	heldMu sync.Mutex
	dshOut map[string][]time.Time
	// tools is the sessions' own tools socket (sessiontools.go).
	tools sessionTools
	// wsReqs are the managed workspaces' requests (workspacereq.go).
	wsReqs workspaceRequests
	// herData: Her was on here once, before Aide dropped her, and her home is
	// still there (HerStatus.data; herlegacy.go).
	herData bool
	// directPort is the ephemeral TCP port of serveDirect when listening.
	directPort atomic.Int32
	// offers are what ACP agents' sessions offered (defaults.go).
	offers      map[string]provider.Offers
	offersAt    map[string]time.Time // when a session offered them (zero: kept by an earlier Aide)
	probeLogged map[string]time.Time // checkedOffers
	probeFailed map[string]string
	// customs are the agents the owner added here and custom their
	// providers (customagents.go); customMu is never held while taking mu.
	customMu sync.RWMutex
	customs  customAgents
	custom   map[string]provider.Provider
}

const (
	kOwnerID     = "owner_id"
	kRelayURL    = "relay_url"
	kRootChain   = "root_chain"
	kMachineCert = "machine_cert"
	kRevocations = "revocations"
	kPaused      = "paused"
	kName        = "name"
)

func KeyPath(dataDir string) string { return filepath.Join(dataDir, "keys", "machine.key") }

// storeName is the machine's database in its data directory.
const storeName = "aided.db"

// keepStore renames the database of a machine from before the rename
// (internal/legacy) to storeName while there is none of that name: its
// sessions, settings and agents go on. The database itself goes last, so
// that a rename cut short is done again at the next start.
func keepStore(dataDir string) error {
	old := filepath.Join(dataDir, legacy.Daemon+".db")
	now := filepath.Join(dataDir, storeName)
	if _, err := os.Stat(now); err == nil {
		return nil
	}
	if _, err := os.Stat(old); err != nil {
		return nil
	}
	for _, ext := range []string{"-wal", "-shm", ""} {
		if err := os.Rename(old+ext, now+ext); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("the machine's database from before the rename: %w", err)
		}
	}
	return nil
}

// Open loads machine state from dataDir (creating the machine key if needed).
func Open(cfg Config) (*Machine, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Providers == nil {
		cfg.Providers = DefaultProviders()
	}
	if cfg.Limits == (Limits{}) {
		cfg.Limits = DefaultLimits()
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	lock, err := flock.Acquire(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	if err := keepStore(cfg.DataDir); err != nil {
		lock.Release()
		return nil, err
	}
	st, err := OpenStore(filepath.Join(cfg.DataDir, storeName))
	if err != nil {
		lock.Release()
		return nil, err
	}
	key, err := identity.LoadOrCreateKey(KeyPath(cfg.DataDir))
	if err != nil {
		return nil, err
	}
	n := &Machine{cfg: cfg, log: cfg.Logger, st: st, key: key, lock: lock, inboxWake: make(chan struct{}, 1), machineID: identity.MachineID(identity.MarshalPublicKey(key.Public())),
		infos: map[string]provider.Info{}, logins: map[string]*agentLogin{}, usageNoCLI: map[string]bool{}, agentUsage: agentUsages{}, releases: map[string]releaseRead{}, updating: map[string]bool{}, hidden: map[string]hiddenAgent{}, offers: map[string]provider.Offers{}, offersAt: map[string]time.Time{}, probeLogged: map[string]time.Time{}, probeFailed: map[string]string{}, defaults: map[string]*identityv1.AgentDefaults{}, tunnels: map[*tunnel]struct{}{}, adminSeen: map[string]time.Time{}, grantSeen: map[string]uint64{}, kept: map[string]keptModels{},
		statusKick: make(chan struct{}, 1), revocations: &identityv1.RevocationList{MinGrantVersions: map[string]uint64{}}}
	if n.audit, err = OpenAudit(filepath.Join(cfg.DataDir, "audit"), key); err != nil {
		return nil, err
	}
	n.catalog = cfg.ModelCatalog
	if n.catalog == nil {
		n.catalog = modelcatalog.Open(modelcatalog.Options{
			CachePath:  filepath.Join(cfg.DataDir, "model-catalog.json"),
			HTTPClient: cfg.HTTPClient,
			Logger:     n.log,
		})
	}
	n.loadCustomAgents()
	if err := n.loadState(); err != nil {
		return nil, err
	}
	ws := NewWorkspace(filepath.Join(cfg.DataDir, "worktrees"))
	ws.none = cfg.WorkspaceMode == "none"
	n.sessions = &Sessions{st: st, ws: ws, audit: n.audit, log: n.log,
		machineID: n.machineID, dataDir: cfg.DataDir, provider: n.provider, info: n.providerInfo, limits: cfg.Limits,
		paused: n.isPaused, agentPaused: n.agentPaused, defaults: n.agentDefaults, outOfProcesses: n.outOfProcesses, live: map[string]*live{},
		memoryLow: resident.MemoryLow}
	n.sessions.w = newWriter(st, n.log, n.sessions.deliver, n.sessions.changed)
	n.sessions.mcp = n.mcpServers
	n.sessions.gone = n.forgetSession
	n.sessions.requester = func(id string) string { return n.knownClients()[id].Label }
	if c, ok := cfg.Providers["claude"].(*claude.Provider); ok && c.Log == nil {
		c.Log = n.log
	}
	if cfg.ClaudeUsage {
		n.usageSrc = &claude.UsageReader{AutoRenew: true, HTTP: cfg.HTTPClient, Log: n.log}
		n.usageCLI = &claude.CLIUsage{}
		n.usageCache = &claude.CacheUsage{}
		n.loadUsageSources()
	}
	n.sessions.offered = n.offered
	n.sessions.signedIn = n.agentSignedIn
	n.sessions.estimateCost = n.estimateTurnCost
	n.sessions.notice = func(s noticeSession, e *sessionv1.SessionEvent) {
		n.sessionNotice(s, e)
		n.sessionNews(s, e)
		n.roomTurn(s, e) // a session that talks for a room (rooms.go)
		if _, ok := e.Kind.(*sessionv1.SessionEvent_TurnCompleted); ok {
			n.turnEnded(s.id)
		}
	}
	n.sessions.registerMetrics()
	return n, nil
}

func (n *Machine) loadState() error {
	n.loadDefaults()
	n.loadHiddenAgents() // the machine's own, logged in or not
	n.loadPausedAgents()
	n.loadAgentCategories()
	was, _ := n.st.Get(kHer)
	n.herData = len(was) > 0 && herHomeExists()
	n.forgetHer() // whoever the owner is, or none yet
	owner, _ := n.st.Get(kOwnerID)
	relay, _ := n.st.Get(kRelayURL)
	n.ownerID, n.relayURL = string(owner), string(relay)
	if n.ownerID == "" {
		return nil
	}
	n.trust = identity.NewTrust(n.ownerID)
	rc := &identityv1.RootChain{}
	if ok, err := n.st.GetProto(kRootChain, rc); err != nil || !ok {
		return fmt.Errorf("stored root chain missing: %v", err)
	}
	if _, err := n.trust.AcceptRootChain(rc); err != nil {
		return fmt.Errorf("stored root chain: %w", err)
	}
	nc := &identityv1.Signed{}
	if ok, _ := n.st.GetProto(kMachineCert, nc); ok {
		if _, _, err := n.trust.VerifyMachineCert(nc); err == nil {
			n.machineCert = nc
		}
	}
	if name, _ := n.st.Get(kName); len(name) > 0 {
		n.machineName = string(name)
	}
	rev := &identityv1.RevocationList{}
	if ok, _ := n.st.GetProto(kRevocations, rev); ok {
		if rev.MinGrantVersions == nil {
			rev.MinGrantVersions = map[string]uint64{}
		}
		n.revocations = rev
	}
	p, _ := n.st.Get(kPaused)
	n.paused = string(p) == "1"
	n.loadGrants()
	return nil
}

func (n *Machine) Close() error {
	n.stopResumeTimers()
	n.sessions.w.close()
	err := n.st.Close()
	n.audit.Close()
	n.lock.Release()
	return err
}

func (n *Machine) MachineID() string { return n.machineID }

var nameRE = regexp.MustCompile(`[^a-z0-9-]+`)

// SanitizeName turns s into a machine name: lowercase letters, digits and
// '-', at most 63 bytes ("machine" if nothing is left).
func SanitizeName(s string) string {
	if s = CleanName(s); s == "" {
		s = "machine"
	}
	return s
}

// CleanName is SanitizeName without the fallback: "" when nothing is left
// (a name all Chinese, say), which a rename refuses.
func CleanName(s string) string {
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".local")
	s = strings.Trim(nameRE.ReplaceAllString(s, "-"), "-")
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-")
	}
	return s
}

// SetName sets the machine's name, a setting: the name it reports and
// presents to agents on their end-to-end connections. An agent holds the
// machine to the name it presented when the connection was made, so the
// agents' connections made under another name are closed: one kept from
// command to command (the MCP gateway's, the CLI link's) would go on saying
// the old name, and the agent refuse the machine under its new one.
func (n *Machine) SetName(name string) error {
	name = SanitizeName(name)
	if err := n.st.Set(kName, []byte(name)); err != nil {
		return err
	}
	n.mu.Lock()
	renamed := n.machineName != name
	n.machineName = name
	var close []*tunnel
	if renamed {
		for t := range n.tunnels {
			if t.principal.Kind == PrincipalClient {
				close = append(close, t)
			}
		}
	}
	n.mu.Unlock()
	for _, t := range close {
		t.conn.Close()
	}
	return nil
}

// OwnerID is the owner this machine is paired with ("" if none).
func (n *Machine) OwnerID() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.ownerID
}

func (n *Machine) Paired() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.machineCert != nil
}

func (n *Machine) Name() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.machineName
}

func (n *Machine) isPaused() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.paused
}

func (n *Machine) Connected() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.connected
}

// providerInfo is what the last check found of a coding agent; one it found
// missing, or never checked, is checked again now, so a session with an
// agent installed since is not refused.
func (n *Machine) providerInfo(name string) provider.Info {
	n.mu.Lock()
	i, ok := n.infos[name]
	p := n.provider(name)
	n.mu.Unlock()
	if (ok && i.Available) || p == nil {
		return i
	}
	i = n.discover(name, p, false)
	i.CheckedAt = time.Now()
	n.mu.Lock()
	i = n.withOffers(name, i)
	n.infos[name] = i
	n.mu.Unlock()
	return i
}

// agentSignedIn is what a session of the agent just showed of its sign-in
// (工单 #108): one that failed "Not logged in" is signed out now, one that
// ran a turn is signed in; its status says so at once, not at its next check
// (MachineService.Watch sends it within statusEvery; no ReportStatus, 工单 #143).
// What it showed is kept (passive.go) for an agent whose check cannot tell
// (工单 #112: an ACP agent's).
func (n *Machine) agentSignedIn(name string, in bool) {
	n.sawSignIn(name, in)
	n.mu.Lock()
	i, ok := n.infos[name]
	changed := ok && i.Available && (i.LoggedIn != in || i.LoginUnconfirmed)
	if changed {
		i.LoggedIn, i.LoginUnconfirmed = in, false
		n.infos[name] = i
	}
	n.mu.Unlock()
}

// kickStatus has ReportStatus sent again (link.go). It carries only the
// name and the version, so only a rename kicks it (工单 #143): a change to
// sessions, agents, sign-ins, health or settings is not the relay's to know,
// and owners see it in MachineService.Watch, which compares the state every
// statusEvery (feed.go). Each needless kick was a call to the relay and a
// wake of every owner's WatchOwner for lists that had not changed.
func (n *Machine) kickStatus() {
	select {
	case n.statusKick <- struct{}{}:
	default:
	}
}

func (n *Machine) credentials() *identityv1.Credentials {
	n.mu.Lock()
	defer n.mu.Unlock()
	return &identityv1.Credentials{RootChain: n.trust.RootChain(), MachineName: n.machineName, Subject: &identityv1.Credentials_MachineCert{MachineCert: n.machineCert}}
}

// The coding agents are checked (installed, version, logged in: each CLI is
// run) only when something needs to know: the app showing them, an agent
// asking where it can work, a session about to start. Nothing checks them
// in the background. A check is passive (工单 #112, provider/passive.go)
// unless the owner asks for it on purpose.

// AgentsFresh is how long a check stands for agents asking where they can
// work; past it, they wait for a new one.
const AgentsFresh = 5 * time.Minute

// AgentsOnce, as CheckAgents' maxAge: check only if no check has run yet.
const AgentsOnce = time.Duration(math.MaxInt64)

// CheckAgents starts a passive check of every coding agent unless one is
// under way, or (!force) the last one ended within maxAge; the channel
// closes when the coding agents are checked. Each agent shows in Status as
// soon as its own check ends.
func (n *Machine) CheckAgents(force bool, maxAge time.Duration) <-chan struct{} {
	return n.checkAgents(force, false, maxAge)
}

// CheckAgentsOnPurpose is the owner's Check Again (工单 #112): every coding
// agent checked now, and asked what only a running one tells (its models).
func (n *Machine) CheckAgentsOnPurpose() <-chan struct{} {
	return n.checkAgents(true, true, 0)
}

func (n *Machine) checkAgents(force, onPurpose bool, maxAge time.Duration) <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.checking != nil {
		if !onPurpose || n.checkingOn {
			return n.checking
		}
		// A passive check under way does not ask what the owner asked for: one on purpose follows it.
		under, done := n.checking, make(chan struct{})
		go func() {
			<-under
			<-n.checkAgents(true, true, 0)
			close(done)
		}()
		return done
	}
	if !force && !n.checked.IsZero() && time.Since(n.checked) < maxAge {
		done := make(chan struct{})
		close(done)
		return done
	}
	done := make(chan struct{})
	n.checking, n.checkingOn = done, onPurpose
	n.pending = map[string]bool{}
	ps := n.providers()
	for name := range ps {
		n.pending[name] = true
	}
	go func() {
		var wg sync.WaitGroup
		for name, p := range ps {
			wg.Add(1)
			go func() {
				defer wg.Done()
				info := n.discover(name, p, onPurpose)
				info.CheckedAt = time.Now()
				var bridge *machinev1.DSHBridge
				if name == "deepseek" && info.Available {
					// The plugin, with DSH: its part of whether DSH is
					// fully connected here (工单 #46, #49).
					if home, err := dshnative.Home(shellenv.Environ()); err == nil {
						bridge = n.dshBridgeState(context.Background(), home)
					}
				}
				n.mu.Lock()
				if n.provider(name) == p { // not removed or changed meanwhile
					n.checkedOffers(name, p, info)
					n.infos[name] = n.withOffers(name, info)
					if name == "deepseek" {
						n.dshBridge = bridge
					}
				}
				delete(n.pending, name)
				n.mu.Unlock()
				// Its release channel, once what it found shows: a slow
				// network holds back no status, only the check's end.
				if n.readRelease(name, p, info, onPurpose) {
					n.kickStatus()
				}
			}()
		}
		wg.Wait()
		n.mu.Lock()
		short := false // an agent could not start for want of processes: say so now
		for _, i := range n.infos {
			short = short || i.Problem == provider.ProblemNoProcesses
		}
		n.checked = time.Now()
		n.checking, n.checkingOn = nil, false
		n.mu.Unlock()
		close(done)
		if short {
			n.refreshHealth()
		}
	}()
	return done
}

// Discover checks every coding agent now and returns what it found.
func (n *Machine) Discover(ctx context.Context) map[string]provider.Info {
	select {
	case <-n.CheckAgents(true, 0):
	case <-ctx.Done():
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return maps.Clone(n.infos)
}

func (n *Machine) Status() *machinev1.MachineStatus {
	n.freshHealth()
	n.mu.Lock()
	st := &machinev1.MachineStatus{Name: n.machineName, ProtocolVersion: transport.ProtocolVersion, Platform: runtime.GOOS + "/" + runtime.GOARCH,
		MaxSessions: uint32(n.cfg.Limits.MaxConcurrent), Paused: n.paused, Version: Version}
	for _, name := range append(AgentNames(), "fake") {
		// Claude Code and Codex are always listed; other agents when found
		// (working or not), when their app lacks the CLI, or while their
		// first check is under way.
		i, ok := n.infos[name]
		if (ok && (i.Available || i.Problem != provider.ProblemNotFound || name == "claude" || name == "codex")) || (!ok && n.pending[name]) {
			a := &machinev1.AgentStatus{Name: name, Available: i.Available, Version: i.Version,
				LoggedIn: i.LoggedIn, LoginUnconfirmed: i.LoginUnconfirmed, Detail: i.Detail, Checking: n.pending[name], Problem: machinev1.AgentProblem(i.Problem), Binary: i.Binary,
				Hidden: n.isHiddenLocked(name), Source: "builtin"}
			a.Capabilities, a.ProtocolVersion = n.truthfulCapabilities(name, n.cfg.Providers[name], i)
			a.Pause, a.AutoResumedAt = n.pauseOfLocked(name)
			a.Category = n.categories[name]
			n.agentModels(a, i)
			if !i.CheckedAt.IsZero() {
				a.CheckedAt = timestamppb.New(i.CheckedAt)
			}
			a.Update = n.updateOfLocked(name, i.Version)
			a.Usage = n.agentUsage[name] // asked at its check (工单 #143); Claude Code's below
			if name == "claude" {
				a.Usage = n.usage // the last reading; a refresh is kicked below
				a.Login = n.loginOfLocked(name)
				if ok {
					a.Desktop = &machinev1.ClaudeDesktop{Installed: i.DesktopInstalled, SignedIn: i.DesktopSignedIn}
				}
			}
			if name == "deepseek" {
				a.DshBridge = n.dshBridge
			}
			st.Agents = append(st.Agents, a)
		}
	}
	for _, name := range n.customNames() {
		// Always listed: the owner added it. Never checked yet, it is
		// about to be.
		i, ok := n.infos[name]
		src, regID, regVer := n.customMeta(name)
		a := &machinev1.AgentStatus{Name: name, Available: i.Available, Version: i.Version, LoggedIn: i.LoggedIn, LoginUnconfirmed: i.LoginUnconfirmed, Detail: i.Detail,
			Checking: n.pending[name] || !ok, Problem: machinev1.AgentProblem(i.Problem), Binary: i.Binary, Custom: true, Hidden: n.isHiddenLocked(name),
			Source: src, RegistryId: regID, RegistryVersion: regVer}
		a.Capabilities, a.ProtocolVersion = n.truthfulCapabilities(name, n.provider(name), i)
		if !i.CheckedAt.IsZero() {
			a.CheckedAt = timestamppb.New(i.CheckedAt)
		}
		a.Pause, a.AutoResumedAt = n.pauseOfLocked(name)
		a.Category = n.categories[name]
		n.agentModels(a, i)
		st.Agents = append(st.Agents, a)
	}
	st.Problems = n.health
	kickUsage := n.usageDue()
	n.mu.Unlock()
	if kickUsage {
		go n.refreshUsage()
	}
	st.RunningSessions = uint32(n.sessions.Running())
	st.Sessions = n.sessions.Counts()
	st.Her = n.herStatus()
	st.DirectAddrs = n.directAddresses()
	st.Rooms = n.roomsInStatus()
	st.RoomOutbox = n.roomOutboxInStatus()
	st.ModelCatalog = n.modelCatalogStatus()
	return st
}

// Run serves until ctx is done.
func (n *Machine) Run(ctx context.Context) error {
	if !n.Paired() {
		return errors.New("this machine is not logged in yet; run `aided login <server>`")
	}
	if err := n.sessions.Recover(); err != nil {
		return err
	}
	go n.sessions.Janitor(ctx)
	go n.sessions.CountWork(ctx)
	go n.serveSessionTools(ctx)
	go n.serveControl(ctx)
	go n.serveLocal(ctx)
	go n.serveDirect(ctx)
	go n.serveDSH(ctx)
	go n.serveHeld(ctx)
	go n.roomWatch(ctx)
	go n.archiveWatch(ctx)
	if n.cfg.ModelCatalogSync && n.catalog != nil {
		go n.catalog.Run(ctx)
	}
	n.log.Info("aided machine running", "machine", n.machineID, "name", n.Name(), "relay", n.relayURL)
	n.runLink(ctx)
	n.mu.Lock()
	erased := n.erased
	n.mu.Unlock()
	if erased {
		return ErrErased
	}
	return nil
}

// applyRevocations merges r (which can only reduce permissions), closes
// affected tunnels and cancels sessions of revoked agents.
func (n *Machine) applyRevocations(r *identityv1.RevocationList) {
	if r == nil {
		return
	}
	n.mu.Lock()
	cur := proto.Clone(n.revocations).(*identityv1.RevocationList)
	before := proto.Clone(cur).(*identityv1.RevocationList)
	mergeRevocations(cur, r)
	if proto.Equal(cur, before) {
		n.mu.Unlock()
		return
	}
	n.revocations = cur
	var close []*tunnel
	for t := range n.tunnels {
		if t.principal.Kind == PrincipalClient && n.grantProblemLocked(t.principal.Grant) != nil ||
			t.principal.DeviceKey != "" && slices.Contains(cur.RevokedDeviceKeys, t.principal.DeviceKey) {
			close = append(close, t)
		}
	}
	n.mu.Unlock()
	n.st.SetProto(kRevocations, cur)
	for _, t := range close {
		t.conn.Close()
	}
	for _, id := range r.RevokedGrantIds {
		for _, c := range n.clientsForGrant(id) {
			n.sessions.InterruptByClient(c, "agent access revoked")
		}
	}
	n.audit.Log(AuditRecord{Kind: "revocations", Detail: fmt.Sprintf("grants=%v machines=%v devices=%v min=%v", cur.RevokedGrantIds, cur.RevokedMachineIds, cur.RevokedDeviceKeys, cur.MinGrantVersions)})
}

// deviceSignedOut tells whether the owner's device with this device key
// id was signed out.
func (n *Machine) deviceSignedOut(keyID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Contains(n.revocations.GetRevokedDeviceKeys(), keyID)
}

func (n *Machine) clientsForGrant(grantID string) []string {
	rows, err := n.st.Sessions(`grant_id = ?`, grantID)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if !seen[r.CreatedBy] {
			seen[r.CreatedBy] = true
			out = append(out, r.CreatedBy)
		}
	}
	return out
}

func mergeRevocations(cur, in *identityv1.RevocationList) {
	add := func(dst *[]string, ids []string) {
		for _, id := range ids {
			found := false
			for _, x := range *dst {
				if x == id {
					found = true
					break
				}
			}
			if !found {
				*dst = append(*dst, id)
			}
		}
	}
	add(&cur.RevokedGrantIds, in.RevokedGrantIds)
	add(&cur.RevokedMachineIds, in.RevokedMachineIds)
	add(&cur.RevokedDeviceKeys, in.RevokedDeviceKeys)
	if cur.MinGrantVersions == nil {
		cur.MinGrantVersions = map[string]uint64{}
	}
	for k, v := range in.MinGrantVersions {
		if v > cur.MinGrantVersions[k] {
			cur.MinGrantVersions[k] = v
		}
	}
}

// grantProblemLocked reports why a grant may not be used now.
func (n *Machine) grantProblemLocked(g *identityv1.Grant) error {
	for _, id := range n.revocations.RevokedGrantIds {
		if id == g.GrantId {
			return errors.New("grant revoked")
		}
	}
	if g.Version < n.revocations.MinGrantVersions[g.GrantId] {
		return errors.New("grant superseded by a newer version")
	}
	if g.ExpiresAt != nil && time.Now().After(g.ExpiresAt.AsTime().Add(identity.ClockSkew)) {
		return errors.New("grant expired")
	}
	if !identity.Allows(g.MachineIds, n.machineID) {
		return errors.New("grant does not cover this machine")
	}
	return nil
}

// grantSeenKept is how many grants' versions the cache holds.
const grantSeenKept = 1024

func (n *Machine) checkGrant(g *identityv1.Grant) error {
	n.mu.Lock()
	maxSeen, ok := n.grantSeen[g.GrantId]
	n.mu.Unlock()
	if !ok || g.Version > maxSeen {
		stored, err := n.st.SeeGrantVersion(g.GrantId, g.Version)
		if err != nil {
			return err
		}
		n.mu.Lock()
		if len(n.grantSeen) >= grantSeenKept {
			// Only a cache of the store's (SeeGrantVersion): every MCP
			// connection is a grant, so it is not let grow (work order #40).
			clear(n.grantSeen)
		}
		if cur, ok := n.grantSeen[g.GrantId]; !ok || stored > cur {
			n.grantSeen[g.GrantId] = stored
		}
		maxSeen = n.grantSeen[g.GrantId]
		n.mu.Unlock()
	}
	if g.Version < maxSeen {
		return errors.New("a newer version of this grant exists")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.grantProblemLocked(g)
}

func (n *Machine) RelayURL() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.relayURL
}

func (n *Machine) Projects() ([]Project, error) { return n.st.Projects() }

func (n *Machine) VerifyAudit() (int, error) {
	return VerifyAudit(filepath.Join(n.cfg.DataDir, "audit"), append([]identity.Signer{n.key}, retiredKeys(n.cfg.DataDir)...)...)
}

// modelCatalogStatus projects the open model catalog's cache/sync state into
// MachineStatus (工单 #114).
func (n *Machine) modelCatalogStatus() *machinev1.ModelCatalogSync {
	if n.catalog == nil {
		return nil
	}
	s := n.catalog.Status()
	out := &machinev1.ModelCatalogSync{
		Source:     s.Source,
		ModelCount: uint32(s.ModelCount),
		LastError:  s.LastError,
	}
	if !s.SyncedAt.IsZero() {
		out.SyncedAt = timestamppb.New(s.SyncedAt)
	}
	if !s.LastAttemptAt.IsZero() {
		out.LastAttemptAt = timestamppb.New(s.LastAttemptAt)
	}
	return out
}

// estimateTurnCost estimates USD cost from open reference pricing when the
// agent reported tokens without a cost (工单 #114). It resolves aliases via
// the agent's last discovered model list, using strict exact match only.
func (n *Machine) estimateTurnCost(agent, model string, inputTokens, outputTokens uint64) (float64, bool) {
	if n.catalog == nil || (inputTokens == 0 && outputTokens == 0) {
		return 0, false
	}
	resolved := ""
	n.mu.Lock()
	info := n.infos[agent]
	if model == "" {
		if d := n.defaults[agent]; d != nil && d.Model != "" {
			model = d.Model
		} else if info.Own.Model != "" {
			model = info.Own.Model
		}
	}
	for _, m := range info.Models {
		if strings.EqualFold(m.ID, model) && m.Resolved != "" {
			resolved = m.Resolved
			break
		}
	}
	n.mu.Unlock()
	return n.catalog.EstimateCostUSD(model, resolved, inputTokens, outputTokens)
}
