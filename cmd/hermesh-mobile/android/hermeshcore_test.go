package hermeshcore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"testing"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	"github.com/hermeshai/hermesh/gen/go/hermesh/local/v1/localv1connect"
	"github.com/hermeshai/hermesh/internal/preview"
	"github.com/hermeshai/hermesh/internal/signer"
	"github.com/hermeshai/hermesh/pkg/identity"
)

// fakeApp is the Android app's signer as the binding sees it: Java's
// encoding of keys and messages, a software key in place of the Keystore's.
type fakeApp struct {
	keys     map[string]*identity.SoftwareKey
	approve  bool
	cards    string
	failed   []string
	matched  string
	refusing bool
}

func (a *fakeApp) CreateKey() ([]byte, error) {
	k, err := identity.GenerateKey()
	if err != nil {
		return nil, err
	}
	a.keys[identity.KeyID(k.Public())] = k
	return identity.MarshalPublicKey(k.Public()), nil
}

func (a *fakeApp) DeleteKey(keyID string) error { delete(a.keys, keyID); return nil }

func (a *fakeApp) Approve(cards string) bool { a.cards = cards; return a.approve }

func (a *fakeApp) Sign(keyID, messages string) (string, error) {
	if a.refusing {
		return "", errors.New("cancelled")
	}
	k := a.keys[keyID]
	if k == nil {
		return "", errors.New("no such key")
	}
	var in []string
	if err := json.Unmarshal([]byte(messages), &in); err != nil {
		return "", err
	}
	out := []string{}
	for _, m := range in {
		b, err := base64.StdEncoding.DecodeString(m)
		if err != nil {
			return "", err
		}
		sig, err := k.SignMessage(b)
		if err != nil {
			return "", err
		}
		out = append(out, base64.StdEncoding.EncodeToString(sig))
	}
	o, _ := json.Marshal(out)
	return string(o), nil
}

func (a *fakeApp) Failed(message string) { a.failed = append(a.failed, message) }
func (a *fakeApp) Matched(label, digits string, m bool) {
	a.matched = fmt.Sprintf("%s %s %v", label, digits, m)
}
func (a *fakeApp) Show()            {}
func (a *fakeApp) Language() string { return "zh-CN" }

func TestKeysCreateAndSignThroughTheApp(t *testing.T) {
	app := &fakeApp{keys: map[string]*identity.SoftwareKey{}}
	k := keys{app}
	pub, err := k.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id, err := KeyID(identity.MarshalPublicKey(pub))
	if err != nil || app.keys[id] == nil {
		t.Fatalf("KeyID %q (%v) is not the app's key", id, err)
	}
	msgs := [][]byte{[]byte("one"), {0, 1, 2, 255}}
	sigs, err := k.Sign(context.Background(), id, msgs)
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range msgs {
		if err := identity.VerifyRaw(pub, m, sigs[i]); err != nil {
			t.Fatalf("signature %d: %v", i, err)
		}
	}
	app.refusing = true
	if _, err := k.Sign(context.Background(), id, msgs); err == nil {
		t.Fatal("a cancelled confirmation signed")
	}
	if _, err := KeyID([]byte("not a key")); err == nil {
		t.Fatal("KeyID took garbage")
	}
}

func TestOwnerShowsTheCardsAsJSON(t *testing.T) {
	app := &fakeApp{approve: true}
	o := owner{app}
	ok, err := o.Approve(context.Background(), []preview.Card{{Title: "Approve a machine", Code: "1234", Lines: []string{"a", "b"}, Own: true}})
	if err != nil || !ok {
		t.Fatalf("approve: %v %v", ok, err)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(app.cards), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["title"] != "Approve a machine" || got[0]["code"] != "1234" || got[0]["own"] != true || len(got[0]["lines"].([]any)) != 2 {
		t.Fatalf("cards: %s", app.cards)
	}
	// A refusal is the person's own answer: nothing to tell them.
	o.Failed(signer.ErrRefused)
	o.Failed(errors.New("the relay went away"))
	if len(app.failed) != 1 || app.failed[0] != "the relay went away" {
		t.Fatalf("failed: %q", app.failed)
	}
	o.Matched(preview.Match{Label: "Pixel", Digits: "42"})
	if app.matched != "Pixel 42 false" {
		t.Fatalf("matched: %q", app.matched)
	}
}

// The binding starts the iPhone's core: LocalService on loopback for the
// token alone, once per process, and its signer stops with Pause.
func TestStartServesLocalService(t *testing.T) {
	// The core lives as long as the process, as the app's does, its log
	// open: Windows can't remove its folder before the process ends, so
	// not t.TempDir, whose cleanup would fail the test there.
	dir, err := os.MkdirTemp("", "hermeshcore")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	info, err := Start(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Start(t.TempDir())
	if again.Port != info.Port || again.Token != info.Token {
		t.Fatal("a second Start started another core")
	}
	RunSigner(&fakeApp{keys: map[string]*identity.SoftwareKey{}})
	hc := &http.Client{Transport: bearer{token: info.Token}}
	c := localv1connect.NewLocalServiceClient(hc, fmt.Sprintf("http://127.0.0.1:%d", info.Port))
	st, err := c.GetStatus(context.Background(), &localv1.GetStatusRequest{})
	if err != nil || st.Initialized {
		t.Fatalf("status: %v %v", st, err)
	}
	Pause()
	mu.Lock()
	running := cancel != nil
	mu.Unlock()
	if running {
		t.Fatal("the signer runs in the background")
	}
	Resume()
	mu.Lock()
	running = cancel != nil
	mu.Unlock()
	if !running {
		t.Fatal("the signer did not come back with Resume")
	}
	NetworkChanged()
	PathChanged()
	Pause()
}
