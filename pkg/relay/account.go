package relay

import (
	"bytes"
	"cmp"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"github.com/hermeshai/hermesh/internal/legacy"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"golang.org/x/crypto/argon2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/hermeshai/hermesh/gen/go/hermesh/identity/v1"
	relayv1 "github.com/hermeshai/hermesh/gen/go/hermesh/relay/v1"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// The account (docs/accounts.md): the server keeps the owner's account and
// password itself (an Argon2id hash of it) and a key of its own, listed in
// the owner's root cert, with which it signs whoever logs in: a device, a
// machine or an agent. Nobody else approves, and nothing is compared. A
// device that logged in is the owner, so the server also signs for it
// (Sign) what adding a machine or an agent, or managing one, takes.
//
// The cost, which the owner chose: whoever holds this server's data holds
// the key that admits anyone, and can guess the password offline at one
// Argon2id per try; whoever runs the server can do anything the owner can.

const (
	// loginsPerMinute bounds login calls per address.
	loginsPerMinute = 20
	// After lockAfter wrong passwords in a row the account is locked for
	// lockFor, doubling with each further run, up to lockMax.
	lockAfter = 5
	lockFor   = time.Minute
	lockMax   = time.Hour
	// MinPassword is the shortest password taken.
	MinPassword = 8
	// Argon2id costs: above OWASP's minimum; well under a second here.
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
)

// The server's key in the root cert.
const (
	ServerKeyLabel    = "服务器"
	ServerKeyPlatform = "server"
)

// ErrWrongPassword is the answer to a wrong account or password.
var ErrWrongPassword = errors.New("wrong account or password")

// NormalizeAccount is an account name as the server compares it.
func NormalizeAccount(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// CheckNewLogin says what is wrong with a new account and password, if anything.
func CheckNewLogin(account, password string) error {
	a := NormalizeAccount(account)
	switch {
	case a == "":
		return errors.New("an account name is needed")
	case len(a) > 64 || strings.ContainsAny(a, " \t\r\n/"):
		return errors.New("the account name is at most 64 characters, without spaces or slashes")
	case utf8.RuneCountInString(password) < MinPassword:
		return fmt.Errorf("the password needs at least %d characters", MinPassword)
	case strings.EqualFold(strings.TrimSpace(password), a):
		return errors.New("the password can't be the account name")
	}
	return nil
}

// NewAccount is what the server keeps for a new account and password.
func NewAccount(account, password string) *relayv1.Account { return hashPassword(account, password, 1) }

func hashPassword(account, password string, generation uint64) *relayv1.Account {
	salt := make([]byte, 16)
	rand.Read(salt)
	a := &relayv1.Account{Account: NormalizeAccount(account), Salt: salt, Time: argonTime, MemoryKib: argonMemory, Threads: argonThreads, Generation: generation}
	a.Hash = argon2.IDKey([]byte(password), salt, a.Time, a.MemoryKib, uint8(a.Threads), 32)
	return a
}

func passwordMatches(a *relayv1.Account, password string) bool {
	if a == nil || len(a.Hash) == 0 || a.Threads == 0 || a.Threads > 255 {
		return false
	}
	h := argon2.IDKey([]byte(password), a.Salt, a.Time, a.MemoryKib, uint8(a.Threads), uint32(len(a.Hash)))
	return subtle.ConstantTimeCompare(h, a.Hash) == 1
}

// loginGuard counts an account's wrong passwords in a row.
type loginGuard struct {
	mu    sync.Mutex
	wrong int
	runs  int
	until time.Time
}

func (g *loginGuard) locked(now time.Time) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.until.Sub(now)
}

func (g *loginGuard) failed(now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.wrong++
	if g.wrong >= lockAfter {
		d := lockFor << g.runs
		if d > lockMax || d <= 0 {
			d = lockMax
		}
		g.until, g.wrong = now.Add(d), 0
		g.runs++
	}
}

func (g *loginGuard) succeeded() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.wrong, g.runs, g.until = 0, 0, time.Time{}
}

// ---------------------------------------------------------------------------
// What the store keeps.

// Login is a device, machine or agent that logged in: KeyID is a device's
// device key id, a machine's id or an agent's grant id.
type Login struct {
	KeyID      string
	Kind       relayv1.JoinKind
	Label      string
	Platform   string
	Generation uint64
	At         time.Time
}

func (s *Store) ServerKey() (*identity.SoftwareKey, error) {
	der, err := s.getMeta("server_key")
	if err != nil || der == nil {
		return nil, err
	}
	return parseKey(der)
}

func (s *Store) setServerKey(k *identity.SoftwareKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(k.Key)
	if err != nil {
		return err
	}
	return s.setMeta("server_key", der)
}

func parseKey(der []byte) (*identity.SoftwareKey, error) {
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok || ek.Curve != elliptic.P256() {
		return nil, errors.New("not a P-256 key")
	}
	return &identity.SoftwareKey{Key: ek}, nil
}

