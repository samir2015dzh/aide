// Package relay implements aide-relay, the owner's server: the account
// and the key that signs whoever logs in with it (account.go), admission,
// presence, tunnel routing and revocation fan-out. It never sees session
// contents (end to end, between the two ends) nor serves executable code.
//
// It serves one owner, set up with a setup code; that owner's live state
// is its tenant.
package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	relayv1 "github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1"
	"github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1/relayv1connect"
	"github.com/hermeshai/hermesh/pkg/identity"
	"github.com/hermeshai/hermesh/pkg/metrics"
	"github.com/hermeshai/hermesh/pkg/transport"
)

type Config struct {
	// DataDir holds the relay's SQLite store.
	DataDir string
	Logger  *slog.Logger
	// MCPURL is the address of this relay's own MCP gateway, with its key:
	// told to the owner's devices alone (GetMCP).
	MCPURL string
	// Pusher sends notifications to the owners' iPhones (push.go); nil
	// sends none.
	Pusher Pusher
}

type Server struct {
	cfg     Config
	log     *slog.Logger
	store   Backend
	sqlite  *Store // self-hosted only
	relayID string
	// noticeLimit bounds the notices each machine sends.
	noticeLimit *rateLimiter
	pushMem     memoryPush
	// The post office (messages.go): its store when the Backend has none,
	// its limits, and what hears of each message it takes in.
	messageMem   memoryMessages
	messageLimit *rateLimiter
	pairLimit    *rateLimiter
	// Logging in (account.go): attempts per address, and the account's run
	// of wrong passwords.
	loginLimit *rateLimiter
	logins     loginGuard
	// setupMu: one setup, account change or conversion at a time.
	setupMu     sync.Mutex
	messageHook func(ownerID, inbox string, ring *string)
	ownerHook   func(ownerID string)
	eraseHooks  []func(ownerID string)
	doorbells   Doorbells
	expiredAt   time.Time
	stop        chan struct{}
	stopOnce    sync.Once
	// The connections being served: Close ends them, and waits for them
	// (unregistering one writes to the store) before closing the store.
	serving sync.WaitGroup

	mu      sync.Mutex
	tenants map[string]*tenant
	// single is the owner of the server ("" until it is set up).
	single string
}

// tenant is one owner's live state. Peers of different owners never see or
// reach each other.
type tenant struct {
	ownerID     string
	trust       *identity.Trust
	machines    map[string]*peer
	peers       map[*peer]struct{}
	reported    map[string]*relayv1.ReportStatusRequest // name and version
	lastSeen    map[string]time.Time
	watchers    map[string]map[*updates]struct{} // each machine's WatchUpdates streams
	revocations *identityv1.RevocationList
	// ownerWatchers are woken when what WatchOwner sends may have changed.
	ownerWatchers map[chan struct{}]struct{}
	// inboxWatchers are woken when a message arrives for their inbox.
	inboxWatchers map[string]map[chan struct{}]struct{}
}

type peer struct {
	t       *tenant
	role    relayv1.Role
	subject *identity.Subject
	// signer is the root key that signed an owner device's credential: the
	// device is cut off once that key leaves the chain.
	signer string
	// deviceCert is an owner device's credential (its push registration
	// goes to machines with it).
	deviceCert *identityv1.Signed
	sess       *yamux.Session
	remote     string
	// epoch tells this connection from the subject's others on every node.
	epoch string
	since time.Time
	// inProcess: this server's own MCP gateway (InProcessHandler).
	inProcess bool
	// heard: when it was last heard from (nil in process).
	heard func() time.Time
}

func New(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	s := &Server{cfg: cfg, log: cfg.Logger, tenants: map[string]*tenant{}, stop: make(chan struct{})}
	s.noticeLimit = newRateLimiter(noticesPerMinute, time.Minute)
	s.messageLimit = newRateLimiter(messagesPerMinute, time.Minute)
	s.pairLimit = newRateLimiter(pairPerHour, time.Hour)
	s.loginLimit = newRateLimiter(loginsPerMinute, time.Minute)
	st, err := OpenStore(filepath.Join(cfg.DataDir, "relay.db"))
	if err != nil {
		return nil, err
	}
	s.store, s.sqlite = st, st
	id, err := s.store.RelayID()
	if err != nil {
		return nil, err
	}
	s.relayID = id
	if s.sqlite != nil {
		owner, err := s.sqlite.OwnerID()
		if err != nil {
			return nil, err
		}
		if owner != "" {
			if _, err := s.loadTenant(owner); err != nil {
				return nil, fmt.Errorf("stored owner: %w", err)
			}
			s.single = owner
		}
	}
	// Orders kept past their time by a relay stopped meanwhile.
	if es, ok := s.store.(EraseStore); ok {
		if err := es.PurgeErases(time.Now()); err != nil {
			s.log.Warn("purging erase orders", "err", err)
		}
	}
	s.registerMetrics()
	return s, nil
}

func (s *Server) Close() error {
	s.stopOnce.Do(func() { close(s.stop) })
	s.mu.Lock()
	for _, t := range s.tenants {
		for p := range t.peers {
			if p.sess != nil {
				p.sess.Close()
			}
		}
	}
	s.mu.Unlock()
	s.serving.Wait()
	return s.store.Close()
}

