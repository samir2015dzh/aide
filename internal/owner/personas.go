package owner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"connectrpc.com/connect"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/gen/go/aide/relay/v1/relayv1connect"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
)

// Contacts (docs/personas.md): names the owner hands work to, kept on the
// owner's server so that every device has the same. Talking to one goes
// on in the session it talks in, or starts one with its presets.

// personaControl is the relay, for a device of the owner.
func (o *Owner) personaControl(ctx context.Context) (relayv1connect.OwnerControlServiceClient, error) {
	if err := o.requireInit(); err != nil {
		return nil, err
	}
	if o.machineOnly() {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("this computer is only a machine: keep contacts on one of your devices"))
	}
	c, _, err := o.control(ctx)
	return c, err
}

// ContactsUnavailable starts the message of the error a contacts call
// answers when the owner's server has no contacts to keep: a relay older
// than contacts (it answers 404, which the client reads as unimplemented)
// or one without their store. The apps match it to say so in words, never
// the raw "404 Not Found".
const ContactsUnavailable = "contacts_unavailable"

// contactsErr is a relay's answer to a contacts call, with "the server has
// none" said the same way whichever relay said it.
func contactsErr(err error) error {
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		return err
	}
	return connect.NewError(connect.CodeUnimplemented, fmt.Errorf("%s: the server does not keep contacts yet; update it (%v)", ContactsUnavailable, err))
}

func (l *localService) ListPersonas(ctx context.Context, _ *localv1.ListPersonasRequest) (*localv1.ListPersonasResponse, error) {
	c, err := l.o().personaControl(ctx)
	if err != nil {
		return nil, err
	}
	res, err := c.ListPersonas(ctx, &relayv1.ListPersonasRequest{})
	if err != nil {
		return nil, contactsErr(err)
	}
	return &localv1.ListPersonasResponse{Personas: res.Personas}, nil
}

func (l *localService) SavePersona(ctx context.Context, req *localv1.SavePersonaRequest) (*localv1.SavePersonaResponse, error) {
	c, err := l.o().personaControl(ctx)
	if err != nil {
		return nil, err
	}
	res, err := c.SavePersona(ctx, &relayv1.SavePersonaRequest{Persona: req.Persona})
	if err != nil {
		return nil, contactsErr(err)
	}
	return &localv1.SavePersonaResponse{Persona: res.Persona}, nil
}

func (l *localService) DeletePersona(ctx context.Context, req *localv1.DeletePersonaRequest) (*localv1.DeletePersonaResponse, error) {
	c, err := l.o().personaControl(ctx)
	if err != nil {
		return nil, err
	}
	_, err = c.DeletePersona(ctx, &relayv1.DeletePersonaRequest{PersonaId: req.PersonaId})
	if err != nil {
		return nil, contactsErr(err)
	}
	return &localv1.DeletePersonaResponse{}, nil
}

func (l *localService) TalkToPersona(ctx context.Context, req *localv1.TalkToPersonaRequest) (*localv1.TalkToPersonaResponse, error) {
	return l.o().TalkToPersona(ctx, req)
}

// TalkToPersona sends a message to a contact (docs/personas.md §3).
func (o *Owner) TalkToPersona(ctx context.Context, req *localv1.TalkToPersonaRequest) (*localv1.TalkToPersonaResponse, error) {
	c, err := o.personaControl(ctx)
	if err != nil {
		return nil, err
	}
	list, err := c.ListPersonas(ctx, &relayv1.ListPersonasRequest{})
	if err != nil {
		return nil, contactsErr(err)
	}
	var p *identityv1.Persona
	for _, x := range list.Personas {
		if x.PersonaId == req.PersonaId {
			p = x
		}
	}
	if p == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such contact"))
	}
	reason := "first"
	if p.SessionId != "" {
		s, err := o.getSession(ctx, p.SessionId)
		switch {
		case connect.CodeOf(err) == connect.CodeNotFound:
			reason = "gone"
		case err != nil:
			// Its machine can't be reached: a new session there could not
			// start either, and one elsewhere would split the conversation.
			return nil, connect.NewError(connect.CodeUnavailable, fmt.Errorf("%s's session can't be reached now (%w): nothing was sent", p.Name, err))
		case s.MachineId != p.MachineId || s.ProjectId != p.ProjectId:
			reason = "changed"
		case s.Status == sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED:
			reason = "archived"
		default:
			s, err := o.SendMessage(ctx, p.SessionId, req.Text, req.Attachments)
			if err != nil {
				return nil, err
			}
			return &localv1.TalkToPersonaResponse{Session: s, Persona: p}, nil
		}
	}
	s, err := o.StartSession(ctx, p.MachineId, &sessionv1.StartSessionRequest{
		IdempotencyKey: req.IdempotencyKey,
		ProjectId:      p.ProjectId,
		Agent:          p.Agent,
		Model:          p.Model,
		Effort:         p.Effort,
		Mode:           p.Mode,
		Prompt:         req.Text,
		Attachments:    req.Attachments,
	})
	if err != nil {
		return nil, err
	}
	if b, err := c.BindPersonaSession(ctx, &relayv1.BindPersonaSessionRequest{PersonaId: p.PersonaId, SessionId: s.SessionId}); err != nil {
		// The session runs; the next message starts another.
		slog.Warn("contact: keeping its session", "persona", p.PersonaId, "session", s.SessionId, "err", err)
	} else {
		p = b.Persona
	}
	return &localv1.TalkToPersonaResponse{Session: s, Started: true, Reason: reason, Persona: p}, nil
}

// getSession is one session from its machine.
func (o *Owner) getSession(ctx context.Context, id string) (*sessionv1.Session, error) {
	if l := o.localSession(id); l != nil {
		return l.Get(id)
	}
	n, err := o.sessionMachine(ctx, id)
	if err != nil {
		return nil, err
	}
	return n.Session.GetSession(ctx, &sessionv1.GetSessionRequest{SessionId: id})
}
