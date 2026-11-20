//go:build unix

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	hermeshcore "github.com/hermeshai/hermesh/cmd/hermesh-mobile/android"
	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	"github.com/hermeshai/hermesh/gen/go/hermesh/local/v1/localv1connect"
	sessionv1 "github.com/hermeshai/hermesh/gen/go/hermesh/session/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// TestAndroidCore is acceptance 2 of the Android app (WO #1) on the core
// the app binds (cmd/hermesh-mobile/android), driven through LocalService
// as the app drives it: the phone logs in with the account and password
// (nothing signed on it, no one approving), sees an agent that logged in,
// follows that agent's session live and answers its question, and the
// session carries on to its end.
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

	// The app starts its core, and logs in.
	info, err := hermeshcore.Start(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hermeshcore.Pause)
	hc := &http.Client{Transport: &bearer{token: info.Token, next: http.DefaultTransport}}
	phone := localv1connect.NewLocalServiceClient(hc, fmt.Sprintf("http://127.0.0.1:%d", info.Port))
	if _, err := phone.LogIn(ctx, &localv1.LogInRequest{RelayUrl: st.RelayUrl, Account: testAccount, Password: testPassword, Label: "Xiaomi 15 Pro", Platform: "android"}); err != nil {
		t.Fatalf("log in: %v", err)
	}
	if st, err := phone.GetStatus(ctx, &localv1.GetStatusRequest{}); err != nil || !st.Initialized {
		t.Fatalf("the phone after logging in: %v %v", st, err)
	}
	var devices *localv1.ListDevicesResponse
	eventually(t, 30*time.Second, "the phone among the Mac's devices", func() bool {
		devices, err = w.app.client.ListDevices(ctx, &localv1.ListDevicesRequest{})
		return err == nil && slices.ContainsFunc(devices.Devices, func(d *localv1.Device) bool { return d.Platform == "android" && d.Label == "Xiaomi 15 Pro" })
	})

	// The app's Watch, as the Kotlin app reads it (the JSON codec): its
	// notices come from it (push/LiveNotices.kt) while the connection
	// service keeps the core running. Recorded for the app's
	// LiveNoticesReplayTest (HERMESH_ANDROID_WATCH_RECORD names the file).
	watch := watchAsTheApp(t, hc, fmt.Sprintf("http://127.0.0.1:%d", info.Port))

	// An agent (the cloud assistant) logs in with the account; the phone's
	// Watch brings it, with no one approving.
	museEnv := []string{"HERMESH_HOME=" + filepath.Join(w.tmp, "muse")}
	bin := filepath.Join(w.bin, "hermesh")
	if r := run(t, append(slices.Clone(museEnv), loginEnv()...), 30*time.Second, bin, "auth", "login", w.server, "--name", "Muse"); r.code != 0 {
		t.Fatalf("login: %+v", r)
	}
	watch.wait(t, "the agent in the app's Watch", func(m *localv1.WatchResponse) bool {
		for _, c := range m.Clients {
			if c.Label == "Muse" {
				return true
			}
		}
		return false
	})

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
