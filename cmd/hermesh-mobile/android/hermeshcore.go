// Package hermeshcore is the owner role bound for the Android app with
// gomobile (apps/android/scripts/build-core.sh: gomobile bind
// -target=android): internal/mobile, the core the iPhone app had too (its C
// library went with it, removed with the fork's iPhone app).
// Everything else goes through LocalService on the loopback port Start
// returns. The app's signer (Android Keystore and BiometricPrompt) answers
// the owner role's signing requests through RunSigner, which shows it what
// each batch signs as internal/preview words it for the Linux and Windows
// signers.
package hermeshcore

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/hermeshai/hermesh/gen/go/hermesh/local/v1/localv1connect"
	"github.com/hermeshai/hermesh/internal/mobile"
	"github.com/hermeshai/hermesh/internal/preview"
	"github.com/hermeshai/hermesh/internal/signer"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// Info is where the core serves LocalService: 127.0.0.1:Port, to callers
// that send "Authorization: Bearer <Token>".
type Info struct {
	Port  int
	Token string
}

var (
	mu     sync.Mutex
	core   *mobile.Core
	app    Signer
	cancel context.CancelFunc
)

// Start starts the owner role with its data in dataDir (once per process;
// later calls return the same port and token).
func Start(dataDir string) (*Info, error) {
	mu.Lock()
	defer mu.Unlock()
	if core == nil {
		c, err := mobile.Start(dataDir, mobile.Logger(dataDir))
		if err != nil {
			return nil, err
		}
		core = c
	}
	return &Info{Port: core.Port(), Token: core.Token()}, nil
}

// Resume keeps the relay connection up and answers signing requests again
// (the app came to the front).
func Resume() {
	mu.Lock()
	defer mu.Unlock()
	if core == nil {
		return
	}
	core.Resume()
	runSignerLocked()
}

// Pause stops following the machines and answering signing requests,
// keeping the relay connection to try again first (the app went to the
// background).
func Pause() {
	mu.Lock()
	c := core
	if cancel != nil {
		cancel()
		cancel = nil
	}
	mu.Unlock()
	if c != nil {
		c.Pause()
	}
}

// NetworkChanged connects again over the network now in use.
func NetworkChanged() {
	if c := started(); c != nil {
		c.Reconnect()
	}
}

// PathChanged connects again when the device's addresses are not those
// last seen.
func PathChanged() {
	if c := started(); c != nil {
		c.PathChanged()
	}
}

func started() *mobile.Core {
	mu.Lock()
	defer mu.Unlock()
	return core
}

// KeyID is the id the owner role names a root key by, from its public key
// (SPKI DER): SigningRequest.root_key_id.
func KeyID(spki []byte) (string, error) {
	pub, err := identity.ParsePublicKey(spki)
	if err != nil {
		return "", err
	}
	return identity.KeyID(pub), nil
}

// Signer is the app's: its root keys, one per profile, which never leave
// the phone, and the person it asks. Each method may block until the
// person answered.
type Signer interface {
	// CreateKey makes a new root key, keeping the others, and returns its
	// public key (SPKI DER).
	CreateKey() ([]byte, error)
	// DeleteKey forgets the root key keyID (the phone left its owner).
	DeleteKey(keyID string) error
	// Approve shows what a batch signs, a JSON array of cards
	// ({"title","code","lines","why","warning","own"}), and returns whether
	// the person approved it.
	Approve(cards string) bool
	// Sign signs each message, a JSON array of base64, with the root key
	// keyID once the person confirmed it is them (biometrics or the screen
	// lock), and returns the signatures in the same form: ASN.1 DER
	// ECDSA P-256 over SHA-256 of each message.
	Sign(keyID string, messages string) (string, error)
	// Failed tells that a batch was not signed, and why.
	Failed(message string)
	// Matched gives the two digits to type on the device or machine a
	// batch just admitted; label names the device ("" for a machine).
	Matched(label, digits string, machine bool)
	// Show brings an approval that is open forward.
	Show()
	// Language is the one the cards are worded in: "en" or "zh-CN".
	Language() string
}

// RunSigner makes s answer the owner role's signing requests while the app
// is in front (from now, and after each Resume).
func RunSigner(s Signer) {
	mu.Lock()
	defer mu.Unlock()
	app = s
	runSignerLocked()
}

func runSignerLocked() {
	if core == nil || app == nil || cancel != nil {
		return
	}
	ctx, stop := context.WithCancel(context.Background())
	cancel = stop
	hc := &http.Client{Transport: bearer{token: core.Token()}}
	c := localv1connect.NewLocalServiceClient(hc, fmt.Sprintf("http://127.0.0.1:%d", core.Port()))
	go signer.Run(ctx, c, keys{app}, owner{app})
}

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

// keys are the app's, as internal/signer asks for them.
type keys struct{ s Signer }

func (k keys) Create(context.Context) (*ecdsa.PublicKey, error) {
	spki, err := k.s.CreateKey()
	if err != nil {
		return nil, err
	}
	return identity.ParsePublicKey(spki)
}

func (k keys) Delete(keyID string) error { return k.s.DeleteKey(keyID) }

func (k keys) Sign(_ context.Context, keyID string, messages [][]byte) ([][]byte, error) {
	in, _ := json.Marshal(messages) // [][]byte: an array of base64
	out, err := k.s.Sign(keyID, string(in))
	if err != nil {
		return nil, err
	}
	var sigs [][]byte
	if err := json.Unmarshal([]byte(out), &sigs); err != nil {
		return nil, fmt.Errorf("the app's signatures: %w", err)
	}
	if len(sigs) != len(messages) {
		return nil, errors.New("the app signed another number of messages")
	}
	return sigs, nil
}

// owner is the person, as internal/signer asks them.
type owner struct{ s Signer }

// card is a preview.Card as the app reads it.
type card struct {
	Title   string   `json:"title"`
	Code    string   `json:"code,omitempty"`
	Lines   []string `json:"lines,omitempty"`
	Why     string   `json:"why,omitempty"`
	Warning string   `json:"warning,omitempty"`
	Own     bool     `json:"own,omitempty"`
}

func (o owner) Approve(_ context.Context, cards []preview.Card) (bool, error) {
	out := make([]card, len(cards))
	for i, c := range cards {
		out[i] = card(c)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return false, err
	}
	return o.s.Approve(string(b)), nil
}

func (o owner) Failed(err error) {
	if !errors.Is(err, signer.ErrRefused) {
		o.s.Failed(err.Error())
	}
}

func (o owner) Show()                   { o.s.Show() }
func (o owner) Language() string        { return o.s.Language() }
func (o owner) Matched(m preview.Match) { o.s.Matched(m.Label, m.Digits, m.Machine) }
