//go:build unix

package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	hermeshcore "github.com/hermeshai/hermesh/cmd/hermesh-mobile/android"
	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	"github.com/hermeshai/hermesh/gen/go/hermesh/local/v1/localv1connect"
	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
	"github.com/hermeshai/hermesh/pkg/identity"
	"google.golang.org/protobuf/encoding/protojson"
)

// androidApp plays the Android app's signer through the binding, as the
// Kotlin AppSigner does: keys it makes and keeps (software keys in place
// of the Keystore's), the cards it is shown, the person's approval.
type androidApp struct {
	mu      sync.Mutex
	keys    map[string]*identity.SoftwareKey
	cards   []string
	matched []string
	failed  []string
}

func (a *androidApp) CreateKey() ([]byte, error) {
	k, err := identity.GenerateKey()
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.keys[identity.KeyID(k.Public())] = k
	a.mu.Unlock()
	return identity.MarshalPublicKey(k.Public()), nil
}

func (a *androidApp) DeleteKey(keyID string) error {
	a.mu.Lock()
	delete(a.keys, keyID)
	a.mu.Unlock()
	return nil
}

func (a *androidApp) Approve(cards string) bool {
	a.mu.Lock()
	a.cards = append(a.cards, cards)
	a.mu.Unlock()
	return true
}

