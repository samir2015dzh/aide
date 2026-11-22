package acp

import (
	"context"

	"github.com/samir2015dzh/aide/internal/provider/acp/dshnative"
	"github.com/samir2015dzh/aide/internal/shellenv"
)

// Archive archives (or unarchives) a DSH session Aide runs in DSH Desktop's
// sidebar too (工单 #67); other ACP agents keep no such list.
func (p *Provider) Archive(_ context.Context, ref string, archived bool) error {
	if p.Agent.Name != "deepseek" {
		return nil
	}
	home, err := dshnative.Home(shellenv.Environ())
	if err != nil {
		return err
	}
	return dshnative.SetArchived(home, ref, archived)
}

// Archived is whether DSH Desktop's sidebar has each of these sessions
// archived; nil for another agent, or without DSH Desktop's state.
func (p *Provider) Archived(refs []string) map[string]bool {
	if p.Agent.Name != "deepseek" {
		return nil
	}
	home, err := dshnative.Home(shellenv.Environ())
	if err != nil {
		return nil
	}
	ids, ok := dshnative.ArchivedState(home)
	if !ok {
		return nil
	}
	out := make(map[string]bool, len(refs))
	for _, r := range refs {
		out[r] = ids[r]
	}
	return out
}
