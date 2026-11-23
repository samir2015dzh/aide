// Package mcpgw is the MCP gateway: an OAuth 2.1 authorization server and
// an MCP resource server (Streamable HTTP) that let remote agent products
// (Claude, ChatGPT, Codex, Cursor, ...) use Aide. Each MCP connection is an
// Aide client whose key the gateway holds, made when the person logs in
// with the owner's account and password on the consent page: its tools run
// the aide CLI's commands for that client, against the machines its grant
// reaches. See docs/mcp-gateway.md.
//
// The gateway is embedded by a relay server, the way the hosted center
// embeds the relay: a self-hosted relay serves its one owner, the hosted
// center every account.
package mcpgw

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/samir2015dzh/aide/internal/legacy"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/samir2015dzh/aide/internal/cli"
	"github.com/samir2015dzh/aide/pkg/relay"
)

// Account is who signed in on the consent page.
type Account struct {
	ID string
	// OwnerID is "" while the account has no owner (Aide isn't set up
	// on a Mac yet).
	OwnerID string
	// Hint names the account, masked, for the consent page ("y***@gmail.com").
	Hint string
}

// Accounts tells the gateway who is who. The hosted center answers with
// its accounts; a self-hosted relay has one owner and no accounts.
type Accounts interface {
	// Browser returns the account signed in on this browser request; false
	// when nobody is (the gateway sends the browser to SignInURL).
	Browser(r *http.Request) (Account, bool)
	// SignInURL is where a browser signs in, coming back to next (a path).
	SignInURL(next string) string
	// Owner returns the owner of an account ("" while it has none).
	Owner(ctx context.Context, accountID string) (string, error)
}

type Config struct {
	// Issuer is the authorization server's URL, where the sign-in cookies
	// live (e.g. https://aide.io).
	Issuer string
	// ResourceURL is the MCP endpoint people paste into agent products
	// (e.g. https://mcp.aide.io/mcp).
	ResourceURL string
	// KeyedResource: ResourceURL's path holds a key only the owner knows
	// (a self-hosted relay, where anyone may connect): nothing served to
	// others names it, and an authorization must name it.
	KeyedResource bool
	// Relay is the relay this server runs: the account, grants,
	// revocations, presence and admission.
	Relay *relay.Server
	// RelayHandler serves the relay's endpoints; the gateway's clients dial
	// it in-process. Default: Relay.InProcessHandler().
	RelayHandler http.Handler
	Backend      Backend
	Vault        Vault
	Accounts     Accounts
	// WakePage is where an owner sets up a doorbell from an agent's link:
	// the code follows it (e.g. https://aide.io/w/). Empty for none: the
	// link is aide://wake?code=…, which the Aide app on a computer opens.
	WakePage string
	// VerifiedClients are the hosts whose Client ID Metadata Documents are
	// shown as verified (default: claude.ai, chatgpt.com, openai.com).
	VerifiedClients []string
	Logger          *slog.Logger

	// CIMDClient fetches Client ID Metadata Documents (default: one that
	// refuses private addresses). Tests set one that reaches a local server.
	CIMDClient *http.Client
	// WebhookClient rings the doorbells' webhooks (default: like
	// CIMDClient, no private addresses, no redirects).
	WebhookClient *http.Client
	// Now is the clock (tests).
	Now func() time.Time

	// CSRFKey keys the consent page's CSRF tokens; empty: a random one,
	// which only this process knows.
	CSRFKey []byte
	// Shared is what this gateway shares with the other nodes of a center
	// on several; nil: this process alone.
	Shared Shared
}

// Shared is what the gateways of a center on several nodes share: locks,
// leases and counters. Its errors leave each node to itself.
type Shared interface {
	// Lock takes a lock held across the nodes for at most ttl, waiting for
	// it until ctx ends.
	Lock(ctx context.Context, name string, ttl time.Duration) (unlock func(), err error)
	// Lead takes or keeps the lease name for this node for ttl; false while
	// another node holds it.
	Lead(name string, ttl time.Duration) bool
	// Claim takes name for ttl unless anyone holds it.
	Claim(name string, ttl time.Duration) bool
	// Allow counts a use of limit by key: false once n uses fell in one
	// window.
	Allow(limit, key string, n int, window time.Duration) (bool, error)
}

// Limits.
const (
	authorizationTTL = 30 * time.Minute
	codeTTL          = 5 * time.Minute
	accessTTL        = time.Hour
	// Tokens unused for this long stop working.
	refreshIdle        = 30 * 24 * time.Hour
	refreshRetryWindow = 60 * time.Second
	// Connections per owner.
	maxConnections = 10
	keyCacheTTL    = 10 * time.Minute
	// loopEvery spaces the loop's rounds; connLockTTL bounds how long a
	// connection's state change holds its lock on every node.
	loopEvery   = 5 * time.Second
	connLockTTL = 30 * time.Second
)