var (
	mAdmission         = metrics.Default.Histogram("hermesh_relay_admission_seconds")
	mAdmissionsOK      = metrics.Default.Counter(`hermesh_relay_admissions_total{result="ok"}`)
	mAdmissionsRefused = metrics.Default.Counter(`hermesh_relay_admissions_total{result="refused"}`)
	mTunnelOpen        = metrics.Default.Histogram("hermesh_relay_tunnel_open_seconds")
	mTunnelsTotal      = metrics.Default.Counter("hermesh_relay_tunnels_total")
	mTunnelsActive     = metrics.Default.Gauge("hermesh_relay_tunnels_active")
	mTunnelBytes       = metrics.Default.Counter("hermesh_relay_tunnel_bytes_total")
)

func (s *Server) registerMetrics() {
	for _, role := range []relayv1.Role{relayv1.Role_ROLE_MACHINE, relayv1.Role_ROLE_CLIENT, relayv1.Role_ROLE_OWNER} {
		role := role
		name := strings.ToLower(strings.TrimPrefix(role.String(), "ROLE_"))
		metrics.Default.GaugeFunc(`hermesh_relay_peers{role="`+name+`"}`, func() int64 {
			s.mu.Lock()
			defer s.mu.Unlock()
			n := int64(0)
			for _, t := range s.tenants {
				for p := range t.peers {
					if p.role == role {
						n++
					}
				}
			}
			return n
		})
	}
}

func (s *Server) RelayID() string { return s.relayID }

// SingleOwner is the owner of the server ("" before it is set up).
func (s *Server) SingleOwner() string { return s.singleOwner() }

func (s *Server) singleOwner() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.single
}

// loadTenant returns the owner's tenant, loading it from the backend; nil
// with no error for an owner the backend does not know.
func (s *Server) loadTenant(ownerID string) (*tenant, error) {
	s.mu.Lock()
	t := s.tenants[ownerID]
	s.mu.Unlock()
	if t != nil {
		return t, nil
	}
	if s.singleOwner() != "" && ownerID != s.singleOwner() {
		return nil, nil
	}
	rc, err := s.store.RootChain(ownerID)
	if err != nil || rc == nil {
		return nil, err
	}
	trust := identity.NewTrust(ownerID)
	if _, err := trust.AcceptRootChain(rc); err != nil {
		return nil, fmt.Errorf("stored root chain of %s: %w", ownerID, err)
	}
	rev, err := s.store.Revocations(ownerID)
	if err != nil {
		return nil, err
	}
	t = &tenant{ownerID: ownerID, trust: trust, machines: map[string]*peer{}, peers: map[*peer]struct{}{},
		reported: map[string]*relayv1.ReportStatusRequest{}, lastSeen: map[string]time.Time{},
		watchers: map[string]map[*updates]struct{}{}, revocations: rev, ownerWatchers: map[chan struct{}]struct{}{}}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur := s.tenants[ownerID]; cur != nil {
		return cur, nil
	}
	s.tenants[ownerID] = t
	return t, nil
}

// tenantFor returns the tenant of an owner known to this relay.
func (s *Server) tenantFor(ownerID string) (*tenant, error) {
	t, err := s.loadTenant(ownerID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("unknown owner %q", ownerID)
	}
	return t, nil
}

// acceptRootChain installs a longer root chain for t and tells its machines.
func (s *Server) acceptRootChain(t *tenant, c *identityv1.RootChain) error {
	defer s.wakeOwner(t)
	changed, err := t.trust.AcceptRootChain(c)
	if err != nil || !changed {
		return err
	}
	if err := s.store.SetRootChain(t.ownerID, t.trust.RootChain()); err != nil {
		return err
	}
	s.chainChanged(t)
	return nil
}

// chainChanged tells the owner's machines of a longer root chain, and
// disconnects the trusted devices it removed.
func (s *Server) chainChanged(t *tenant) {
	s.broadcastMachineUpdate(t, &relayv1.MachineUpdate{Update: &relayv1.MachineUpdate_RootChain{RootChain: t.trust.RootChain()}})
	// A removed device gets no more notices.
	s.broadcastPushTargets(t)
	// A device removed from the chain is disconnected now, not when it
	// next connects.
	current := map[string]bool{}
	for _, rk := range t.trust.Root().GetRootKeys() {
		if pub, err := identity.ParsePublicKey(rk.PublicKey); err == nil {
			current[identity.KeyID(pub)] = true
		}
	}
	s.mu.Lock()
	var kick []*peer
	for p := range t.peers {
		if p.role == relayv1.Role_ROLE_OWNER && !current[p.signer] {
			kick = append(kick, p)
		}
	}
	s.mu.Unlock()
	for _, p := range kick {
		s.log.Info("disconnecting a removed device", "owner", t.ownerID)
		p.sess.Close()
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.Mount(mux)
	return mux
}

type inProcessKey struct{}

// InProcessHandler is Handler for a client in this process, the server's
// own MCP gateway.
func (s *Server) InProcessHandler() http.Handler {
	h := s.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), inProcessKey{}, true)))
	})
}

// Mount adds the relay endpoints to mux.
func (s *Server) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/connect", s.handleConnect)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "ok\n")
	})
	mux.Handle(relayv1connect.NewRelayPublicServiceHandler(&publicService{s: s}))
}

