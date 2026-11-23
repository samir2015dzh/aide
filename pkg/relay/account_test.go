package relay

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"github.com/samir2015dzh/aide/internal/legacy"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/crypto/argon2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/gen/go/aide/relay/v1/relayv1connect"
	"github.com/samir2015dzh/aide/pkg/identity"
)

func newTestServer(t *testing.T) (*Server, relayv1connect.RelayPublicServiceClient) {
	t.Helper()
	s, err := New(Config{DataDir: t.TempDir(), Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	hs := httptest.NewServer(s.Handler())
	t.Cleanup(hs.Close)
	return s, relayv1connect.NewRelayPublicServiceClient(hs.Client(), hs.URL)
}

func joiner(kind relayv1.JoinKind) (*identity.SoftwareKey, *relayv1.Joiner) {
	k, _ := identity.GenerateKey()
	return k, &relayv1.Joiner{Kind: kind, PublicKey: identity.MarshalPublicKey(k.Public()), Label: "x"}
}

// The server keeps the account: set up once with its setup code, it signs
// in a device, a machine and an agent for the right password alone, each
// with a credential that verifies under the owner its root chain names;
// a wrong password, a wrong setup code or a second setup is refused, and
// the account locks after five wrong passwords in a row.
func TestAccountLogIn(t *testing.T) {
	s, pub := newTestServer(t)
	ctx := context.Background()
	code, _ := s.SetupCode()
	_, dj := joiner(relayv1.JoinKind_JOIN_KIND_DEVICE)
	if _, err := pub.SetUp(ctx, &relayv1.SetUpRequest{SetupCode: "WRONG", Account: "me", Password: "correct horse", Joiner: dj}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a wrong setup code: %v", err)
	}
	if _, err := pub.SetUp(ctx, &relayv1.SetUpRequest{SetupCode: code, Account: "me", Password: "short", Joiner: dj}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a short password: %v", err)
	}
	set, err := pub.SetUp(ctx, &relayv1.SetUpRequest{SetupCode: code, Account: " Me ", Password: "correct horse", Joiner: dj})
	if err != nil {
		t.Fatal(err)
	}
	if set.Account != "me" || set.OwnerId != s.SingleOwner() {
		t.Fatalf("setup: %v", set)
	}
	if again, _ := s.SetupCode(); again != "" {
		t.Fatalf("a setup code once set up: %q", again)
	}
	if _, err := pub.SetUp(ctx, &relayv1.SetUpRequest{SetupCode: code, Account: "evil", Password: "evil password", Joiner: dj}); connect.CodeOf(err) != connect.CodeAlreadyExists {
		t.Fatalf("a second setup: %v", err)
	}
	trust := identity.NewTrust(set.OwnerId)
	if _, err := trust.AcceptRootChain(set.RootChain); err != nil || len(trust.Root().RootKeys) != 1 || trust.Root().RootKeys[0].Platform != ServerKeyPlatform {
		t.Fatalf("the root chain: %v %v", trust.Root(), err)
	}
	if _, _, err := trust.VerifyDeviceCert(set.Credential); err != nil {
		t.Fatal(err)
	}

	// A device with its own machine, a machine, an agent.
	devKey, dev := joiner(relayv1.JoinKind_JOIN_KIND_DEVICE)
	mk, _ := identity.GenerateKey()
	dev.MachinePublicKey = identity.MarshalPublicKey(mk.Public())
	res, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "ME", Password: "correct horse", Joiner: dev})
	if err != nil {
		t.Fatal(err)
	}
	nc, _, err := trust.VerifyMachineCert(res.MachineCert)
	if err != nil || nc.DeviceKeyId != identity.KeyID(devKey.Public()) {
		t.Fatalf("the device's machine: %v %v", nc, err)
	}
	_, mach := joiner(relayv1.JoinKind_JOIN_KIND_MACHINE)
	if res, err = pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "correct horse", Joiner: mach}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := trust.VerifyMachineCert(res.Credential); err != nil {
		t.Fatal(err)
	}
	_, agent := joiner(relayv1.JoinKind_JOIN_KIND_AGENT)
	if res, err = pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "correct horse", Joiner: agent}); err != nil {
		t.Fatal(err)
	}
	g, _, err := trust.VerifyGrant(res.Credential, time.Now())
	if err != nil || !identity.Allows(g.MachineIds, "m_any") || !identity.Allows(g.ProjectIds, "p") || g.TaskVisibility != identityv1.TaskVisibility_TASK_VISIBILITY_OWN {
		t.Fatalf("the agent's grant: %v %v", g, err)
	}

	// Guessing: refused alike, then locked.
	for i := range lockAfter {
		if _, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "guess", Joiner: agent}); connect.CodeOf(err) != connect.CodePermissionDenied {
			t.Fatalf("guess %d: %v", i, err)
		}
	}
	if _, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "correct horse", Joiner: agent}); connect.CodeOf(err) != connect.CodeResourceExhausted {
		t.Fatalf("locked: %v", err)
	}
}

