package owner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	relayv1 "github.com/samir2015dzh/aide/gen/go/aide/relay/v1"
	"github.com/samir2015dzh/aide/pkg/identity"
)

// The owner's information boundary (docs/agent-messages.md §15.5): what
// must not flow between their agents, to which kinds of recipient. The
// rules are the owner's own, written to boundary.json in this device's data
// directory (never in Aide's repository); this device signs them with its
// device key and publishes them, and every sender and recipient of the
// owner's messages checks against them. An absent file changes nothing;
// {"rules": []} clears the boundary.

// BoundaryFile is where this device reads the owner's rules.
func (o *Owner) BoundaryFile() string { return filepath.Join(o.cfg.DataDir, "boundary.json") }

type boundaryFile struct {
	Rules []struct {
		Category       string   `json:"category"`
		Patterns       []string `json:"patterns"`
		RecipientKinds []string `json:"recipient_kinds"`
	} `json:"rules"`
}

// syncBoundary publishes the rules in BoundaryFile when they changed since
// this device last published them; it keeps looking every two minutes.
func (o *Owner) syncBoundary() {
	o.boundaryOnce.Do(func() {
		go func() {
			for {
				time.Sleep(2 * time.Minute)
				o.publishBoundary()
			}
		}()
	})
	o.publishBoundary()
}

func (o *Owner) publishBoundary() {
	o.boundaryMu.Lock()
	defer o.boundaryMu.Unlock()
	b, err := os.ReadFile(o.BoundaryFile())
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		o.log.Warn("reading the information boundary", "err", err)
		return
	}
	sum := sha256.Sum256(b)
	if sum == o.boundarySent {
		return
	}
	var f boundaryFile
	if err := json.Unmarshal(b, &f); err != nil {
		o.log.Warn("the information boundary is not JSON", "file", o.BoundaryFile(), "err", err)
		return
	}
	o.mu.Lock()
	dev, ownerID := o.device, o.ownerID
	o.mu.Unlock()
	if dev == nil {
		return
	}
	p := &identityv1.BoundaryPolicy{OwnerId: ownerID, IssuedAt: timestamppb.Now()}
	for _, r := range f.Rules {
		p.Rules = append(p.Rules, &identityv1.BoundaryRule{Category: r.Category, Patterns: r.Patterns, RecipientKinds: r.RecipientKinds})
	}
	if err := identity.CheckBoundaryRules(p.Rules); err != nil {
		o.log.Warn("the information boundary is not published", "file", o.BoundaryFile(), "err", err)
		return
	}
	signed, err := identity.Sign(p, dev)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctl, _, err := o.control(ctx)
	if err != nil {
		return
	}
	if _, err := ctl.PublishBoundaryPolicy(ctx, &relayv1.PublishBoundaryPolicyRequest{Boundary: &relayv1.Boundary{Policy: signed}}); err != nil {
		o.log.Warn("publishing the information boundary", "err", err)
		return
	}
	o.boundarySent = sum
	o.log.Info("published the information boundary", "rules", len(p.Rules))
}