// ---------------------------------------------------------------------------
// Admission.

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	select {
	case <-s.stop:
		http.Error(w, "the relay is closing", http.StatusServiceUnavailable)
		return
	default:
	}
	s.serving.Add(1)
	defer s.serving.Done()
	inProcess := r.Context().Value(inProcessKey{}) != nil
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"hermesh.v1"}})
	if err != nil {
		return
	}
	conn := transport.NetConnFromWS(ws)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	nonce := make([]byte, 32)
	rand.Read(nonce)
	if err := transport.WriteFrame(conn, &relayv1.Challenge{Nonce: nonce, RelayId: s.relayID, ServerTime: timestamppb.Now()}); err != nil {
		return
	}
	var hello relayv1.Hello
	if err := transport.ReadFrame(conn, &hello); err != nil {
		return
	}
	admitStart := time.Now()
	p, err := s.admit(&hello, nonce)
	mAdmission.Since(admitStart)
	if err != nil {
		mAdmissionsRefused.Inc()
		s.log.Info("admission refused", "remote", r.RemoteAddr, "role", hello.Role, "err", err)
		transport.WriteFrame(conn, &relayv1.HelloResult{Ok: false, Error: err.Error()})
		return
	}
	if err := transport.WriteFrame(conn, &relayv1.HelloResult{Ok: true}); err != nil {
		return
	}
	mAdmissionsOK.Inc()
	conn.SetDeadline(time.Time{})
	cfg := transport.YamuxConfig()
	cfg.EnableKeepAlive = false // keepAlive, which decides by how long the peer was quiet
	heard := &heardConn{Conn: conn}
	heard.at.Store(time.Now().UnixNano())
	sess, err := yamux.Server(heard, cfg)
	if err != nil {
		return
	}
	quiet := peerQuiet
	if p.role == relayv1.Role_ROLE_OWNER {
		quiet = ownerQuiet
	}
	go keepAlive(sess, heard.last, cfg.KeepAliveInterval, quiet)
	p.sess, p.remote, p.heard = sess, r.RemoteAddr, heard.last
	p.epoch, p.since, p.inProcess = identity.RandomCode(16), time.Now(), inProcess
	s.register(p)
	defer s.unregister(p)
	if p.role == relayv1.Role_ROLE_OWNER {
		s.mu.Lock()
		hook := s.ownerHook
		s.mu.Unlock()
		if hook != nil {
			go hook(p.t.ownerID)
		}
	}
	for {
		st, err := sess.AcceptStream()
		if err != nil {
			return
		}
		go s.handleStream(p, st)
	}
}

// ownerQuiet is how long an owner device may say nothing before its
// connection is closed. The iPhone app answers no ping while it is
// suspended in the background, and with its connection kept for a few
// minutes it comes back in a ping rather than a dial (internal/owner
// connectOrKeep).
const ownerQuiet = 5 * time.Minute

// peerQuiet is the same for machines and agents: whether a machine is
// online is shown, and what is sent to one that is gone waits for nothing.
// A machine that lost its network or fell asleep shows as offline this long
// after it was last heard from (yamux's keepalive took 30–45 s).
const peerQuiet = 20 * time.Second

// heardConn notes when its peer was last heard from.
type heardConn struct {
	net.Conn
	at atomic.Int64 // UnixNano
}

func (c *heardConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.at.Store(time.Now().UnixNano())
	}
	return n, err
}

func (c *heardConn) last() time.Time { return time.Unix(0, c.at.Load()) }

// keepAlive pings sess every interval, and closes it once a ping is not
// answered and the peer was not heard from at all (heard: when it last
// was) for quiet. Anything it sends counts: a peer on a busy link answers
// late, but is there.
func keepAlive(sess *yamux.Session, heard func() time.Time, every, quiet time.Duration) {
	for {
		select {
		case <-sess.CloseChan():
			return
		case <-time.After(every):
		}
		sent := time.Now()
		answered := make(chan struct{})
		go func() {
			if _, err := sess.Ping(); err == nil {
				close(answered)
			}
		}()
		for !heard().After(sent) {
			left := time.Until(heard().Add(quiet))
			if left <= 0 {
				sess.Close()
				return
			}
			select {
			case <-sess.CloseChan():
				return
			case <-answered:
			case <-time.After(min(left, time.Second)):
				continue
			}
			break
		}
	}
}