// The server signs for a device of the owner what adding or managing a
// machine or an agent takes, and nothing for another owner or of another
// kind; a new password signs out the other devices and the agents, and
// keeps the device that changed it and the machines.
func TestAccountSignsAndSignsOut(t *testing.T) {
	s, pub := newTestServer(t)
	ctx := context.Background()
	code, _ := s.SetupCode()
	keepKey, keep := joiner(relayv1.JoinKind_JOIN_KIND_DEVICE)
	set, err := pub.SetUp(ctx, &relayv1.SetUpRequest{SetupCode: code, Account: "me", Password: "correct horse", Joiner: keep})
	if err != nil {
		t.Fatal(err)
	}
	t1, _ := s.tenantFor(set.OwnerId)
	o := &ownerControl{s: s}
	pctx := context.WithValue(ctx, peerKey{}, &peer{t: t1, role: relayv1.Role_ROLE_OWNER,
		subject: &identity.Subject{Kind: identity.SubjectOwnerDevice, PublicKey: keepKey.Public()}})
	body := func(m proto.Message) *relayv1.Unsigned {
		b, _ := proto.Marshal(m)
		return &relayv1.Unsigned{Type: identity.MessageType(m), Body: b}
	}
	mk, _ := identity.GenerateKey()
	spki := identity.MarshalPublicKey(mk.Public())
	signed, err := o.Sign(pctx, &relayv1.SignRequest{Messages: []*relayv1.Unsigned{body(&identityv1.MachineCert{OwnerId: set.OwnerId, MachineId: identity.MachineID(spki), MachinePublicKey: spki})}})
	if err != nil || len(signed.Signed) != 1 {
		t.Fatalf("sign a machine cert: %v %v", signed, err)
	}
	if _, _, err := t1.trust.VerifyMachineCert(signed.Signed[0]); err != nil {
		t.Fatal(err)
	}
	for name, u := range map[string]*relayv1.Unsigned{
		"another owner's": body(&identityv1.MachineCert{OwnerId: "o_other", MachineId: identity.MachineID(spki), MachinePublicKey: spki}),
		"a root cert":     body(&identityv1.RootCert{OwnerId: set.OwnerId, Version: 9}),
	} {
		if _, err := o.Sign(pctx, &relayv1.SignRequest{Messages: []*relayv1.Unsigned{u}}); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: %v", name, err)
		}
	}

	otherKey, other := joiner(relayv1.JoinKind_JOIN_KIND_DEVICE)
	if _, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "correct horse", Joiner: other}); err != nil {
		t.Fatal(err)
	}
	_, agent := joiner(relayv1.JoinKind_JOIN_KIND_AGENT)
	ares, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "correct horse", Joiner: agent})
	if err != nil {
		t.Fatal(err)
	}
	g := &identityv1.Grant{}
	identity.Unverified(ares.Credential, g)
	_, mach := joiner(relayv1.JoinKind_JOIN_KIND_MACHINE)
	mres, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "correct horse", Joiner: mach})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.SetLogin(pctx, &relayv1.SetLoginRequest{CurrentPassword: "wrong", NewPassword: "a new password"}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a wrong current password: %v", err)
	}
	ch, err := o.SetLogin(pctx, &relayv1.SetLoginRequest{CurrentPassword: "correct horse", NewPassword: "a new password", DeviceKey: identity.KeyID(keepKey.Public())})
	if err != nil || ch.SignedOut != 2 {
		t.Fatalf("a new password: %v %v", ch, err)
	}
	rev := t1.revocations
	if !slices.Contains(rev.RevokedDeviceKeys, identity.KeyID(otherKey.Public())) || slices.Contains(rev.RevokedDeviceKeys, identity.KeyID(keepKey.Public())) ||
		!slices.Contains(rev.RevokedGrantIds, g.GrantId) || slices.Contains(rev.RevokedMachineIds, identity.MachineID(mach.PublicKey)) {
		t.Fatalf("revocations after a new password: %v", rev)
	}
	if isRevoked(rev, &identity.Subject{Kind: identity.SubjectOwnerDevice, PublicKey: otherKey.Public()}) == nil {
		t.Fatal("the other device is still admitted")
	}
	_ = mres
	if _, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "correct horse", Joiner: agent}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("the old password: %v", err)
	}
	if _, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "a new password", Joiner: agent}); err != nil {
		t.Fatalf("the new password: %v", err)
	}

	// Signing a device out from another removes its own machine too.
	thirdKey, third := joiner(relayv1.JoinKind_JOIN_KIND_DEVICE)
	tk, _ := identity.GenerateKey()
	third.MachinePublicKey = identity.MarshalPublicKey(tk.Public())
	if _, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "a new password", Joiner: third}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.RemoveDevice(pctx, &relayv1.RemoveDeviceRequest{KeyId: identity.KeyID(keepKey.Public())}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a device signing itself out: %v", err)
	}
	if _, err := o.RemoveDevice(pctx, &relayv1.RemoveDeviceRequest{KeyId: identity.KeyID(thirdKey.Public())}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(t1.revocations.RevokedMachineIds, identity.MachineID(third.MachinePublicKey)) {
		t.Fatalf("its machine stays: %v", t1.revocations)
	}
	devs, _ := s.devices(t1)
	for _, d := range devs.Devices {
		if d.KeyId == identity.KeyID(thirdKey.Public()) || d.KeyId == identity.KeyID(otherKey.Public()) {
			t.Fatalf("a device signed out is listed: %v", devs)
		}
	}
}