type Gateway struct {
	cfg      Config
	log      *slog.Logger
	db       Backend
	issuer   *url.URL
	resource *url.URL
	// secret keys the consent page's CSRF tokens; it changes on restart,
	// which only means a form open across a restart is posted again.
	secret []byte
	tools  []cli.Tool
	relay  *http.Client

	mu        sync.Mutex
	keys      map[string]cachedKey   // connection id → unsealed key
	peers     map[string]*keptPeer   // connection id → what its calls connect with (peers.go)
	connLocks map[string]*sync.Mutex // serialises a connection's state changes
	dcr       map[string][]time.Time // DCR registrations per IP
	inboxes   InboxBackend           // the connections' inboxes (inbox.go)
	doorbells DoorbellBackend        // every agent's doorbells (doorbells.go)
	rings     ringer
	stop      chan struct{}
	done      chan struct{}
}

func New(cfg Config) (*Gateway, error) {
	if cfg.Relay == nil || cfg.Backend == nil || cfg.Vault == nil || cfg.Accounts == nil {
		return nil, errors.New("mcpgw: Relay, Backend, Vault and Accounts are required")
	}
	issuer, err := url.Parse(strings.TrimSuffix(cfg.Issuer, "/"))
	if err != nil || issuer.Host == "" || (issuer.Scheme != "https" && issuer.Scheme != "http") {
		return nil, fmt.Errorf("mcpgw: bad issuer %q", cfg.Issuer)
	}
	resource, err := url.Parse(cfg.ResourceURL)
	if err != nil || resource.Host == "" || resource.Path == "" || resource.Path == "/" {
		return nil, fmt.Errorf("mcpgw: bad resource URL %q (it needs a path such as /mcp)", cfg.ResourceURL)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.RelayHandler == nil {
		cfg.RelayHandler = cfg.Relay.InProcessHandler()
	}
	if cfg.VerifiedClients == nil {
		cfg.VerifiedClients = []string{"claude.ai", "chatgpt.com", "openai.com"}
	}
	if cfg.CIMDClient == nil {
		cfg.CIMDClient = safeHTTPClient()
	}
	if cfg.WebhookClient == nil {
		cfg.WebhookClient = safeHTTPClient()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	g := &Gateway{cfg: cfg, log: cfg.Logger, db: cfg.Backend, issuer: issuer, resource: resource, secret: make([]byte, 32),
		tools: cli.Tools(), keys: map[string]cachedKey{}, peers: map[string]*keptPeer{}, connLocks: map[string]*sync.Mutex{}, dcr: map[string][]time.Time{},
		stop: make(chan struct{}), done: make(chan struct{})}
	if len(cfg.CSRFKey) > 0 {
		g.secret = cfg.CSRFKey
	} else {
		rand.Read(g.secret)
	}
	g.relay = inProcessClient(cfg.RelayHandler)
	switch b := cfg.Backend.(type) {
	case *SQLBackend:
		if err := b.ensureInboxes(context.Background()); err != nil {
			return nil, fmt.Errorf("mcpgw: inbox tables: %w", err)
		}
		g.inboxes, g.doorbells = b, b
	default:
		g.inboxes, g.doorbells = &memoryInboxes{}, &memoryDoorbells{}
		if ib, ok := b.(InboxBackend); ok {
			g.inboxes = ib
		}
		if db, ok := b.(DoorbellBackend); ok {
			g.doorbells = db
		}
	}
	cfg.Relay.SetMessageHook(g.onMessage)
	cfg.Relay.AddEraseHook(func(ownerID string) { g.OwnerGone(context.Background(), ownerID) })
	cfg.Relay.SetDoorbells(g)
	go g.loop()
	return g, nil
}

// Close stops the gateway's background work and closes the connections'
// peers.
func (g *Gateway) Close() {
	select {
	case <-g.stop:
	default:
		close(g.stop)
	}
	<-g.done
	g.closeIdlePeers(true)
}

// Mount adds the gateway's endpoints to mux.
func (g *Gateway) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", g.handleASMetadata)
	mux.HandleFunc("GET /oauth/authorize", g.handleAuthorize)
	mux.HandleFunc("POST /oauth/token", g.handleToken)
	mux.HandleFunc("POST /oauth/register", g.handleRegister)
	mux.HandleFunc("POST /oauth/revoke", g.handleRevoke)
	mux.HandleFunc("GET /oauth/client.json", g.handleHerClient)
	mux.HandleFunc("GET /oauth/consent", g.handleConsentPage)
	mux.HandleFunc("POST /oauth/consent", g.handleConsentAction)
	mux.HandleFunc("GET /oauth/consent/continue", g.handleConsentContinue)
	prm := "/.well-known/oauth-protected-resource"
	if !g.cfg.KeyedResource {
		mux.HandleFunc("GET "+prm, g.handlePRM)
	}
	mux.HandleFunc("GET "+prm+g.resource.Path, g.handlePRM)
	mux.Handle(g.resource.Path, g.mcpHandler())
}

// loop rings the doorbells due again, closes the peers no longer used and
// sweeps what has expired.
func (g *Gateway) loop() {
	defer close(g.done)
	if g.cfg.Relay.SingleOwner() != "" && g.lead() {
		g.renameHosted(context.Background()) // once, on a self-hosted relay: its server signs the grants
	}
	t := time.NewTicker(loopEvery)
	defer t.Stop()
	sweep := time.NewTicker(10 * time.Minute)
	defer sweep.Stop()
	for {
		select {
		case <-g.stop:
			return
		case <-t.C:
			g.ringAgainDue()
			g.closeIdlePeers(false)
		case <-sweep.C:
			g.sweepLimits()
			if !g.lead() {
				continue
			}
			if err := g.db.Sweep(context.Background(), g.cfg.Now()); err != nil {
				g.log.Warn("mcp sweep", "err", err)
			}
		}
	}
}

// OwnerGone ends the connections of an owner the server forgot (the owner
// started over, or erased everything): their grants mean nothing any more,
// and connecting again starts from the consent page. Their inboxes and the
// owner's doorbells (the bots' webhooks and their keys) go too.
func (g *Gateway) OwnerGone(ctx context.Context, ownerID string) {
	conns, err := g.db.ListConnections(ctx, ConnectionFilter{OwnerID: ownerID})
	if err != nil {
		g.log.Warn("mcp: ending a forgotten owner's connections", "owner", ownerID, "err", err)
		return
	}
	for _, c := range conns {
		if c.Live() {
			g.endConnection(ctx, c.ID, StateRevoked, "the owner started over")
		}
		if err := g.inboxes.DeleteInbox(ctx, c.ID); err != nil {
			g.log.Warn("mcp: forgetting an inbox", "connection", c.ID, "err", err)
		}
	}
	bells, err := g.doorbells.ListDoorbells(ctx, ownerID, "")
	if err != nil {
		g.log.Warn("mcp: forgetting a forgotten owner's doorbells", "owner", ownerID, "err", err)
		return
	}
	for _, d := range bells {
		if err := g.doorbells.DeleteDoorbell(ctx, ownerID, d.Inbox, d.Name); err != nil {
			g.log.Warn("mcp: forgetting a doorbell", "owner", ownerID, "err", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers.

func randomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// hashSecret is how secrets the gateway hands out are stored.
func hashSecret(domain, secret string) []byte {
	h := sha256.Sum256([]byte(legacy.MCPSecret + domain + "-v1\x00" + secret))
	return h[:]
}

func (g *Gateway) csrfToken(authzID string) string {
	m := hmac.New(sha256.New, g.secret)
	m.Write([]byte("consent\x00" + authzID))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// lead reports whether this node does the work one node does for all: the
// loop's waiting connections and sweep (always, without other nodes).
func (g *Gateway) lead() bool {
	return g.cfg.Shared == nil || g.cfg.Shared.Lead("mcp-loop", 3*loopEvery)
}

// connLock serialises the state changes of one connection, on every node.
func (g *Gateway) connLock(id string) sync.Locker {
	g.mu.Lock()
	defer g.mu.Unlock()
	l := g.connLocks[id]
	if l == nil {
		l = &sync.Mutex{}
		g.connLocks[id] = l
	}
	if g.cfg.Shared == nil {
		return l
	}
	return &sharedLock{local: l, shared: g.cfg.Shared, name: "mcpconn:" + id, log: g.log}
}

// sharedLock is a connection's lock on this node, then on every node. While
// the nodes can't share it, each node holds its own.
type sharedLock struct {
	local  *sync.Mutex
	shared Shared
	name   string
	log    *slog.Logger
	unlock func()
}

func (l *sharedLock) Lock() {
	l.local.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), connLockTTL)
	defer cancel()
	unlock, err := l.shared.Lock(ctx, l.name, connLockTTL)
	if err != nil {
		l.log.Warn("mcp: taking a connection's lock on every node", "lock", l.name, "err", err)
		unlock = func() {}
	}
	l.unlock = unlock
}

func (l *sharedLock) Unlock() {
	l.unlock()
	l.local.Unlock()
}

func (g *Gateway) resourceMetadataURL() string {
	return g.resource.Scheme + "://" + g.resource.Host + "/.well-known/oauth-protected-resource" + g.resource.Path
}

// secureCookies: cookies are Secure (and __Host-) unless the issuer is
// plain http (local development and tests).
func (g *Gateway) secureCookies() bool { return g.issuer.Scheme == "https" }

func (g *Gateway) authzCookie() string {
	if g.secureCookies() {
		return "__Host-aide_mcp_authz"
	}
	return "aide_mcp_authz"
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// inProcessClient is an HTTP client whose connections reach h directly,
// in memory: the gateway's Aide clients dial their own relay.
func inProcessClient(h http.Handler) *http.Client {
	l := newPipeListener()
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 15 * time.Second}
	go srv.Serve(l)
	return &http.Client{Transport: &http.Transport{
		DialContext:       func(ctx context.Context, _, _ string) (net.Conn, error) { return l.dial(ctx) },
		ForceAttemptHTTP2: false,
	}}
}

// relayURL is the URL the in-process client dials.
const relayURL = "http://relay.internal"

type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *pipeListener) dial(ctx context.Context) (net.Conn, error) {
	a, b := net.Pipe()
	select {
	case l.conns <- b:
		return a, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "gateway" }
