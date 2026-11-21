package relay

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
)

// Contacts are kept for the owner's devices: a change to what a contact's
// sessions start with forgets its session, a new name or icon keeps it;
// an agent or machine can't touch them; erasing the owner forgets them.
func TestPersonas(t *testing.T) {
	st, err := OpenStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	o := &ownerControl{s: &Server{store: st}}
	tn := &tenant{ownerID: "o_x"}
	device := context.WithValue(context.Background(), peerKey{}, &peer{t: tn, deviceCert: &identityv1.Signed{}})
	agent := context.WithValue(context.Background(), peerKey{}, &peer{t: tn})

	if _, err := o.ListPersonas(agent, &relayv1.ListPersonasRequest{}); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("an agent listed contacts: %v", err)
	}
	for _, bad := range []*identityv1.Persona{
		{Name: " ", MachineId: "m_1", ProjectId: "p", Agent: "claude"},
		{Name: "A", ProjectId: "p", Agent: "claude"},
		{Name: "A", MachineId: "m_1", ProjectId: "p", Agent: "claude", Color: "red"},
		{PersonaId: "p_none", Name: "A", MachineId: "m_1", ProjectId: "p", Agent: "claude"},
	} {
		if _, err := o.SavePersona(device, &relayv1.SavePersonaRequest{Persona: bad}); err == nil {
			t.Fatalf("saved %v", bad)
		}
	}
	saved, err := o.SavePersona(device, &relayv1.SavePersonaRequest{Persona: &identityv1.Persona{Name: " Reviewer ", Icon: "robot", Color: "#112233",
		MachineId: "m_1", ProjectId: "demo", Agent: "codex", Model: "gpt", Effort: "high", SessionId: "s_forged"}})
	if err != nil {
		t.Fatal(err)
	}
	p := saved.Persona
	if p.PersonaId == "" || p.Name != "Reviewer" || p.SessionId != "" || p.UpdatedAt == nil {
		t.Fatalf("saved: %v", p)
	}
	if _, err := o.BindPersonaSession(device, &relayv1.BindPersonaSessionRequest{PersonaId: p.PersonaId, SessionId: "s_m1_a"}); err != nil {
		t.Fatal(err)
	}
	// A new name and colour keep the session; a session_id sent is ignored.
	p.Name, p.Color, p.SessionId = "Code reviewer", "#445566", "s_other"
	if res, err := o.SavePersona(device, &relayv1.SavePersonaRequest{Persona: p}); err != nil || res.Persona.SessionId != "s_m1_a" {
		t.Fatalf("renamed: %v %v", res, err)
	}
	// Each preset forgets it.
	for _, change := range []func(*identityv1.Persona){
		func(p *identityv1.Persona) { p.MachineId = "m_2" },
		func(p *identityv1.Persona) { p.ProjectId = "other" },
		func(p *identityv1.Persona) { p.Agent = "claude" },
		func(p *identityv1.Persona) { p.Model = "gpt-mini" },
		func(p *identityv1.Persona) { p.Effort = "low" },
		func(p *identityv1.Persona) { p.Mode = "plan" },
	} {
		o.BindPersonaSession(device, &relayv1.BindPersonaSessionRequest{PersonaId: p.PersonaId, SessionId: "s_m1_a"})
		change(p)
		if res, err := o.SavePersona(device, &relayv1.SavePersonaRequest{Persona: p}); err != nil || res.Persona.SessionId != "" {
			t.Fatalf("changed presets kept the session: %v %v", res, err)
		}
	}
	second, _ := o.SavePersona(device, &relayv1.SavePersonaRequest{Persona: &identityv1.Persona{Name: "B", MachineId: "m_1", ProjectId: "demo", Agent: "claude"}})
	list, err := o.ListPersonas(device, &relayv1.ListPersonasRequest{})
	if err != nil || len(list.Personas) != 2 || list.Personas[0].PersonaId != p.PersonaId || list.Personas[1].PersonaId != second.Persona.PersonaId {
		t.Fatalf("listed in the order added: %v %v", list, err)
	}
	if _, err := o.DeletePersona(device, &relayv1.DeletePersonaRequest{PersonaId: p.PersonaId}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.BindPersonaSession(device, &relayv1.BindPersonaSessionRequest{PersonaId: p.PersonaId, SessionId: "s"}); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("bound a deleted contact: %v", err)
	}
	if err := st.EraseOwner("o_x", "e1", &identityv1.Signed{}, &identityv1.RootChain{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if left, _ := st.Personas("o_x"); len(left) != 0 {
		t.Fatalf("kept after erasing: %v", left)
	}
}