// A server set up before accounts, with a password login (the password
// key, a root key, sealed by the password), converts it at the first login
// with its password: the next root cert drops the password key and lists a
// key of the server's own, what the password key signed is signed again by
// it, the account keeps working with the same password, and the old record
// is gone.
func TestAccountFromAnOldPasswordLogin(t *testing.T) {
	dir := t.TempDir()
	device, _ := identity.GenerateKey()
	passwordKey, _ := identity.GenerateKey()
	dspki, pspki := identity.MarshalPublicKey(device.Public()), identity.MarshalPublicKey(passwordKey.Public())
	ownerID := identity.OwnerID(dspki)
	v1, _ := identity.Sign(&identityv1.RootCert{OwnerId: ownerID, Version: 1, RootKeys: []*identityv1.RootKey{{PublicKey: dspki, Label: "pc", Platform: "windows"}}}, device)
	v2, _ := identity.Sign(&identityv1.RootCert{OwnerId: ownerID, Version: 2, RootKeys: []*identityv1.RootKey{{PublicKey: dspki, Label: "pc", Platform: "windows"},
		{PublicKey: pspki, Label: "账号密码登录", Platform: "password"}}}, device)
	chain := &identityv1.RootChain{Certs: []*identityv1.Signed{v1, v2}}
	// What the old app kept on the server for account "me", password "correct horse".
	salt := make([]byte, 16)
	rand.Read(salt)
	derived := argon2.IDKey([]byte("correct horse"), salt, 1, 8*1024, 1, 64)
	verifier := sha256.Sum256(derived[:32])
	block, _ := aes.NewCipher(derived[32:])
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	der, _ := x509.MarshalPKCS8PrivateKey(passwordKey.Key)
	sealed := gcm.Seal(nil, nonce, der, append([]byte(legacy.PasswordKey+ownerID+"\x00"), pspki...))
	old := &relayv1.OldPasswordLogin{Account: "me", Kdf: &relayv1.OldPasswordLogin_Kdf{Salt: salt, Time: 1, MemoryKib: 8 * 1024, Threads: 1},
		Verifier: verifier[:], Nonce: nonce, SealedKey: sealed, PublicKey: pspki}
	// An agent the password key had signed in, and a machine the device had.
	ak, _ := identity.GenerateKey()
	aspki := identity.MarshalPublicKey(ak.Public())
	grant := &identityv1.Grant{OwnerId: ownerID, GrantId: "g_old", Version: 1, ClientId: identity.ClientID(aspki), ClientPublicKey: aspki,
		MachineIds: []string{"*"}, ProjectIds: []string{"*"}, Agents: []string{"*"}, ExpiresAt: timestamppb.New(identity.NoExpiry)}
	sg, _ := identity.Sign(grant, passwordKey)
	st, err := OpenStore(filepath.Join(dir, "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSetup(ownerID, chain, nil, nil); err != nil {
		t.Fatal(err)
	}
	b, _ := proto.Marshal(old)
	st.setMeta("password_login", b)
	st.PutGrant(ownerID, grant, sg)
	st.Close()

	s, err := New(Config{DataDir: dir, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	hs := httptest.NewServer(s.Handler())
	defer hs.Close()
	pub := relayv1connect.NewRelayPublicServiceClient(hs.Client(), hs.URL)
	ctx := context.Background()
	_, dj := joiner(relayv1.JoinKind_JOIN_KIND_DEVICE)
	if _, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "wrong password", Joiner: dj}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a wrong password before converting: %v", err)
	}
	res, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "correct horse", Joiner: dj})
	if err != nil {
		t.Fatalf("the first login: %v", err)
	}
	trust := identity.NewTrust(ownerID)
	if _, err := trust.AcceptRootChain(res.RootChain); err != nil {
		t.Fatal(err)
	}
	var platforms []string
	for _, rk := range trust.Root().RootKeys {
		platforms = append(platforms, rk.Platform)
	}
	if trust.RootVersion() != 3 || !slices.Equal(platforms, []string{"windows", ServerKeyPlatform}) {
		t.Fatalf("the root cert after converting: v%d %v", trust.RootVersion(), platforms)
	}
	if _, _, err := trust.VerifyDeviceCert(res.Credential); err != nil {
		t.Fatal(err)
	}
	again, err := s.store.GrantForClient(ownerID, identity.ClientID(aspki))
	if err != nil {
		t.Fatal(err)
	}
	if g, _, err := trust.VerifyGrant(again, time.Now()); err != nil || g.Version != 2 {
		t.Fatalf("the agent's grant, signed again: %v %v", g, err)
	}
	if l, _ := s.sqlite.oldPasswordLogin(); l != nil {
		t.Fatal("the old record stays")
	}
	if _, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "correct horse", Joiner: dj}); err != nil {
		t.Fatalf("a login after converting: %v", err)
	}
}