func (s *Server) admit(h *relayv1.Hello, nonce []byte) (*peer, error) {
	ownerID := identity.ChainOwner(h.GetCredentials().GetRootChain())
	if ownerID == "" {
		return nil, errors.New("missing root chain")
	}
	// An owner that erased everything moments ago: its devices ask for its
	// order (docs/erase-everything.md). Only for an owner not served here.
	errErased := errors.New("this relay no longer serves this device's owner (the owner erased everything)")
	erased := func() bool { order, _, _ := s.eraseOrder(ownerID); return order != nil }
	if s.sqlite != nil && s.singleOwner() != ownerID && erased() {
		return nil, errErased
	}
	if s.sqlite != nil && s.singleOwner() == "" {
		return nil, errors.New("this server is not set up yet")
	}
	t, err := s.loadTenant(ownerID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		if s.sqlite == nil {
			if erased() {
				return nil, errErased
			}
			return nil, errors.New("this center no longer serves this device's owner (the owner identity was reset); pair this device again")
		}
		return nil, errors.New("this relay does not serve that owner")
	}
	before := t.trust.RootVersion()
	sub, err := t.trust.VerifyCredentials(h.Credentials, time.Now())
	superseded := false
	if err != nil {
		// A machine or agent whose credential was replaced (owner recovery or a
		// lowered grant) is admitted with the newer stored credential for the
		// same key; it then fetches that credential.
		var ferr error
		if sub, ferr = s.supersedingCredential(t, h); ferr != nil {
			return nil, err
		}
		superseded = true
	}
	if t.trust.RootVersion() != before {
		s.store.SetRootChain(t.ownerID, t.trust.RootChain())
		s.broadcastMachineUpdate(t, &relayv1.MachineUpdate{Update: &relayv1.MachineUpdate_RootChain{RootChain: t.trust.RootChain()}})
	}
	want := map[relayv1.Role]identity.SubjectKind{
		relayv1.Role_ROLE_MACHINE: identity.SubjectMachine,
		relayv1.Role_ROLE_CLIENT:  identity.SubjectClient,
		relayv1.Role_ROLE_OWNER:   identity.SubjectOwnerDevice,
	}
	if k, ok := want[h.Role]; !ok || k != sub.Kind {
		return nil, errors.New("credentials do not match role")
	}
	if err := identity.VerifyRaw(sub.PublicKey, transport.AdmitMessage(s.relayID, nonce, h.Role), h.Proof); err != nil {
		return nil, errors.New("bad admission proof")
	}
	if err := s.checkNotRevoked(t, sub); err != nil {
		// A lowered grant: admit with the newer version for the same key.
		alt, ferr := s.supersedingCredential(t, h)
		if superseded || ferr != nil || s.checkNotRevoked(t, alt) != nil {
			return nil, err
		}
		sub, superseded = alt, true
	}
	if sub.Kind == identity.SubjectMachine {
		known, err := s.store.MachineCerts(t.ownerID)
		if err != nil {
			return nil, err
		}
		if _, ok := known[sub.ID]; !ok {
			// Machines become known by logging in (or the owner publishing
			// a cert); a removed machine's cert is gone. (A machine reads this
			// text, and the other refusals that mean it is no longer its
			// owner's: internal/machine gone.)
			return nil, errors.New("unknown or removed machine")
		}
		if !superseded {
			s.store.PutMachineCert(t.ownerID, sub.ID, h.Credentials.GetMachineCert())
		}
	}
	if sub.Kind == identity.SubjectClient && !superseded {
		s.store.PutGrant(t.ownerID, sub.Grant, h.Credentials.GetGrant())
	}
	return &peer{t: t, role: h.Role, subject: sub, signer: h.Credentials.GetDeviceCert().GetSignerKeyId(), deviceCert: h.Credentials.GetDeviceCert()}, nil
}

func (s *Server) supersedingCredential(t *tenant, h *relayv1.Hello) (*identity.Subject, error) {
	switch c := h.GetCredentials().GetSubject().(type) {
	case *identityv1.Credentials_MachineCert:
		old := &identityv1.MachineCert{}
		if err := identity.Unverified(c.MachineCert, old); err != nil {
			return nil, err
		}
		certs, err := s.store.MachineCerts(t.ownerID)
		if err != nil {
			return nil, err
		}
		stored := certs[identity.MachineID(old.MachinePublicKey)]
		if stored == nil {
			return nil, errors.New("no stored machine cert")
		}
		nc, pub, err := t.trust.VerifyMachineCert(stored)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(nc.MachinePublicKey, old.MachinePublicKey) {
			return nil, errors.New("key mismatch")
		}
		return &identity.Subject{Kind: identity.SubjectMachine, ID: nc.MachineId, PublicKey: pub, Machine: nc}, nil
	case *identityv1.Credentials_Grant:
		old := &identityv1.Grant{}
		if err := identity.Unverified(c.Grant, old); err != nil {
			return nil, err
		}
		stored, err := s.store.GrantForClient(t.ownerID, identity.ClientID(old.ClientPublicKey))
		if err != nil || stored == nil {
			return nil, errors.New("no stored grant")
		}
		g, pub, err := t.trust.VerifyGrant(stored, time.Now())
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(g.ClientPublicKey, old.ClientPublicKey) {
			return nil, errors.New("key mismatch")
		}
		return &identity.Subject{Kind: identity.SubjectClient, ID: g.ClientId, PublicKey: pub, Grant: g}, nil
	}
	return nil, errors.New("no superseding credential")
}

func (s *Server) checkNotRevoked(t *tenant, sub *identity.Subject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return isRevoked(t.revocations, sub)
}

// isRevoked reports why sub is no longer allowed, if it is not.
func isRevoked(r *identityv1.RevocationList, sub *identity.Subject) error {
	switch sub.Kind {
	case identity.SubjectMachine:
		for _, id := range r.RevokedMachineIds {
			if id == sub.ID {
				return errors.New("machine removed")
			}
		}
	case identity.SubjectClient:
		for _, id := range r.RevokedGrantIds {
			if id == sub.Grant.GrantId {
				return errors.New("grant revoked")
			}
		}
		if min := r.MinGrantVersions[sub.Grant.GrantId]; sub.Grant.Version < min {
			return errors.New("grant superseded")
		}
	case identity.SubjectOwnerDevice:
		if sub.PublicKey != nil && slices.Contains(r.RevokedDeviceKeys, identity.KeyID(sub.PublicKey)) {
			return errors.New("this device was signed out")
		}
	}
	return nil
}