// Account is the account kept, nil before there is one.
func (s *Store) Account() (*relayv1.Account, error) {
	v, err := s.getMeta("account")
	if err != nil || len(v) == 0 {
		return nil, err
	}
	a := &relayv1.Account{}
	return a, proto.Unmarshal(v, a)
}

func (s *Store) SetAccount(a *relayv1.Account) error {
	b, err := proto.Marshal(a)
	if err != nil {
		return err
	}
	return s.setMeta("account", b)
}

// LoginsOff tells whether logging in is turned off.
func (s *Store) LoginsOff() (bool, error) {
	v, err := s.getMeta("logins_off")
	return string(v) == "1", err
}

func (s *Store) setLoginsOff(off bool) error {
	if !off {
		_, err := s.db.Exec(`DELETE FROM meta WHERE key='logins_off'`)
		return err
	}
	return s.setMeta("logins_off", []byte("1"))
}

func (s *Store) PutLogin(l Login) error {
	_, err := s.db.Exec(`INSERT INTO logins(key_id, kind, label, platform, generation, logged_in_at) VALUES(?,?,?,?,?,?)
ON CONFLICT(key_id) DO UPDATE SET kind=excluded.kind, label=excluded.label, platform=excluded.platform, generation=excluded.generation, logged_in_at=excluded.logged_in_at`,
		l.KeyID, int(l.Kind), l.Label, l.Platform, l.Generation, l.At.Unix())
	return err
}

func (s *Store) DeleteLogin(keyID string) error {
	_, err := s.db.Exec(`DELETE FROM logins WHERE key_id=?`, keyID)
	return err
}