// A setup code lasts its time (Config.SetupCodeTTL): expired, it is
// refused, and the next one asked for is new, the old one refused still;
// a code kept from before codes expired starts its time when next asked
// for; a server set up has none, whatever the time.
func TestSetupCodeExpires(t *testing.T) {
	st, err := OpenStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t0 := time.Unix(1_800_000_000, 0)
	code, until, err := st.SetupCode(t0, time.Hour)
	if err != nil || code == "" || !until.Equal(t0.Add(time.Hour)) {
		t.Fatalf("first: %q %v %v", code, until, err)
	}
	if again, u, _ := st.SetupCode(t0.Add(59*time.Minute), time.Hour); again != code || !u.Equal(until) {
		t.Fatalf("within its time: %q %v", again, u)
	}
	next, u, _ := st.SetupCode(t0.Add(time.Hour), time.Hour)
	if next == code || next == "" || !u.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("after its time: %q %v", next, u)
	}
	if never, u, _ := st.SetupCode(t0.Add(2*time.Hour), 0); never == next || !u.IsZero() {
		t.Fatalf("forever: %q %v", never, u)
	}
	// From before codes expired: kept, timed from now.
	st.setMeta("claim_code", []byte("OLDC-ODE0-0000-0000"))
	st.db.Exec(`DELETE FROM meta WHERE key='claim_code_until'`)
	if old, u, _ := st.SetupCode(t0, time.Hour); old != "OLDC-ODE0-0000-0000" || !u.Equal(t0.Add(time.Hour)) {
		t.Fatalf("a code from before: %q %v", old, u)
	}

	s, pub := newTestServer(t)
	ctx := context.Background()
	_, dj := joiner(relayv1.JoinKind_JOIN_KIND_DEVICE)
	s.cfg.SetupCodeTTL = time.Hour
	stale, _ := s.SetupCode()
	s.sqlite.setMeta("claim_code_until", []byte(strconv.FormatInt(time.Now().Add(-time.Second).Unix(), 10)))
	if _, err := pub.SetUp(ctx, &relayv1.SetUpRequest{SetupCode: stale, Account: "me", Password: "correct horse", Joiner: dj}); connect.CodeOf(err) != connect.CodeFailedPrecondition ||
		!strings.Contains(err.Error(), "aide-relay setup-code") {
		t.Fatalf("an expired code: %v", err)
	}
	fresh, _ := s.SetupCode()
	if fresh == stale {
		t.Fatal("no new code after the old one expired")
	}
	if _, err := pub.SetUp(ctx, &relayv1.SetUpRequest{SetupCode: stale, Account: "me", Password: "correct horse", Joiner: dj}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("the expired code after a new one: %v", err)
	}
	if _, err := pub.SetUp(ctx, &relayv1.SetUpRequest{SetupCode: fresh, Account: "me", Password: "correct horse", Joiner: dj}); err != nil {
		t.Fatal(err)
	}
	s.sqlite.setMeta("claim_code_until", []byte("1"))
	if c, _ := s.SetupCode(); c != "" {
		t.Fatalf("a server set up has a setup code: %q", c)
	}
}