func (s *Server) register(p *peer) {
	t := p.t
	s.mu.Lock()
	t.peers[p] = struct{}{}
	var old *peer
	if p.role == relayv1.Role_ROLE_MACHINE {
		old = t.machines[p.subject.ID]
		t.machines[p.subject.ID] = p
		t.lastSeen[p.subject.ID] = time.Now()
	}
	s.wakeOwnerLocked(t)
	s.mu.Unlock()
	if old != nil {
		old.sess.Close()
	}
	s.store.LogConnection(t.ownerID, int(p.role), p.subject.ID, p.remote, "connect")
	s.log.Info("peer connected", "owner", t.ownerID, "role", p.role, "subject", p.subject.ID, "remote", p.remote)
}

func (s *Server) unregister(p *peer) {
	t := p.t
	s.mu.Lock()
	delete(t.peers, p)
	if p.role == relayv1.Role_ROLE_MACHINE && t.machines[p.subject.ID] == p {
		delete(t.machines, p.subject.ID)
		// When it was last heard from, not when that was found to be over
		// (peerQuiet later, for one that went silent): owners count how
		// long it has been gone from it (docs/session-states.md §3).
		seen := time.Now()
		if p.heard != nil {
			if h := p.heard(); h.Before(seen) && h.After(p.since) {
				seen = h
			}
		}
		t.lastSeen[p.subject.ID] = seen
	}
	s.wakeOwnerLocked(t)
	s.mu.Unlock()
	p.sess.Close()
	s.store.LogConnection(t.ownerID, int(p.role), p.subject.ID, p.remote, "disconnect")
	s.log.Info("peer disconnected", "owner", t.ownerID, "role", p.role, "subject", p.subject.ID)
}

// ---------------------------------------------------------------------------
// Streams.

type peerKey struct{}

func peerFrom(ctx context.Context) *peer { p, _ := ctx.Value(peerKey{}).(*peer); return p }

func (s *Server) handleStream(p *peer, st net.Conn) {
	st.SetReadDeadline(time.Now().Add(10 * time.Second))
	var h relayv1.StreamHeader
	if err := transport.ReadFrame(st, &h); err != nil {
		st.Close()
		return
	}
	st.SetReadDeadline(time.Time{})
	switch h.Kind {
	case relayv1.StreamKind_STREAM_KIND_CONTROL:
		defer st.Close()
		ctx := context.WithValue(context.Background(), peerKey{}, p)
		transport.ServeH2(ctx, st, s.controlMux(p.role))
	case relayv1.StreamKind_STREAM_KIND_TUNNEL:
		s.handleTunnel(p, st, h.MachineId)
	default:
		st.Close()
	}
}

func (s *Server) controlMux(role relayv1.Role) http.Handler {
	mux := http.NewServeMux()
	switch role {
	case relayv1.Role_ROLE_MACHINE:
		mux.Handle(relayv1connect.NewMachineControlServiceHandler(&machineControl{s: s}, transport.NoCompression))
		mux.Handle(relayv1connect.NewMessageServiceHandler(&messageService{s: s}, transport.NoCompression))
	case relayv1.Role_ROLE_CLIENT:
		mux.Handle(relayv1connect.NewClientControlServiceHandler(&clientControl{s: s}, transport.NoCompression))
		mux.Handle(relayv1connect.NewMessageServiceHandler(&messageService{s: s}, transport.NoCompression))
	case relayv1.Role_ROLE_OWNER:
		path, h := relayv1connect.NewOwnerControlServiceHandler(&ownerControl{s: s}, transport.NoCompression)
		mux.Handle(path, countOwnerRPCs(h))
	}
	return mux
}

// mOwnerRPCs counts the owner devices' control calls by method: what an
// owner's app costs the relay (docs/state-sync.md §8). Nothing per owner.
var mOwnerRPCs = func() map[string]*metrics.Counter {
	m := map[string]*metrics.Counter{}
	methods := relayv1.File_hermesh_relay_v1_relay_proto.Services().ByName("OwnerControlService").Methods()
	for i := range methods.Len() {
		name := string(methods.Get(i).Name())
		m["/"+relayv1connect.OwnerControlServiceName+"/"+name] = metrics.Default.Counter(`hermesh_relay_owner_rpcs_total{method="` + name + `"}`)
	}
	return m
}()

func countOwnerRPCs(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c := mOwnerRPCs[r.URL.Path]; c != nil {
			c.Inc()
		}
		h.ServeHTTP(w, r)
	})
}

