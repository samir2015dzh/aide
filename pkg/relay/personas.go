package relay

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// PersonaStore keeps the owner's contacts (docs/personas.md). A Backend
// that has it keeps them; without it the calls answer Unimplemented.
type PersonaStore interface {
	Personas(ownerID string) ([]*identityv1.Persona, error)
	// Persona is nil for none.
	Persona(ownerID, id string) (*identityv1.Persona, error)
	PutPersona(ownerID string, p *identityv1.Persona) error
	DeletePersona(ownerID, id string) error
}

var colorRE = regexp.MustCompile(`^(#[0-9a-fA-F]{6})?$`)

// ownerPersonas is the contacts, for a trusted device of the owner.
func (o *ownerControl) ownerPersonas(ctx context.Context) (PersonaStore, string, error) {
	p := peerFrom(ctx)
	if p.deviceCert == nil {
		return nil, "", errf(connect.CodePermissionDenied, "only one of the owner's devices keeps contacts")
	}
	ps, ok := o.s.store.(PersonaStore)
	if !ok {
		return nil, "", errf(connect.CodeUnimplemented, "this relay keeps no contacts")
	}
	return ps, p.t.ownerID, nil
}

func (o *ownerControl) ListPersonas(ctx context.Context, _ *relayv1.ListPersonasRequest) (*relayv1.ListPersonasResponse, error) {
	ps, owner, err := o.ownerPersonas(ctx)
	if err != nil {
		return nil, err
	}
	list, err := ps.Personas(owner)
	if err != nil {
		return nil, err
	}
	return &relayv1.ListPersonasResponse{Personas: list}, nil
}

// presetsOf is what a contact's sessions start with: a change to it
// forgets the contact's session.
func presetsOf(p *identityv1.Persona) [6]string {
	return [6]string{p.MachineId, p.ProjectId, p.Agent, p.Model, p.Effort, p.Mode}
}

func (o *ownerControl) SavePersona(ctx context.Context, req *relayv1.SavePersonaRequest) (*relayv1.SavePersonaResponse, error) {
	ps, owner, err := o.ownerPersonas(ctx)
	if err != nil {
		return nil, err
	}
	p := proto.Clone(req.GetPersona()).(*identityv1.Persona)
	if p == nil {
		return nil, errf(connect.CodeInvalidArgument, "no contact")
	}
	p.Name = strings.TrimSpace(p.Name)
	switch {
	case p.Name == "" || utf8.RuneCountInString(p.Name) > 64:
		return nil, errf(connect.CodeInvalidArgument, "a contact's name is 1 to 64 characters")
	case p.MachineId == "" || p.ProjectId == "" || p.Agent == "":
		return nil, errf(connect.CodeInvalidArgument, "a contact needs a machine, a project and a coding agent")
	case len(p.Icon) > 32 || !colorRE.MatchString(p.Color):
		return nil, errf(connect.CodeInvalidArgument, "bad icon or colour")
	}
	p.SessionId = ""
	if p.PersonaId == "" {
		p.PersonaId = "p_" + strings.ToLower(identity.RandomCode(16))
	} else {
		old, err := ps.Persona(owner, p.PersonaId)
		if err != nil {
			return nil, err
		}
		if old == nil {
			return nil, errf(connect.CodeNotFound, "no such contact")
		}
		if presetsOf(old) == presetsOf(p) {
			p.SessionId = old.SessionId
		}
	}
	p.UpdatedAt = timestamppb.New(time.Now())
	if err := ps.PutPersona(owner, p); err != nil {
		return nil, err
	}
	return &relayv1.SavePersonaResponse{Persona: p}, nil
}

func (o *ownerControl) DeletePersona(ctx context.Context, req *relayv1.DeletePersonaRequest) (*relayv1.DeletePersonaResponse, error) {
	ps, owner, err := o.ownerPersonas(ctx)
	if err != nil {
		return nil, err
	}
	return &relayv1.DeletePersonaResponse{}, ps.DeletePersona(owner, req.PersonaId)
}

func (o *ownerControl) BindPersonaSession(ctx context.Context, req *relayv1.BindPersonaSessionRequest) (*relayv1.BindPersonaSessionResponse, error) {
	ps, owner, err := o.ownerPersonas(ctx)
	if err != nil {
		return nil, err
	}
	p, err := ps.Persona(owner, req.PersonaId)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errf(connect.CodeNotFound, "no such contact")
	}
	p.SessionId = req.SessionId
	p.UpdatedAt = timestamppb.New(time.Now())
	if err := ps.PutPersona(owner, p); err != nil {
		return nil, err
	}
	return &relayv1.BindPersonaSessionResponse{Persona: p}, nil
}

// The contacts of a self-hosted relay's one owner.

var _ PersonaStore = (*Store)(nil)

func (s *Store) Personas(string) ([]*identityv1.Persona, error) {
	rows, err := s.db.Query(`SELECT persona FROM personas ORDER BY created_at, persona_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*identityv1.Persona
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		p := &identityv1.Persona{}
		if err := proto.Unmarshal(b, p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Persona(_ string, id string) (*identityv1.Persona, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT persona FROM personas WHERE persona_id=?`, id).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p := &identityv1.Persona{}
	return p, proto.Unmarshal(b, p)
}

func (s *Store) PutPersona(_ string, p *identityv1.Persona) error {
	b, err := proto.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO personas(persona_id, persona, created_at) VALUES(?,?,?)
ON CONFLICT(persona_id) DO UPDATE SET persona=excluded.persona`, p.PersonaId, b, time.Now().UnixNano())
	return err
}

func (s *Store) DeletePersona(_ string, id string) error {
	_, err := s.db.Exec(`DELETE FROM personas WHERE persona_id=?`, id)
	return err
}