// Deleting an agent from the roster (工单 #82) is revoking its grant: the
// server then forgets everything it kept of it — the grant, its login, its
// contact card, its inbox and what waited there — so it is on no list, an
// agent that never connected as much as one that did. Only the revocation
// itself stays, to refuse it.
func TestRevokedAgentIsForgotten(t *testing.T) {
	s, pub := newTestServer(t)
	ctx := context.Background()
	code, _ := s.SetupCode()
	_, dj := joiner(relayv1.JoinKind_JOIN_KIND_DEVICE)
	if _, err := pub.SetUp(ctx, &relayv1.SetUpRequest{SetupCode: code, Account: "me", Password: "correct horse", Joiner: dj}); err != nil {
		t.Fatal(err)
	}
	owner := s.SingleOwner()
	_, agent := joiner(relayv1.JoinKind_JOIN_KIND_AGENT)
	res, err := pub.LogIn(ctx, &relayv1.LogInRequest{Account: "me", Password: "correct horse", Joiner: agent})
	if err != nil {
		t.Fatal(err)
	}
	g := &identityv1.Grant{}
	identity.Unverified(res.Credential, g)
	ms := s.messageStore()
	ms.PutCard(owner, g.ClientId, &identityv1.Signed{Body: []byte("card")}, &identityv1.Signed{Body: []byte("cert")})
	ms.PutRegistration(owner, g.ClientId, &identityv1.Signed{Body: []byte("reg")})
	ms.PutMessage(owner, Message{ID: "msg_a", Inbox: g.ClientId, Sealed: []byte("x"), ReceivedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})

	if err := s.ApplyRevocations(owner, &identityv1.RevocationList{RevokedGrantIds: []string{g.GrantId}}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := s.ClientEntries(owner); len(entries) != 0 {
		t.Fatalf("still listed: %v", entries)
	}
	if logins, _ := s.sqlite.Logins(); slices.ContainsFunc(logins, func(l Login) bool { return l.KeyID == g.GrantId }) {
		t.Fatal("its login is kept")
	}
	if cards, _ := ms.Cards(owner); cards[g.ClientId].Card != nil {
		t.Fatal("its card is kept")
	}
	if regs, _ := ms.Registrations(owner); regs[g.ClientId] != nil {
		t.Fatal("its inbox is kept")
	}
	if waiting, _ := ms.WaitingMessages(owner, g.ClientId); len(waiting) != 0 {
		t.Fatalf("messages wait for it: %v", waiting)
	}
	if rev, _ := s.store.Revocations(owner); !slices.Contains(rev.GetRevokedGrantIds(), g.GrantId) {
		t.Fatal("the revocation must stay")
	}
}