func (s *Server) handleTunnel(p *peer, st net.Conn, machineID string) {
	defer st.Close()
	if p.role != relayv1.Role_ROLE_CLIENT && p.role != relayv1.Role_ROLE_OWNER {
		return
	}
	if p.role == relayv1.Role_ROLE_CLIENT {
		if !identity.Allows(p.subject.Grant.MachineIds, machineID) || s.checkNotRevoked(p.t, p.subject) != nil {
			return
		}
	}
	s.mu.Lock()
	machine := p.t.machines[machineID] // only this owner's machines
	s.mu.Unlock()
	if machine == nil {
		return
	}
	s.spliceToMachine(st, machine, machineID, p.role, p.subject.ID)
}

// spliceToMachine opens a tunnel stream on a machine's connection for a
// principal and splices st with it.
func (s *Server) spliceToMachine(st net.Conn, machine *peer, machineID string, role relayv1.Role, subjectID string) {
	openStart := time.Now()
	ns, err := machine.sess.OpenStream()
	if err != nil {
		return
	}
	mTunnelOpen.Since(openStart)
	mTunnelsTotal.Inc()
	mTunnelsActive.Add(1)
	defer mTunnelsActive.Add(-1)
	defer ns.Close()
	tid := identity.RandomCode(12)
	hdr := &relayv1.StreamHeader{Kind: relayv1.StreamKind_STREAM_KIND_TUNNEL, MachineId: machineID,
		PrincipalRole: role, SubjectId: subjectID, TunnelId: tid, TraceId: tid}
	if err := transport.WriteFrame(ns, hdr); err != nil {
		return
	}
	splice(st, ns)
}

func splice(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		n, _ := io.Copy(dst, src)
		mTunnelBytes.Add(n)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		} else {
			dst.Close()
		}
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	a.Close()
	b.Close()
	<-done
}

// ---------------------------------------------------------------------------
// Revocation fan-out.

// ApplyRevocations merges in into the owner's revocations, disconnects
// whatever they revoke and pushes them to the owner's machines. Revocations only
// ever reduce permissions, so the embedding server may call this directly.
func (s *Server) ApplyRevocations(ownerID string, in *identityv1.RevocationList) error {
	t, err := s.tenantFor(ownerID)
	if err != nil {
		return err
	}
	return s.applyRevocations(t, in)
}

func (s *Server) applyRevocations(t *tenant, in *identityv1.RevocationList) error {
	defer s.wakeOwner(t)
	in = proto.Clone(in).(*identityv1.RevocationList)
	in.OwnerId = t.ownerID
	grants, err := s.store.Grants(t.ownerID)
	if err != nil {
		return err
	}
	merge := func(cur *identityv1.RevocationList) {
		MergeRevocations(cur, in)
		// An agent whose machines are all removed can reach nothing: revoke
		// it too. Revoking only lowers permissions, so it needs no root
		// signature.
		removed := setOf(cur.RevokedMachineIds)
		var stranded []string
		for _, sg := range grants {
			g := &identityv1.Grant{}
			if identity.Unverified(sg, g) == nil && len(g.MachineIds) > 0 && len(liveMachines(g.MachineIds, removed)) == 0 {
				stranded = append(stranded, g.GrantId)
			}
		}
		MergeRevocations(cur, &identityv1.RevocationList{RevokedGrantIds: stranded})
	}
	s.mu.Lock()
	cur := proto.Clone(t.revocations).(*identityv1.RevocationList)
	s.mu.Unlock()
	merge(cur)
	if err := s.store.SetRevocations(t.ownerID, cur); err != nil {
		return err
	}
	for _, id := range in.RevokedMachineIds {
		s.store.DeleteMachineCert(t.ownerID, id)
	}
	// Revoked agents leave the list. What refuses them is the revocation
	// list, kept apart and never pruned, here and on every machine.
	revoked := setOf(cur.RevokedGrantIds)
	var gone []string
	for _, sg := range grants {
		g := &identityv1.Grant{}
		if identity.Unverified(sg, g) == nil && revoked[g.GrantId] {
			if err := s.store.DeleteGrant(t.ownerID, g.GrantId); err != nil {
				s.log.Warn("forgetting a revoked grant", "owner", t.ownerID, "grant", g.GrantId, "err", err)
			}
			gone = append(gone, g.ClientId)
		}
	}
	s.dropInboxes(t, gone, in.RevokedMachineIds)
	s.mu.Lock()
	t.revocations = cur
	var kick []*peer
	for p := range t.peers {
		if err := isRevoked(cur, p.subject); err != nil {
			kick = append(kick, p)
		}
	}
	s.mu.Unlock()
	for _, p := range kick {
		s.log.Info("disconnecting revoked peer", "owner", t.ownerID, "subject", p.subject.ID)
		p.sess.Close()
	}
	s.broadcastMachineUpdate(t, &relayv1.MachineUpdate{Update: &relayv1.MachineUpdate_Revocations{Revocations: cur}})
	s.pushGrants(t)
	return nil
}

