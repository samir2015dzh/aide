// Package boundary checks a message against the owner's information
// boundary (docs/agent-messages.md §15.5): rules the owner signed, each a
// category, patterns and the kinds of recipient it is for. Senders refuse
// a message that matches one for its recipient (refused: boundary, naming
// the category); recipients check again before a model reads it, and hold
// one that matches for the owner. What matched is never shown.
package boundary

import (
	"regexp"
	"slices"
	"strings"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// Checker is a compiled boundary. The zero value, and nil, hold nothing.
type Checker struct {
	rules []rule
}

type rule struct {
	category string
	res      []*regexp.Regexp
	kinds    []string
}

// Compile compiles the rules of a verified policy (a pattern that doesn't
// compile is left out; VerifyBoundary refuses such a policy anyway).
func Compile(p *identityv1.BoundaryPolicy) *Checker {
	c := &Checker{}
	for _, r := range p.GetRules() {
		cr := rule{category: r.Category}
		for _, k := range r.RecipientKinds {
			cr.kinds = append(cr.kinds, strings.ToLower(strings.TrimSpace(k)))
		}
		for _, pat := range r.Patterns {
			if re, err := regexp.Compile(pat); err == nil {
				cr.res = append(cr.res, re)
			}
		}
		if len(cr.res) > 0 {
			c.rules = append(c.rules, cr)
		}
	}
	return c
}

// FromContacts is the boundary the post office handed with the contacts,
// verified against the owner's trust; nil when there is none, or it does
// not verify (err says why).
func FromContacts(t *identity.Trust, b *relayv1.Boundary) (*Checker, error) {
	if b.GetPolicy() == nil {
		return nil, nil
	}
	p, err := t.VerifyBoundary(b.Policy, b.DeviceCert)
	if err != nil {
		return nil, err
	}
	return Compile(p), nil
}

// Check says the category of the first rule text matches for a recipient
// of kind ("claude", "codex", "deepseek", …; "" when the sender can't tell,
// which only rules for every recipient hold for), or "" when none does.
func (c *Checker) Check(text, kind string) string {
	if c == nil {
		return ""
	}
	kind = strings.ToLower(kind)
	for _, r := range c.rules {
		if len(r.kinds) > 0 && (kind == "" || !slices.Contains(r.kinds, kind)) {
			continue
		}
		for _, re := range r.res {
			if re.MatchString(text) {
				return r.category
			}
		}
	}
	return ""
}

// Empty reports whether it holds nothing.
func (c *Checker) Empty() bool { return c == nil || len(c.rules) == 0 }