func (s *Store) Logins() ([]Login, error) {
	rows, err := s.db.Query(`SELECT key_id, kind, label, platform, generation, logged_in_at FROM logins ORDER BY logged_in_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Login
	for rows.Next() {
		var l Login
		var kind int
		var at int64
		if err := rows.Scan(&l.KeyID, &kind, &l.Label, &l.Platform, &l.Generation, &at); err != nil {
			return nil, err
		}
		l.Kind, l.At = relayv1.JoinKind(kind), time.Unix(at, 0)
		out = append(out, l)
	}
	return out, rows.Err()
}

// oldPasswordLogin is a password login from before accounts, nil if none.
func (s *Store) oldPasswordLogin() (*relayv1.OldPasswordLogin, error) {
	v, err := s.getMeta("password_login")
	if err != nil || len(v) == 0 {
		return nil, err
	}
	l := &relayv1.OldPasswordLogin{}
	return l, proto.Unmarshal(v, l)
}

// ---------------------------------------------------------------------------
// Setting up, and logging in.

// SetupCode returns the code to set the server up with, or "" once it is set up.
func (s *Server) SetupCode() (string, error) {
	if s.singleOwner() != "" {
		return "", nil
	}
	return s.sqlite.SetupCode()
}

func (p *publicService) SetUp(ctx context.Context, req *relayv1.SetUpRequest) (*relayv1.LogInResponse, error) {
	s := p.s
	if !s.loginLimit.Allow(callerIP(ctx)) {
		return nil, errf(connect.CodeResourceExhausted, "too many attempts; wait a minute")
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.singleOwner() != "" {
		return nil, errf(connect.CodeAlreadyExists, "this server is set up already: log in with its account")
	}
	code, err := s.sqlite.SetupCode()
	if err != nil {
		return nil, err
	}
	if !constantTimeEqual(strings.ToUpper(strings.TrimSpace(req.SetupCode)), code) {
		return nil, errf(connect.CodePermissionDenied, "wrong setup code")
	}
	if err := CheckNewLogin(req.Account, req.Password); err != nil {
		return nil, errf(connect.CodeInvalidArgument, err.Error())
	}
	if err := checkJoiner(req.Joiner); err != nil {
		return nil, err
	}
	key, err := identity.GenerateKey()
	if err != nil {
		return nil, err
	}
	spki := identity.MarshalPublicKey(key.Public())
	ownerID := identity.OwnerID(spki)
	rc, err := identity.Sign(&identityv1.RootCert{OwnerId: ownerID, Version: 1, IssuedAt: timestamppb.Now(),
		RootKeys: []*identityv1.RootKey{{PublicKey: spki, Label: ServerKeyLabel, Platform: ServerKeyPlatform}}}, key)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key.Key)
	if err != nil {
		return nil, err
	}
	acct := hashPassword(req.Account, req.Password, 1)
	if err := s.sqlite.SaveSetup(ownerID, &identityv1.RootChain{Certs: []*identityv1.Signed{rc}}, der, acct); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.single = ownerID
	s.mu.Unlock()
	t, err := s.tenantFor(ownerID)
	if err != nil {
		return nil, err
	}
	s.log.Info("server set up", "owner", ownerID, "remote", callerIP(ctx))
	return s.logIn(t, key, acct, req.Joiner)
}

func (p *publicService) LogIn(ctx context.Context, req *relayv1.LogInRequest) (*relayv1.LogInResponse, error) {
	s := p.s
	owner := s.singleOwner()
	if owner == "" {
		return nil, errf(connect.CodeFailedPrecondition, "this server is not set up yet: set it up with the setup code it printed")
	}
	if !s.loginLimit.Allow(callerIP(ctx)) {
		return nil, errf(connect.CodeResourceExhausted, "too many attempts; wait a minute")
	}
	now := time.Now()
	if d := s.logins.locked(now); d > 0 {
		return nil, errf(connect.CodeResourceExhausted, fmt.Sprintf("too many wrong passwords; try again in %d minutes", int(d.Minutes())+1))
	}
	if err := checkJoiner(req.Joiner); err != nil {
		return nil, err
	}
	t, err := s.tenantFor(owner)
	if err != nil {
		return nil, err
	}
	key, acct, err := s.checkPassword(t, req.Account, req.Password)
	if err != nil {
		if errors.Is(err, ErrWrongPassword) {
			s.logins.failed(now)
			s.log.Info("login refused", "remote", callerIP(ctx))
			return nil, connect.NewError(connect.CodePermissionDenied, ErrWrongPassword)
		}
		return nil, err
	}
	s.logins.succeeded()
	if off, err := s.sqlite.LoginsOff(); err != nil || off {
		if err == nil {
			err = errf(connect.CodeFailedPrecondition, "logging in is turned off for this account; turn it on from a device that is logged in")
		}
		return nil, err
	}
	res, err := s.logIn(t, key, acct, req.Joiner)
	if err == nil {
		s.log.Info("logged in", "kind", req.Joiner.GetKind(), "remote", callerIP(ctx))
	}
	return res, err
}

// checkPassword returns the server's key and the account for the right
// account and password, converting a password login from before accounts
// with them first (convertOld).
func (s *Server) checkPassword(t *tenant, account, password string) (*identity.SoftwareKey, *relayv1.Account, error) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	acct, err := s.sqlite.Account()
	if err != nil {
		return nil, nil, err
	}
	if acct == nil {
		old, err := s.sqlite.oldPasswordLogin()
		if err != nil {
			return nil, nil, err
		}
		if old == nil {
			return nil, nil, errf(connect.CodeFailedPrecondition, "this server has no account: it was set up before accounts, without a password login (docs/accounts.md §5)")
		}
		if err := s.convertOld(t, old, account, password); err != nil {
			return nil, nil, err
		}
		if acct, err = s.sqlite.Account(); err != nil {
			return nil, nil, err
		}
	} else if acct.Account != NormalizeAccount(account) || !passwordMatches(acct, password) {
		return nil, nil, ErrWrongPassword
	}
	key, err := s.sqlite.ServerKey()
	if err != nil || key == nil {
		return nil, nil, fmt.Errorf("the server's key: %v", err)
	}
	return key, acct, nil
}

// checkJoiner says what is wrong with what asks to log in.
func checkJoiner(j *relayv1.Joiner) error {
	bad := func(msg string) error { return errf(connect.CodeInvalidArgument, msg) }
	if j == nil {
		return bad("say what logs in")
	}
	if _, err := identity.ParsePublicKey(j.PublicKey); err != nil {
		return bad("its key: " + err.Error())
	}
	if len(j.MachinePublicKey) > 0 {
		if j.Kind != relayv1.JoinKind_JOIN_KIND_DEVICE {
			return bad("only a device brings a machine of its own")
		}
		if _, err := identity.ParsePublicKey(j.MachinePublicKey); err != nil {
			return bad("its machine's key: " + err.Error())
		}
	}
	if utf8.RuneCountInString(j.Label) > 100 || utf8.RuneCountInString(j.Platform) > 40 || utf8.RuneCountInString(j.Hostname) > 255 || len(j.MachineName) > 63 {
		return bad("a name too long")
	}
	switch j.Kind {
	case relayv1.JoinKind_JOIN_KIND_DEVICE, relayv1.JoinKind_JOIN_KIND_MACHINE, relayv1.JoinKind_JOIN_KIND_AGENT:
		return nil
	}
	return bad("say what logs in: a device, a machine or an agent")
}

// logIn signs j in: its credential, by the server's key.
func (s *Server) logIn(t *tenant, key *identity.SoftwareKey, acct *relayv1.Account, j *relayv1.Joiner) (*relayv1.LogInResponse, error) {
	now := time.Now()
	label := strings.TrimSpace(j.Label)
	if label == "" {
		label = strings.TrimSpace(j.Hostname)
	}
	res := &relayv1.LogInResponse{OwnerId: t.ownerID, Account: acct.Account}
	machine := func(spki []byte, deviceKeyID string, name string) (*identityv1.Signed, error) {
		id := identity.MachineID(spki)
		res.MachineName = s.freeMachineName(t, name, id)
		s.mu.Lock()
		removed := slices.Contains(t.revocations.GetRevokedMachineIds(), id)
		s.mu.Unlock()
		if removed {
			return nil, errf(connect.CodeFailedPrecondition, "this machine was removed by its owner; erase it here (aided erase), then log it in again: it comes back under a new key")
		}
		nc, err := identity.Sign(&identityv1.MachineCert{OwnerId: t.ownerID, MachineId: id, MachinePublicKey: spki, IssuedAt: timestamppb.New(now), DeviceKeyId: deviceKeyID}, key)
		if err != nil {
			return nil, err
		}
		if err := s.store.PutMachineCert(t.ownerID, id, nc); err != nil {
			return nil, err
		}
		return nc, nil
	}
	var l Login
	switch j.Kind {
	case relayv1.JoinKind_JOIN_KIND_DEVICE:
		pub, _ := identity.ParsePublicKey(j.PublicKey)
		dc, err := identity.Sign(&identityv1.OwnerDeviceCert{OwnerId: t.ownerID, DevicePublicKey: j.PublicKey, Label: label, IssuedAt: timestamppb.New(now)}, key)
		if err != nil {
			return nil, err
		}
		res.Credential = dc
		l = Login{KeyID: identity.KeyID(pub), Kind: j.Kind, Label: label, Platform: j.Platform}
		if len(j.MachinePublicKey) > 0 {
			if res.MachineCert, err = machine(j.MachinePublicKey, l.KeyID, cmp.Or(strings.TrimSpace(j.MachineName), label)); err != nil {
				return nil, err
			}
		}
	case relayv1.JoinKind_JOIN_KIND_MACHINE:
		nc, err := machine(j.PublicKey, "", label)
		if err != nil {
			return nil, err
		}
		res.Credential = nc
		l = Login{KeyID: identity.MachineID(j.PublicKey), Kind: j.Kind, Label: label, Platform: j.Platform}
	case relayv1.JoinKind_JOIN_KIND_AGENT:
		if label == "" {
			label = "agent"
		}
		g := DefaultGrant(t.ownerID, j.PublicKey, label, now)
		sg, err := identity.Sign(g, key)
		if err != nil {
			return nil, err
		}
		if err := s.store.PutGrant(t.ownerID, g, sg); err != nil {
			return nil, err
		}
		res.Credential = sg
		l = Login{KeyID: g.GrantId, Kind: j.Kind, Label: label, Platform: j.Platform}
		s.pushGrants(t)
	}
	l.Generation, l.At = acct.Generation, now
	if err := s.sqlite.PutLogin(l); err != nil {
		return nil, err
	}
	res.RootChain = t.trust.RootChain()
	s.wakeOwner(t)
	return res, nil
}

// freeMachineName is name unless another of t's machines (than id) has it,
// else name with the first suffix (-2, -3…) none has, within a machine
// name's 63 bytes.
func (s *Server) freeMachineName(t *tenant, name, id string) string {
	taken := map[string]bool{}
	s.mu.Lock()
	for mid, r := range t.reported {
		if mid != id {
			taken[r.GetName()] = true
		}
	}
	s.mu.Unlock()
	if name == "" || !taken[name] {
		return name
	}
	for i := 2; ; i++ {
		suffix := fmt.Sprintf("-%d", i)
		base := name
		if len(base)+len(suffix) > 63 {
			base = strings.TrimRight(base[:63-len(suffix)], "-")
		}
		if !taken[base+suffix] {
			return base + suffix
		}
	}
}

// DefaultGrant is what an agent that logs in gets: every machine, project
// and coding agent, its own tasks, until revoked.
func DefaultGrant(ownerID string, clientKey []byte, label string, now time.Time) *identityv1.Grant {
	return &identityv1.Grant{OwnerId: ownerID, GrantId: "g_" + strings.ToLower(identity.RandomCode(26)), Version: 1,
		ClientId: identity.ClientID(clientKey), ClientPublicKey: clientKey, ClientLabel: label,
		MachineIds: []string{"*"}, ProjectIds: []string{"*"}, Agents: []string{"*"},
		TaskVisibility: identityv1.TaskVisibility_TASK_VISIBILITY_OWN,
		NotBefore:      timestamppb.New(now.Add(-time.Minute)), ExpiresAt: timestamppb.New(identity.NoExpiry)}
}

// IssueGrant signs in an agent whose key a server holds for it (the MCP
// gateway, docs/mcp-gateway.md): as LogIn with an agent, for the right
// account and password, with the grant's custodian and task visibility
// set. It returns the signed grant and the root chain.
func (s *Server) IssueGrant(account, password string, clientKey []byte, label, custodian string) (*identityv1.Signed, *identityv1.RootChain, error) {
	owner := s.singleOwner()
	if owner == "" {
		return nil, nil, errf(connect.CodeFailedPrecondition, "this server is not set up yet")
	}
	now := time.Now()
	if d := s.logins.locked(now); d > 0 {
		return nil, nil, errf(connect.CodeResourceExhausted, fmt.Sprintf("too many wrong passwords; try again in %d minutes", int(d.Minutes())+1))
	}
	t, err := s.tenantFor(owner)
	if err != nil {
		return nil, nil, err
	}
	key, acct, err := s.checkPassword(t, account, password)
	if err != nil {
		if errors.Is(err, ErrWrongPassword) {
			s.logins.failed(now)
			return nil, nil, connect.NewError(connect.CodePermissionDenied, ErrWrongPassword)
		}
		return nil, nil, err
	}
	s.logins.succeeded()
	if off, err := s.sqlite.LoginsOff(); err != nil || off {
		if err == nil {
			err = errf(connect.CodeFailedPrecondition, "logging in is turned off for this account")
		}
		return nil, nil, err
	}
	g := DefaultGrant(owner, clientKey, label, now)
	g.Custodian = custodian
	// An agent product reached through the gateway sees what is going on,
	// as the owner chose for those when they were approved.
	g.TaskVisibility = identityv1.TaskVisibility_TASK_VISIBILITY_ALL
	sg, err := identity.Sign(g, key)
	if err != nil {
		return nil, nil, err
	}
	if err := s.store.PutGrant(owner, g, sg); err != nil {
		return nil, nil, err
	}
	if err := s.sqlite.PutLogin(Login{KeyID: g.GrantId, Kind: relayv1.JoinKind_JOIN_KIND_AGENT, Label: label, Platform: "mcp", Generation: acct.Generation, At: now}); err != nil {
		return nil, nil, err
	}
	s.pushGrants(t)
	s.wakeOwner(t)
	return sg, t.trust.RootChain(), nil
}

// ---------------------------------------------------------------------------
// The owner's devices: signing for them, the account, the device list.

// signable is what the server signs for a device of the owner, by type.
var signable = map[string]func() proto.Message{
	identity.MessageType(&identityv1.MachineCert{}):  func() proto.Message { return &identityv1.MachineCert{} },
	identity.MessageType(&identityv1.Grant{}):        func() proto.Message { return &identityv1.Grant{} },
	identity.MessageType(&identityv1.AdminCommand{}): func() proto.Message { return &identityv1.AdminCommand{} },
	identity.MessageType(&identityv1.EraseOrder{}):   func() proto.Message { return &identityv1.EraseOrder{} },
}

type ownerOf interface{ GetOwnerId() string }

func (o *ownerControl) Sign(ctx context.Context, req *relayv1.SignRequest) (*relayv1.SignResponse, error) {
	t := peerFrom(ctx).t
	key, err := o.s.sqlite.ServerKey()
	if err != nil {
		return nil, err
	}
	if key == nil {
		return nil, errf(connect.CodeFailedPrecondition, "this server has no key of its own yet: log in once with the account and password (docs/accounts.md §5)")
	}
	if len(req.Messages) > 1000 {
		return nil, errf(connect.CodeInvalidArgument, "too many at once")
	}
	out := &relayv1.SignResponse{}
	for _, u := range req.Messages {
		mk := signable[u.Type]
		if mk == nil {
			return nil, errf(connect.CodeInvalidArgument, "the server does not sign "+u.Type)
		}
		m := mk()
		if err := proto.Unmarshal(u.Body, m); err != nil {
			return nil, errf(connect.CodeInvalidArgument, err.Error())
		}
		if m.(ownerOf).GetOwnerId() != t.ownerID {
			return nil, errf(connect.CodeInvalidArgument, "that is for another owner")
		}
		sg, err := identity.Sign(m, key)
		if err != nil {
			return nil, err
		}
		out.Signed = append(out.Signed, sg)
	}
	return out, nil
}

func (o *ownerControl) GetLogin(ctx context.Context, _ *relayv1.GetLoginRequest) (*relayv1.GetLoginResponse, error) {
	st := o.s.sqlite
	acct, err := st.Account()
	if err != nil {
		return nil, err
	}
	off, err := st.LoginsOff()
	if err != nil {
		return nil, err
	}
	res := &relayv1.GetLoginResponse{Account: acct.GetAccount(), Off: off}
	if acct == nil {
		old, err := st.oldPasswordLogin()
		if err != nil {
			return nil, err
		}
		res.Convert, res.Account = old != nil, old.GetAccount()
	}
	return res, nil
}

func (o *ownerControl) SetLogin(ctx context.Context, req *relayv1.SetLoginRequest) (*relayv1.SetLoginResponse, error) {
	s := o.s
	t := peerFrom(ctx).t
	now := time.Now()
	if d := s.logins.locked(now); d > 0 {
		return nil, errf(connect.CodeResourceExhausted, fmt.Sprintf("too many wrong passwords; try again in %d minutes", int(d.Minutes())+1))
	}
	cur, err := s.sqlite.Account()
	if err != nil {
		return nil, err
	}
	name := cur.GetAccount()
	if cur == nil {
		old, err := s.sqlite.oldPasswordLogin()
		if err != nil {
			return nil, err
		}
		name = old.GetAccount()
	}
	if _, _, err := s.checkPassword(t, name, req.CurrentPassword); err != nil {
		if errors.Is(err, ErrWrongPassword) {
			s.logins.failed(now)
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("the current password is wrong"))
		}
		return nil, err
	}
	s.logins.succeeded()
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	acct, err := s.sqlite.Account()
	if err != nil {
		return nil, err
	}
	res := &relayv1.SetLoginResponse{}
	account := acct.Account
	if req.Account != "" {
		account = NormalizeAccount(req.Account)
	}
	switch {
	case req.NewPassword != "":
		if err := CheckNewLogin(account, req.NewPassword); err != nil {
			return nil, errf(connect.CodeInvalidArgument, err.Error())
		}
		next := hashPassword(account, req.NewPassword, acct.Generation+1)
		if err := s.sqlite.SetAccount(next); err != nil {
			return nil, err
		}
		n, err := s.signOutBefore(t, next.Generation, req.DeviceKey)
		if err != nil {
			return nil, err
		}
		res.SignedOut = uint32(n)
	case account != acct.Account:
		if err := CheckNewLogin(account, strings.Repeat("x", MinPassword)); err != nil {
			return nil, errf(connect.CodeInvalidArgument, err.Error())
		}
		acct.Account = account
		if err := s.sqlite.SetAccount(acct); err != nil {
			return nil, err
		}
	}
	if err := s.sqlite.setLoginsOff(req.Off); err != nil {
		return nil, err
	}
	res.Account = account
	s.log.Info("account changed", "new_password", req.NewPassword != "", "off", req.Off)
	return res, nil
}

// signOutBefore signs out every device and agent that logged in before
// generation, except the device with key id keep, and returns how many.
// Machines stay: they run the owner's tasks, and are removed one by one.
func (s *Server) signOutBefore(t *tenant, generation uint64, keep string) (int, error) {
	logins, err := s.sqlite.Logins()
	if err != nil {
		return 0, err
	}
	r := &identityv1.RevocationList{}
	for _, l := range logins {
		if l.Generation >= generation {
			continue
		}
		switch {
		case l.Kind == relayv1.JoinKind_JOIN_KIND_DEVICE && l.KeyID != keep:
			r.RevokedDeviceKeys = append(r.RevokedDeviceKeys, l.KeyID)
		case l.Kind == relayv1.JoinKind_JOIN_KIND_AGENT:
			r.RevokedGrantIds = append(r.RevokedGrantIds, l.KeyID)
		default:
			continue
		}
		s.sqlite.DeleteLogin(l.KeyID)
	}
	if n := len(r.RevokedDeviceKeys) + len(r.RevokedGrantIds); n > 0 {
		return n, s.revokeDevices(t, r)
	}
	return 0, nil
}

// revokeDevices applies r, and also removes the own machines of the
// devices it signs out.
func (s *Server) revokeDevices(t *tenant, r *identityv1.RevocationList) error {
	if len(r.RevokedDeviceKeys) > 0 {
		certs, err := s.store.MachineCerts(t.ownerID)
		if err != nil {
			return err
		}
		for id, sg := range certs {
			nc := &identityv1.MachineCert{}
			if identity.Unverified(sg, nc) == nil && nc.DeviceKeyId != "" && slices.Contains(r.RevokedDeviceKeys, nc.DeviceKeyId) {
				r.RevokedMachineIds = append(r.RevokedMachineIds, id)
			}
		}
	}
	return s.applyRevocations(t, r)
}

func (o *ownerControl) ListDevices(ctx context.Context, _ *relayv1.ListDevicesRequest) (*relayv1.ListDevicesResponse, error) {
	return o.s.devices(peerFrom(ctx).t)
}

// devices is the owner's devices: those that logged in, and those whose
// root key the root cert lists (set up before accounts).
func (s *Server) devices(t *tenant) (*relayv1.ListDevicesResponse, error) {
	logins, err := s.sqlite.Logins()
	if err != nil {
		return nil, err
	}
	online := map[string]bool{}
	s.mu.Lock()
	revoked := setOf(t.revocations.GetRevokedDeviceKeys())
	for p := range t.peers {
		if p.role == relayv1.Role_ROLE_OWNER && p.subject != nil && p.subject.PublicKey != nil {
			online[identity.KeyID(p.subject.PublicKey)] = true
			online[p.signer] = true
		}
	}
	s.mu.Unlock()
	res := &relayv1.ListDevicesResponse{}
	for _, rk := range t.trust.Root().GetRootKeys() {
		if rk.Platform == ServerKeyPlatform || rk.Platform == "password" {
			continue
		}
		pub, err := identity.ParsePublicKey(rk.PublicKey)
		if err != nil {
			continue
		}
		id := identity.KeyID(pub)
		res.Devices = append(res.Devices, &relayv1.DeviceEntry{KeyId: id, Label: rk.Label, Platform: rk.Platform, Legacy: true, Online: online[id]})
	}
	for _, l := range logins {
		if l.Kind != relayv1.JoinKind_JOIN_KIND_DEVICE || revoked[l.KeyID] {
			continue
		}
		res.Devices = append(res.Devices, &relayv1.DeviceEntry{KeyId: l.KeyID, Label: l.Label, Platform: l.Platform,
			LoggedInAt: timestamppb.New(l.At), Online: online[l.KeyID]})
	}
	return res, nil
}

func (o *ownerControl) RemoveDevice(ctx context.Context, req *relayv1.RemoveDeviceRequest) (*relayv1.RemoveDeviceResponse, error) {
	p := peerFrom(ctx)
	if p.subject != nil && p.subject.PublicKey != nil && (identity.KeyID(p.subject.PublicKey) == req.KeyId || p.signer == req.KeyId) {
		return nil, errf(connect.CodeInvalidArgument, "sign this device out from another of your devices")
	}
	return o.s.removeDevice(p.t, req.KeyId)
}

func (s *Server) removeDevice(t *tenant, keyID string) (*relayv1.RemoveDeviceResponse, error) {
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	logins, err := s.sqlite.Logins()
	if err != nil {
		return nil, err
	}
	for _, l := range logins {
		if l.Kind == relayv1.JoinKind_JOIN_KIND_DEVICE && l.KeyID == keyID {
			if err := s.revokeDevices(t, &identityv1.RevocationList{RevokedDeviceKeys: []string{keyID}}); err != nil {
				return nil, err
			}
			s.sqlite.DeleteLogin(keyID)
			s.log.Info("device signed out", "device", keyID)
			return &relayv1.RemoveDeviceResponse{}, nil
		}
	}
	// A device from before accounts: the next root cert without its root
	// key, and what that key signed signed again by the server's.
	cur := t.trust.Root()
	var keep []*identityv1.RootKey
	found := false
	for _, rk := range cur.GetRootKeys() {
		if pub, err := identity.ParsePublicKey(rk.PublicKey); err == nil && identity.KeyID(pub) == keyID && rk.Platform != ServerKeyPlatform {
			found = true
			continue
		}
		keep = append(keep, rk)
	}
	if !found {
		return nil, errf(connect.CodeNotFound, "no such device")
	}
	key, err := s.sqlite.ServerKey()
	if err != nil || key == nil {
		return nil, errf(connect.CodeFailedPrecondition, "this server has no key of its own yet: log in once with the account and password")
	}
	res, err := s.replaceRootKeys(t, key, key, keep, keyID)
	if err != nil {
		return nil, err
	}
	// Its own machine goes with it.
	certs, err := s.store.MachineCerts(t.ownerID)
	if err != nil {
		return nil, err
	}
	var gone []string
	for id, sg := range certs {
		nc := &identityv1.MachineCert{}
		if identity.Unverified(sg, nc) == nil && nc.DeviceKeyId == keyID {
			gone = append(gone, id)
		}
	}
	if len(gone) > 0 {
		if err := s.applyRevocations(t, &identityv1.RevocationList{RevokedMachineIds: gone}); err != nil {
			return nil, err
		}
	}
	s.log.Info("device removed", "device", keyID, "reissued_machines", res.ReissuedMachines, "reissued_grants", res.ReissuedGrants)
	return res, nil
}

// replaceRootKeys makes the next root cert list keys, signed by signer (a
// key of the current one), and has the server's key sign again every
// machine cert and grant that the key with id gone had signed (a grant one
// version up), so they keep working.
func (s *Server) replaceRootKeys(t *tenant, signer, key *identity.SoftwareKey, keys []*identityv1.RootKey, gone string) (*relayv1.RemoveDeviceResponse, error) {
	cur := t.trust.Root()
	next, err := identity.Sign(&identityv1.RootCert{OwnerId: t.ownerID, RootKeys: keys, Version: cur.Version + 1, IssuedAt: timestamppb.Now()}, signer)
	if err != nil {
		return nil, err
	}
	// What the leaving key signed, before the chain without it.
	certs, err := s.store.MachineCerts(t.ownerID)
	if err != nil {
		return nil, err
	}
	grants, err := s.store.Grants(t.ownerID)
	if err != nil {
		return nil, err
	}
	chain := t.trust.RootChain()
	chain.Certs = append(chain.Certs, next)
	if err := s.acceptRootChain(t, chain); err != nil {
		return nil, err
	}
	res := &relayv1.RemoveDeviceResponse{}
	for id, sg := range certs {
		nc := &identityv1.MachineCert{}
		if sg.SignerKeyId != gone || identity.Unverified(sg, nc) != nil {
			continue
		}
		nc.IssuedAt = timestamppb.Now()
		again, err := identity.Sign(nc, key)
		if err != nil {
			return nil, err
		}
		if err := s.store.PutMachineCert(t.ownerID, id, again); err != nil {
			return nil, err
		}
		s.broadcastMachineUpdate(t, &relayv1.MachineUpdate{Update: &relayv1.MachineUpdate_MachineCert{MachineCert: again}})
		res.ReissuedMachines++
	}
	for _, sg := range grants {
		g := &identityv1.Grant{}
		if sg.SignerKeyId != gone || identity.Unverified(sg, g) != nil {
			continue
		}
		g.Version++
		again, err := identity.Sign(g, key)
		if err != nil {
			return nil, err
		}
		if err := s.store.PutGrant(t.ownerID, g, again); err != nil {
			return nil, err
		}
		res.ReissuedGrants++
	}
	if res.ReissuedGrants > 0 {
		s.pushGrants(t)
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// Converting a password login from before accounts (docs/accounts.md §5).

// convertOld checks account and password against old, the password key
// sealed by the password, opens the key with it, and has it sign the next
// root cert: without itself, with a new key of the server's own. What it
// had signed is signed again by the server's key, the account is kept as
// an Argon2id hash, and old is forgotten.
func (s *Server) convertOld(t *tenant, old *relayv1.OldPasswordLogin, account, password string) error {
	k := old.Kdf
	if old.Account != NormalizeAccount(account) || k == nil || len(k.Salt) < 16 || k.Time == 0 || k.Time > 10 ||
		k.MemoryKib < 8*1024 || k.MemoryKib > 256*1024 || k.Threads == 0 || k.Threads > 16 {
		return ErrWrongPassword
	}
	derived := argon2.IDKey([]byte(password), k.Salt, k.Time, k.MemoryKib, uint8(k.Threads), 64)
	verifier := sha256.Sum256(derived[:32])
	if subtle.ConstantTimeCompare(verifier[:], old.Verifier) != 1 {
		return ErrWrongPassword
	}
	block, err := aes.NewCipher(derived[32:])
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	aad := append([]byte(legacy.PasswordKey+t.ownerID+"\x00"), old.PublicKey...)
	der, err := gcm.Open(nil, old.Nonce, old.SealedKey, aad)
	if err != nil {
		return errors.New("the password does not open the old password key")
	}
	passwordKey, err := parseKey(der)
	if err != nil {
		return err
	}
	if !bytes.Equal(identity.MarshalPublicKey(passwordKey.Public()), old.PublicKey) {
		return errors.New("the old password key is not the one named")
	}
	pkID := identity.KeyID(passwordKey.Public())
	key, err := identity.GenerateKey()
	if err != nil {
		return err
	}
	var keys []*identityv1.RootKey
	listed := false
	for _, rk := range t.trust.Root().GetRootKeys() {
		if bytes.Equal(rk.PublicKey, old.PublicKey) {
			listed = true
			continue
		}
		keys = append(keys, rk)
	}
	if !listed {
		return errf(connect.CodeFailedPrecondition, "the old password login was turned off; this server has no account")
	}
	keys = append(keys, &identityv1.RootKey{PublicKey: identity.MarshalPublicKey(key.Public()), Label: ServerKeyLabel, Platform: ServerKeyPlatform})
	// The key first: what is signed next needs it.
	if err := s.sqlite.setServerKey(key); err != nil {
		return err
	}
	res, err := s.replaceRootKeys(t, passwordKey, key, keys, pkID)
	if err != nil {
		return err
	}
	if err := s.sqlite.SetAccount(hashPassword(account, password, 1)); err != nil {
		return err
	}
	if _, err := s.sqlite.db.Exec(`DELETE FROM meta WHERE key='password_login'`); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	s.log.Info("the old password login is now the account", "reissued_machines", res.ReissuedMachines, "reissued_grants", res.ReissuedGrants)
	return nil
}

// SetPassword sets the account's password on the server's own computer
// (aide-relay account), for an owner who forgot it: every device and agent
// that logged in is signed out, as with a new password.
func (s *Server) SetPassword(account, password string) (int, error) {
	owner := s.singleOwner()
	if owner == "" {
		return 0, errors.New("this server is not set up yet")
	}
	if err := CheckNewLogin(account, password); err != nil {
		return 0, err
	}
	t, err := s.tenantFor(owner)
	if err != nil {
		return 0, err
	}
	s.setupMu.Lock()
	acct, err := s.sqlite.Account()
	if err == nil && acct == nil {
		err = errors.New("this server has no account (it was set up before accounts, without a password login)")
	}
	if err != nil {
		s.setupMu.Unlock()
		return 0, err
	}
	next := hashPassword(account, password, acct.Generation+1)
	err = s.sqlite.SetAccount(next)
	s.setupMu.Unlock()
	if err != nil {
		return 0, err
	}
	s.logins.succeeded()
	return s.signOutBefore(t, next.Generation, "")
}

func callerIP(ctx context.Context) string {
	if ci, ok := connect.CallInfoForHandlerContext(ctx); ok {
		return clientIP(ci.Peer().Addr)
	}
	return ""
}