func setOf(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

// liveMachines is scope without removed machines ("*" stays).
func liveMachines(scope []string, removed map[string]bool) []string {
	out := []string{}
	for _, id := range scope {
		if !removed[id] {
			out = append(out, id)
		}
	}
	return out
}

// MergeRevocations folds in into cur (union of ids, max of versions).
func MergeRevocations(cur, in *identityv1.RevocationList) {
	add := func(dst *[]string, ids []string) {
		seen := map[string]bool{}
		for _, id := range *dst {
			seen[id] = true
		}
		for _, id := range ids {
			if !seen[id] {
				*dst = append(*dst, id)
				seen[id] = true
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
	if cur.OwnerId == "" {
		cur.OwnerId = in.OwnerId
	}
}

func (s *Server) broadcastMachineUpdate(t *tenant, u *relayv1.MachineUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, set := range t.watchers {
		for q := range set {
			q.put(u)
		}
	}
}

// MachineEntries lists an owner's machines with presence, name and version.
func (s *Server) MachineEntries(ownerID string) ([]*relayv1.MachineEntry, error) {
	t, err := s.tenantFor(ownerID)
	if err != nil {
		return nil, err
	}
	return s.machineEntries(t, nil, false)
}

func (s *Server) machineEntries(t *tenant, filter func(string) bool, onlineOnly bool) ([]*relayv1.MachineEntry, error) {
	certs, err := s.store.MachineCerts(t.ownerID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*relayv1.MachineEntry
	for id, c := range certs {
		if filter != nil && !filter(id) {
			continue
		}
		_, online := t.machines[id]
		if onlineOnly && !online {
			continue
		}
		r := t.reported[id]
		e := &relayv1.MachineEntry{MachineCert: c, Online: online, Name: r.GetName(), Version: r.GetVersion()}
		if ls, ok := t.lastSeen[id]; ok {
			e.LastSeen = timestamppb.New(ls)
		}
		out = append(out, e)
	}
	return out, nil
}

// ClientEntries lists an owner's agents that are not revoked (latest grant,
// presence, machines that still exist).
func (s *Server) ClientEntries(ownerID string) ([]*relayv1.ClientEntry, error) {
	t, err := s.tenantFor(ownerID)
	if err != nil {
		return nil, err
	}
	return s.clientEntries(t)
}

func (s *Server) clientEntries(t *tenant) ([]*relayv1.ClientEntry, error) {
	grants, err := s.store.Grants(t.ownerID)
	if err != nil {
		return nil, err
	}
	online := map[string]bool{}
	s.mu.Lock()
	for p := range t.peers {
		if p.role == relayv1.Role_ROLE_CLIENT {
			online[p.subject.ID] = true
		}
	}
	rev := t.revocations
	s.mu.Unlock()
	removed := setOf(rev.RevokedMachineIds)
	cards, _ := s.messageStore().Cards(t.ownerID)
	var out []*relayv1.ClientEntry
	for _, sg := range grants {
		g := &identityv1.Grant{}
		if err := identity.Unverified(sg, g); err != nil {
			continue
		}
		// Rows revoked before revocations forgot them are not listed either.
		if isRevoked(rev, &identity.Subject{Kind: identity.SubjectClient, ID: g.ClientId, Grant: g}) != nil {
			continue
		}
		out = append(out, &relayv1.ClientEntry{Grant: sg, Online: online[g.ClientId], MachineIds: liveMachines(g.MachineIds, removed),
			Card: cards[g.ClientId].Card, CardDeviceCert: cards[g.ClientId].DeviceCert})
	}
	return out, nil
}

// machineGrants is the grants that cover machineID, unrevoked, for that
// machine to show who may start sessions on it; nil when they can't be read.
func (s *Server) machineGrants(t *tenant, machineID string) *relayv1.MachineGrants {
	grants, err := s.store.Grants(t.ownerID)
	if err != nil {
		s.log.Warn("listing grants for a machine", "owner", t.ownerID, "machine", machineID, "err", err)
		return nil
	}
	s.mu.Lock()
	rev := t.revocations
	s.mu.Unlock()
	out := &relayv1.MachineGrants{}
	for _, sg := range grants {
		g := &identityv1.Grant{}
		if identity.Unverified(sg, g) != nil || !identity.Allows(g.MachineIds, machineID) {
			continue
		}
		if isRevoked(rev, &identity.Subject{Kind: identity.SubjectClient, ID: g.ClientId, Grant: g}) != nil {
			continue
		}
		out.Grants = append(out.Grants, sg)
	}
	return out
}

// pushGrants sends each of the owner's connected machines the grants that
// cover it, after one was published or revoked.
func (s *Server) pushGrants(t *tenant) {
	s.mu.Lock()
	var ids []string
	for id, set := range t.watchers {
		if len(set) > 0 {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		g := s.machineGrants(t, id)
		if g == nil {
			continue
		}
		u := &relayv1.MachineUpdate{Update: &relayv1.MachineUpdate_Grants{Grants: g}}
		s.mu.Lock()
		for q := range t.watchers[id] {
			q.put(u)
		}
		s.mu.Unlock()
	}
}

// eraseOrder is an erased owner's order, root chain and acks, while the
// relay keeps them.
func (s *Server) eraseOrder(ownerID string) (*identityv1.Signed, *identityv1.RootChain, []string) {
	es, ok := s.store.(EraseStore)
	if !ok {
		return nil, nil, nil
	}
	order, chain, acks, err := es.EraseOrder(ownerID, time.Now())
	if err != nil {
		s.log.Warn("reading an erase order", "owner", ownerID, "err", err)
		return nil, nil, nil
	}
	return order, chain, acks
}

// AddEraseHook tells f each owner that erased everything, once the relay
// forgot it: the MCP gateway ends its connections.
func (s *Server) AddEraseHook(f func(ownerID string)) {
	s.mu.Lock()
	s.eraseHooks = append(s.eraseHooks, f)
	s.mu.Unlock()
}

// erased forgets an owner that erased everything. Its connections close a
// moment later, so that the device that asked hears the answer first; a
// server is then as it was before it was set up. Its order goes after
// EraseRetention.
func (s *Server) erased(ownerID string) {
	s.mu.Lock()
	if s.single == ownerID {
		s.single = ""
	}
	hooks := slices.Clone(s.eraseHooks)
	// Its agents are told at their next command, by the refusal to admit
	// them: one that keeps a connection between commands must not go on
	// being answered on it meanwhile. (The owner's devices and machines
	// keep theirs a moment longer, to hear of the order.)
	var agents []*peer
	if t := s.tenants[ownerID]; t != nil {
		for p := range t.peers {
			if p.role == relayv1.Role_ROLE_CLIENT {
				agents = append(agents, p)
			}
		}
	}
	s.mu.Unlock()
	for _, p := range agents {
		p.sess.Close()
	}
	s.log.Info("owner erased everything", "owner", ownerID)
	time.AfterFunc(500*time.Millisecond, func() {
		s.forget(ownerID)
		for _, f := range hooks {
			f(ownerID)
		}
		if code, err := s.SetupCode(); err == nil && code != "" {
			fmt.Fprintf(os.Stderr, "\n  This server is not set up any more: its owner erased everything.\n  Setup code: %s\n\n", code)
		}
	})
	// The order goes once the erase had its time: devices that connect later
	// are refused, as for an owner that is gone.
	if es, ok := s.store.(EraseStore); ok {
		time.AfterFunc(EraseRetention+time.Second, func() {
			if err := es.PurgeErases(time.Now()); err != nil {
				s.log.Warn("purging erase orders", "err", err)
			}
		})
	}
}

// SetOwnerHook tells f the owner each time one of its trusted devices
// connects: the MCP gateway sends it the approvals still waiting.
func (s *Server) SetOwnerHook(f func(ownerID string)) {
	s.mu.Lock()
	s.ownerHook = f
	s.mu.Unlock()
}

// OwnerReachable reports whether a request to connect would reach the owner
// now: one of its devices is connected, or one is registered for push.
func (s *Server) OwnerReachable(ownerID string) bool {
	if s.OwnerOnline(ownerID) {
		return true
	}
	if !s.pushing() {
		return false
	}
	t, err := s.loadTenant(ownerID)
	if err != nil || t == nil {
		return false
	}
	for _, pt := range s.pushTargets(t) {
		reg := &identityv1.PushRegistration{}
		identity.Unverified(pt.Registration, reg) // verified by pushTargets
		if reg.Service == "" {                    // APNs, the push service this relay sends through
			return true
		}
	}
	return false
}

// OwnerOnline reports whether one of the owner's devices is connected.
func (s *Server) OwnerOnline(ownerID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tenants[ownerID]
	if t == nil {
		return false
	}
	for p := range t.peers {
		if p.role == relayv1.Role_ROLE_OWNER {
			return true
		}
	}
	return false
}

// forget drops an owner: its devices are disconnected.
func (s *Server) forget(ownerID string) {
	s.mu.Lock()
	t := s.tenants[ownerID]
	delete(s.tenants, ownerID)
	var peers []*peer
	if t != nil {
		for p := range t.peers {
			peers = append(peers, p)
		}
	}
	s.mu.Unlock()
	for _, p := range peers {
		p.sess.Close()
	}
	if t != nil {
		s.log.Info("owner forgotten", "owner", ownerID, "disconnected", len(peers))
	}
}

// ---------------------------------------------------------------------------

type rateLimiter struct {
	mu     sync.Mutex
	n      int
	window time.Duration
	hits   map[string][]time.Time
}

func newRateLimiter(n int, window time.Duration) *rateLimiter {
	return &rateLimiter{n: n, window: window, hits: map[string][]time.Time{}}
}

func (r *rateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	h := r.hits[key][:0]
	for _, t := range r.hits[key] {
		if now.Sub(t) < r.window {
			h = append(h, t)
		}
	}
	if len(h) >= r.n {
		r.hits[key] = h
		return false
	}
	r.hits[key] = append(h, now)
	return true
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// RawTunnel opens a tunnel stream to a connected machine with an arbitrary
// header, bypassing every relay check. It exists so red-team tests can act
// as a compromised relay; a real compromised relay can do the same.
func (s *Server) RawTunnel(machineID string, hdr *relayv1.StreamHeader) (net.Conn, error) {
	s.mu.Lock()
	var n *peer
	for _, t := range s.tenants {
		if p := t.machines[machineID]; p != nil {
			n = p
		}
	}
	s.mu.Unlock()
	if n == nil {
		return nil, errors.New("machine not connected")
	}
	st, err := n.sess.OpenStream()
	if err != nil {
		return nil, err
	}
	if err := transport.WriteFrame(st, hdr); err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}

// Store exposes the relay database of a self-hosted relay (red-team tests
// tamper with it the way an attacker who owns the relay host could).
func (s *Server) Store() *Store { return s.sqlite }

// Backend exposes the relay's storage.
func (s *Server) Backend() Backend { return s.store }
