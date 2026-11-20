package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	"github.com/samir2015dzh/aide/gen/go/aide/local/v1/localv1connect"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/mobile"
)

// TestPersonas: the owner adds a contact in the app (docs/personas.md) and
// talks to it as the desktop does, and from the phone's core: the first message starts a
// session with the contact's machine, project, agent, model, effort and
// mode; the next goes on in that session; once it is archived, the next
// starts a new one. Renaming keeps the session, a new mode starts another.
// A contact whose machine is offline sends nothing and starts nothing.
// Deleting a contact keeps its sessions.
func TestPersonas(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_DEEPSEEK_BIN=" + filepath.Join(binaries(t), "fakeacp"+exe), "FAKE_ACP_STATE=" + t.TempDir(), "FAKE_ACP_RESUME=resume",
		"FAKE_ACP_CONFIG=1", "FAKE_ACP_MODELS=magpie/v4-pro,magpie/v4-flash", "FAKE_ACP_EFFORTS=magpie/v4-pro=low|high;magpie/v4-flash=max", "FAKE_ACP_MODES=ask,code"},
		&localv1.GrantSpec{Agents: []string{"deepseek"}})
	ctx := context.Background()
	app := w.app.client
	home, err := app.GetStatus(ctx, &localv1.GetStatusRequest{})
	if err != nil {
		t.Fatal(err)
	}
	sessions := func() map[string]*sessionv1.Session {
		out := map[string]*sessionv1.Session{}
		res, err := app.ListSessions(ctx, &localv1.ListSessionsRequest{})
		if err == nil {
			for _, s := range res.Sessions {
				out[s.SessionId] = s
			}
		}
		return out
	}
	// idle waits for turn n of a session to finish, and returns what the agent said.
	idle := func(id string, n uint32) string {
		t.Helper()
		var said string
		eventually(t, time.Minute, "turn done", func() bool {
			s := sessions()[id]
			if s == nil || s.Status != sessionv1.SessionStatus_SESSION_STATUS_IDLE || s.Progress.GetTurnCount() < n {
				return false
			}
			said = s.Result
			return true
		})
		return said
	}
	// The agent tells its modes once a session ran it.
	eventually(t, time.Minute, "a first session", func() bool {
		_, err = app.StartSession(ctx, &localv1.StartSessionRequest{MachineId: home.LocalMachineId,
			Session: &sessionv1.StartSessionRequest{ProjectId: "demo", Agent: "deepseek", Prompt: "config"}})
		return err == nil
	})
	eventually(t, time.Minute, "deepseek's modes", func() bool {
		res, err := app.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil {
			return false
		}
		for _, m := range res.Machines {
			for _, a := range m.Status.GetAgents() {
				if a.Name == "deepseek" && len(a.Modes) == 2 {
					return true
				}
			}
		}
		return false
	})

	// A contact.
	if _, err := app.SavePersona(ctx, &localv1.SavePersonaRequest{Persona: &identityv1.Persona{Name: "", MachineId: home.LocalMachineId, ProjectId: "demo", Agent: "deepseek"}}); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("a contact without a name: %v", err)
	}
	saved, err := app.SavePersona(ctx, &localv1.SavePersonaRequest{Persona: &identityv1.Persona{Name: "Reviewer", Icon: "robot", Color: "#3366cc",
		MachineId: home.LocalMachineId, ProjectId: "demo", Agent: "deepseek", Model: "magpie/v4-pro", Effort: "high", Mode: "code"}})
	if err != nil {
		t.Fatal(err)
	}
	p := saved.Persona
	if p.PersonaId == "" || p.SessionId != "" {
		t.Fatalf("saved: %v", p)
	}
	talk := func(text string) *localv1.TalkToPersonaResponse {
		t.Helper()
		res, err := app.TalkToPersona(ctx, &localv1.TalkToPersonaRequest{PersonaId: p.PersonaId, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	// The first message starts a session with the contact's presets.
	first := talk("config")
	s1 := first.Session
	if !first.Started || first.Reason != "first" || first.Persona.SessionId != s1.SessionId || s1.CreatedBy != "owner" ||
		s1.MachineId != home.LocalMachineId || s1.ProjectId != "demo" || s1.Agent != "deepseek" {
		t.Fatalf("first: %v", first)
	}
	if got := idle(s1.SessionId, 1); got != "config: magpie/v4-pro high code" {
		t.Fatalf("first turn ran with %q", got)
	}
	// The next goes on in it.
	if again := talk("config"); again.Started || again.Session.SessionId != s1.SessionId {
		t.Fatalf("again: %v", again)
	}
	idle(s1.SessionId, 2)

	// The phone (the core the Android app runs) has the same contact, and
	// its message goes on in the same session.
	// Its data outside t.TempDir: the core has no Close, and its lock stays
	// held until the process ends (Windows can't remove a held file).
	phoneDir, err := os.MkdirTemp("", "aide-phone-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(phoneDir) })
	core, err := mobile.Start(phoneDir, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(core.Pause)
	phone := localv1connect.NewLocalServiceClient(&http.Client{Transport: &bearer{token: core.Token(), next: http.DefaultTransport}}, fmt.Sprintf("http://127.0.0.1:%d", core.Port()))
	if _, err := phone.LogIn(ctx, &localv1.LogInRequest{RelayUrl: home.RelayUrl, Account: testAccount, Password: testPassword, Label: "Test Phone", Platform: "android"}); err != nil {
		t.Fatalf("phone log in: %v", err)
	}
	onPhone, err := phone.ListPersonas(ctx, &localv1.ListPersonasRequest{})
	if err != nil || len(onPhone.Personas) != 1 || onPhone.Personas[0].Name != "Reviewer" || onPhone.Personas[0].SessionId != s1.SessionId {
		t.Fatalf("the phone's contacts: %v %v", onPhone, err)
	}
	var fromPhone *localv1.TalkToPersonaResponse
	eventually(t, time.Minute, "the phone reaching the session", func() bool {
		fromPhone, err = phone.TalkToPersona(ctx, &localv1.TalkToPersonaRequest{PersonaId: p.PersonaId, Text: "config"})
		return err == nil
	})
	if fromPhone.Started || fromPhone.Session.SessionId != s1.SessionId {
		t.Fatalf("from the phone: %v", fromPhone)
	}
	idle(s1.SessionId, 3)

	// A new name keeps it; every device lists the contact as kept.
	p.Name = "Code reviewer"
	if res, err := app.SavePersona(ctx, &localv1.SavePersonaRequest{Persona: p}); err != nil || res.Persona.SessionId != s1.SessionId {
		t.Fatalf("renamed: %v %v", res, err)
	}
	if again := talk("config"); again.Started || again.Session.SessionId != s1.SessionId {
		t.Fatalf("after renaming: %v", again)
	}
	idle(s1.SessionId, 4)

	// Archived, the session takes no more: the next message starts another.
	if _, err := app.ArchiveSession(ctx, &localv1.ArchiveSessionRequest{SessionId: s1.SessionId}); err != nil {
		t.Fatal(err)
	}
	after := talk("config")
	s2 := after.Session
	if !after.Started || after.Reason != "archived" || s2.SessionId == s1.SessionId || after.Persona.SessionId != s2.SessionId {
		t.Fatalf("after archiving: %v", after)
	}
	if got := idle(s2.SessionId, 1); got != "config: magpie/v4-pro high code" {
		t.Fatalf("new session ran with %q", got)
	}

	// Another mode: its sessions start another way, so a new one.
	p.Mode = "ask"
	changed, err := app.SavePersona(ctx, &localv1.SavePersonaRequest{Persona: p})
	if err != nil || changed.Persona.SessionId != "" {
		t.Fatalf("new mode: %v %v", changed, err)
	}
	s3 := talk("config")
	if !s3.Started || s3.Session.SessionId == s2.SessionId {
		t.Fatalf("after the new mode: %v", s3)
	}
	if got := idle(s3.Session.SessionId, 1); got != "config: magpie/v4-pro high ask" {
		t.Fatalf("with the new mode: %q", got)
	}

	// A contact on a machine that went offline: nothing sent, nothing started.
	other, _ := w.otherMac("work-mac", []string{"AIDE_FAKE_PROVIDERS=1"})
	otherProc := w.logs[len(w.logs)-1]
	far := w.logInMachineOnly(other, home, "work-mac").LocalMachineId
	if _, err := other.AddProject(ctx, &localv1.AddProjectRequest{MachineId: far, ProjectId: "billing", Path: gitRepo(t, map[string]string{"README.md": "# billing\n"})}); err != nil {
		t.Fatal(err)
	}
	fp, err := app.SavePersona(ctx, &localv1.SavePersonaRequest{Persona: &identityv1.Persona{Name: "Billing", MachineId: far, ProjectId: "billing", Agent: "claude"}})
	if err != nil {
		t.Fatal(err)
	}
	var fs *sessionv1.Session
	eventually(t, time.Minute, "a session on work-mac", func() bool {
		res, err := app.TalkToPersona(ctx, &localv1.TalkToPersonaRequest{PersonaId: fp.Persona.PersonaId, Text: "say hello", IdempotencyKey: "billing-1"})
		fs = res.GetSession()
		return err == nil
	})
	idle(fs.SessionId, 1)
	otherProc.kill()
	eventually(t, time.Minute, "work-mac offline", func() bool {
		res, err := app.ListMachines(ctx, &localv1.ListMachinesRequest{})
		if err != nil {
			return false
		}
		for _, m := range res.Machines {
			if m.MachineId == far {
				return !m.Online
			}
		}
		return false
	})
	before := len(sessions())
	if _, err := app.TalkToPersona(ctx, &localv1.TalkToPersonaRequest{PersonaId: fp.Persona.PersonaId, Text: "say lost"}); connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("to an offline machine: %v", err)
	}
	list, err := app.ListPersonas(ctx, &localv1.ListPersonasRequest{})
	if err != nil || len(list.Personas) != 2 || list.Personas[1].SessionId != fs.SessionId || len(sessions()) != before {
		t.Fatalf("after the offline message: %v %v", list, err)
	}

	// Deleting a contact keeps its sessions.
	if _, err := app.DeletePersona(ctx, &localv1.DeletePersonaRequest{PersonaId: p.PersonaId}); err != nil {
		t.Fatal(err)
	}
	list, err = app.ListPersonas(ctx, &localv1.ListPersonasRequest{})
	if err != nil || len(list.Personas) != 1 || list.Personas[0].PersonaId != fp.Persona.PersonaId {
		t.Fatalf("after deleting: %v %v", list, err)
	}
	all := sessions()
	for _, id := range []string{s1.SessionId, s2.SessionId, s3.Session.SessionId} {
		if all[id] == nil {
			t.Fatalf("session %s went with its contact", id)
		}
	}
	if _, err := app.TalkToPersona(ctx, &localv1.TalkToPersonaRequest{PersonaId: p.PersonaId, Text: "hi"}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("to a deleted contact: %v", err)
	}
}