func (a *androidApp) Sign(keyID, messages string) (string, error) {
	a.mu.Lock()
	k := a.keys[keyID]
	a.mu.Unlock()
	if k == nil {
		return "", errors.New("this phone does not have that owner key")
	}
	var in [][]byte
	if err := json.Unmarshal([]byte(messages), &in); err != nil {
		return "", err
	}
	var out []string
	for _, m := range in {
		sig, err := k.SignMessage(m)
		if err != nil {
			return "", err
		}
		out = append(out, base64.StdEncoding.EncodeToString(sig))
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

func (a *androidApp) Failed(message string) {
	a.mu.Lock()
	a.failed = append(a.failed, message)
	a.mu.Unlock()
}

func (a *androidApp) Matched(label, digits string, machine bool) {
	a.mu.Lock()
	a.matched = append(a.matched, digits)
	a.mu.Unlock()
}

func (a *androidApp) Show()            {}
func (a *androidApp) Language() string { return "zh-CN" }

func (a *androidApp) seen() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.Join(a.cards, "\n")
}

// TestAndroidCore is acceptance 2 of the Android app (WO #1) on the core
// the app binds (cmd/hermesh-mobile/android), driven through LocalService
// as the app drives it: the phone joins as a trusted device (its root key
// made and used through the app's signer), approves an agent that asks to
// connect, follows that agent's session live and answers its question, and
// the session carries on to its end.
func TestAndroidCore(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"HERMESH_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	ctx := context.Background()
	st, err := w.app.client.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}

	// The app starts its core and its signer.
	info, err := hermeshcore.Start(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hermeshcore.Pause)
	app := &androidApp{keys: map[string]*identity.SoftwareKey{}}
	hermeshcore.RunSigner(app)
	hc := &http.Client{Transport: &bearer{token: info.Token, next: http.DefaultTransport}}
	phone := localv1connect.NewLocalServiceClient(hc, fmt.Sprintf("http://127.0.0.1:%d", info.Port))

	// Joining: no root key passed, so the core asks the app to make one.
	join, err := phone.RequestJoin(ctx, &localv1.RequestJoinRequest{RelayUrl: st.RelayUrl, OwnerId: st.OwnerId, RootLabel: "Xiaomi 15 Pro", RootPlatform: "android"})
	if err != nil {
		t.Fatalf("request join: %v", err)
	}
	app.mu.Lock()
	made := len(app.keys)
	app.mu.Unlock()
	if made != 1 {
		t.Fatalf("the app made %d root keys", made)
	}
	var approval *localv1.ApproveDevicePairingResponse
	eventually(t, 30*time.Second, "the phone's request approved on the Mac", func() bool {
		approval, err = w.app.client.ApproveDevicePairing(ctx, &localv1.ApproveDevicePairingRequest{Code: join.Code})
		return err == nil
	})
	eventually(t, 60*time.Second, "the approval reaches the phone", func() bool {
		r, err := phone.WaitJoin(ctx, &localv1.WaitJoinRequest{Code: join.Code})
		return err == nil && r.Done
	})
	if _, err := phone.ConfirmJoin(ctx, &localv1.ConfirmJoinRequest{Code: join.Code, Accept: true, MatchCode: approval.MatchCode}); err != nil {
		t.Fatal(err)
	}
	if st, err := phone.GetStatus(ctx, &localv1.GetStatusRequest{}); err != nil || !st.Initialized {
		t.Fatalf("the phone after joining: %v %v", st, err)
	}
	devices, err := w.app.client.ListDevices(ctx, &localv1.ListDevicesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var android bool
	for _, d := range devices.Devices {
		android = android || d.Platform == "android" && d.Label == "Xiaomi 15 Pro"
	}
	if !android {
		t.Fatalf("the Mac's trusted devices: %v", devices.Devices)
	}

	// The app's Watch, as the Kotlin app reads it (the JSON codec): its
	// notices come from it (push/LiveNotices.kt) while the connection
	// service keeps the core running. Recorded for the app's
	// LiveNoticesReplayTest (HERMESH_ANDROID_WATCH_RECORD names the file).
	watch := watchAsTheApp(t, hc, fmt.Sprintf("http://127.0.0.1:%d", info.Port))

	// An agent (the cloud assistant) asks to connect; the phone approves it,
	// shown the card of what it signs, in Chinese.
	museEnv := []string{"HERMESH_HOME=" + filepath.Join(w.tmp, "muse")}
	bin := filepath.Join(w.bin, "hermesh")
	login := start(t, "muse login", museEnv, bin, "auth", "login", w.addr, "--name", "Muse")
	code := login.pairingCode(t)
	watch.wait(t, "the request to connect in the app's Watch", func(m *localv1.WatchResponse) bool {
		for _, p := range m.Pairings {
			if p.Code == code && p.Label == "Muse" {
				return true
			}
		}
		return false
	})
	var details *localv1.PairingDetails
	eventually(t, 30*time.Second, "the agent's request on the phone", func() bool {
		r, err := phone.LookupPairing(ctx, &localv1.LookupPairingRequest{Code: code})
		if err == nil {
			details = r.Details
		}
		return err == nil
	})
	if _, err := phone.ApproveClientPairing(ctx, &localv1.ApproveClientPairingRequest{Code: code,
		Spec: &localv1.GrantSpec{Label: "Muse", Agents: []string{"*"}, TtlDays: 30}}); err != nil {
		t.Fatalf("approve the agent on the phone: %v", err)
	}
	if c := login.wait(t, 30*time.Second); c != 0 {
		t.Fatalf("login: %s", login.output())
	}
	if details.VerificationCode == "" || !strings.Contains(app.seen(), details.VerificationCode) || !strings.Contains(app.seen(), "Muse") {
		t.Fatalf("the cards shown (code %q):\n%s", details.VerificationCode, app.seen())
	}

	// The agent starts a session that asks; the phone sees it as Muse's, follows it, and answers.
	r := run(t, museEnv, time.Minute, bin, "session", "start", "--project", "demo", "-o", "json", "--prompt", "ask Color|Which color?|red,blue")
	s := firstSession(r.stdout)
	if s.SessionID == "" {
		t.Fatalf("start: %+v", r)
	}
	follow, stop := context.WithCancel(ctx)
	defer stop()
	stream, err := phone.StreamSessionEvents(follow, &localv1.StreamSessionEventsRequest{SessionId: s.SessionID, Follow: true, Partial: true, View: sessionv1.EventView_EVENT_VIEW_BASIC})
	if err != nil {
		t.Fatal(err)
	}
	events := make(chan *sessionv1.SessionEvent, 64)
	go func() {
		for stream.Receive() {
			events <- stream.Msg()
		}
		close(events)
	}()
	var q *sessionv1.Question
	eventually(t, time.Minute, "the question on the phone", func() bool {
		res, err := phone.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err != nil {
			return false
		}
		clients, _ := phone.ListClients(ctx, &localv1.ListClientsRequest{})
		for _, x := range res.Sessions {
			if x.SessionId != s.SessionID || x.Progress.GetPendingQuestion() == nil {
				continue
			}
			for _, c := range clients.GetClients() {
				if c.ClientId == x.CreatedBy && c.Label == "Muse" {
					q = x.Progress.PendingQuestion
				}
			}
		}
		return q != nil
	})
	// A person answers once the phone showed the question: the app's Watch brought it.
	watch.wait(t, "the question in the app's Watch", func(m *localv1.WatchResponse) bool {
		for _, x := range m.Sessions {
			if x.SessionId == s.SessionID && x.Status == sessionv1.SessionStatus_SESSION_STATUS_WAITING_INPUT && x.Progress.GetPendingQuestion() != nil {
				return true
			}
		}
		return false
	})
	if _, err := phone.AnswerQuestion(ctx, &localv1.AnswerQuestionRequest{SessionId: s.SessionID, QuestionId: q.QuestionId,
		Answers: []*sessionv1.Answer{{ItemId: q.Items[0].Id, Selected: []string{"red"}}}}); err != nil {
		t.Fatalf("answer on the phone: %v", err)
	}
	// The phone's live stream carries the whole turn: the question, its answer and the reply.
	var asked, resolved, replied bool
	deadline := time.After(time.Minute)
	for !(asked && resolved && replied) {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("the stream ended")
			}
			asked = asked || e.GetQuestionAsked() != nil
			resolved = resolved || e.GetQuestionResolved() != nil
			replied = replied || strings.Contains(e.GetAgentMessage().GetText(), "answer: red")
		case <-deadline:
			t.Fatalf("the phone's stream: asked %v, resolved %v, replied %v", asked, resolved, replied)
		}
	}
	// Muse's own sessions are Muse's: the phone (the owner) sees them end.
	eventually(t, time.Minute, "the session carries on to its end", func() bool {
		res, err := phone.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err != nil {
			return false
		}
		for _, x := range res.Sessions {
			if x.SessionId == s.SessionID {
				return x.Status == sessionv1.SessionStatus_SESSION_STATUS_IDLE && strings.Contains(x.Result, "answer: red")
			}
		}
		return false
	})

	// The owner's own session (started in the Mac's app; w.cli is an agent),
	// and one of Muse's that fails.
	mine, err := w.app.client.StartSession(ctx, &localv1.StartSessionRequest{MachineId: st.LocalMachineId,
		Session: &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "claude", Prompt: "say mine"}})
	if err != nil || mine.GetSession().GetCreatedBy() != "owner" {
		t.Fatalf("the owner's session: %v %v", mine, err)
	}
	failing := firstSession(run(t, museEnv, time.Minute, bin, "session", "start", "--project", "demo", "-o", "json", "--prompt", "sleep 3s\nfail boom").stdout)
	watch.wait(t, "the failure in the app's Watch", func(m *localv1.WatchResponse) bool {
		for _, x := range m.Sessions {
			if x.SessionId == failing.SessionID && x.Progress.GetLastFailure() != nil {
				return true
			}
		}
		return false
	})

	// The relay goes away while one of Muse's sessions is about to ask: the
	// core tries again until it is back, and the question comes then.
	later := firstSession(run(t, museEnv, time.Minute, bin, "session", "start", "--project", "demo", "-o", "json", "--prompt", "sleep 8s\nask Shade|Which shade?|light,dark").stdout)
	if later.SessionID == "" {
		t.Fatal("no session")
	}
	w.relay.stop()
	watch.wait(t, "the app sees the relay gone", func(m *localv1.WatchResponse) bool { return m.Status != nil && !m.Status.RelayConnected })
	time.Sleep(10 * time.Second) // the question is asked meanwhile
	w.startRelay()
	watch.wait(t, "the app reconnected", func(m *localv1.WatchResponse) bool { return m.Status != nil && m.Status.RelayConnected })
	watch.wait(t, "the question asked while the relay was away", func(m *localv1.WatchResponse) bool {
		for _, x := range m.Sessions {
			if x.SessionId == later.SessionID && x.Status == sessionv1.SessionStatus_SESSION_STATUS_WAITING_INPUT && x.Progress.GetPendingQuestion() != nil {
				return true
			}
		}
		return false
	})
	watch.save(t)

	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.failed) != 0 {
		t.Fatalf("the signer failed: %q", app.failed)
	}
}

// appWatch is the core's Watch as the app follows it: each message, when
// it came, in the JSON the app reads.
type appWatch struct {
	mu    sync.Mutex
	msgs  []*localv1.WatchResponse
	lines []string
	next  int // the first message wait has not looked at
}

func watchAsTheApp(t *testing.T, hc *http.Client, url string) *appWatch {
	t.Helper()
	jsonPhone := localv1connect.NewLocalServiceClient(hc, url, connect.WithProtoJSON())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w := &appWatch{}
	go func() {
		for ctx.Err() == nil {
			stream, err := jsonPhone.Watch(ctx, &localv1.WatchRequest{})
			if err == nil {
				for stream.Receive() {
					m := stream.Msg()
					b, _ := protojson.Marshal(m)
					line, _ := json.Marshal(struct {
						At    string          `json:"at"`
						Watch json.RawMessage `json:"watch"`
					}{time.Now().UTC().Format(time.RFC3339Nano), b})
					w.mu.Lock()
					w.msgs = append(w.msgs, m)
					w.lines = append(w.lines, string(line))
					w.mu.Unlock()
				}
			}
			time.Sleep(500 * time.Millisecond) // again, as the app does
		}
	}()
	return w
}

// wait waits for a message, from the next one not looked at on, that cond holds for.
func (w *appWatch) wait(t *testing.T, what string, cond func(*localv1.WatchResponse) bool) {
	t.Helper()
	eventually(t, 90*time.Second, what, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		for ; w.next < len(w.msgs); w.next++ {
			if cond(w.msgs[w.next]) {
				w.next++
				return true
			}
		}
		return false
	})
}

func (w *appWatch) save(t *testing.T) {
	out := os.Getenv("HERMESH_ANDROID_WATCH_RECORD")
	if out == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.WriteFile(out, []byte(strings.Join(w.lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
