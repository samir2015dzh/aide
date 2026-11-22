package machine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

// Archived on both sides (工单 #67 rule 6): a session Aide runs is also a
// conversation in its agent's own app (DSH Desktop's sidebar, the Claude
// desktop app's list). Archiving or unarchiving it in Aide does the same
// there (archiveInAgent, as for Codex). The other way, SyncArchives looks at
// those apps every so often: a conversation the owner archived or
// unarchived there since the last look is archived or unarchived in Aide.
// What each app said at the last look is kept (archive-sync.json), so a
// change made while this machine was off is still seen; the first look
// only learns it.

const archiveSyncEvery = 30 * time.Second

type archiveSync struct {
	mu   sync.Mutex
	seen map[string]bool // provider ref → archived in its app, at the last look
	read bool
}

func (s *Sessions) archiveSeenPath() string { return filepath.Join(s.dataDir, "archive-sync.json") }

// SyncArchives follows what the owner archived or unarchived in the agents' own apps.
func (s *Sessions) SyncArchives(ctx context.Context) {
	rows, err := s.st.Sessions(`provider_ref != ''`)
	if err != nil {
		return
	}
	byAgent := map[string][]*sessionRow{}
	for _, r := range rows {
		byAgent[r.Agent] = append(byAgent[r.Agent], r)
	}
	a := &s.archives
	a.mu.Lock()
	if !a.read {
		a.read = true
		if b, err := os.ReadFile(s.archiveSeenPath()); err == nil {
			json.Unmarshal(b, &a.seen)
		}
	}
	if a.seen == nil {
		a.seen = map[string]bool{}
	}
	a.mu.Unlock()
	owner := &Principal{Kind: PrincipalOwner}
	changed := false
	for agent, list := range byAgent {
		reader, ok := s.provider(agent).(provider.ArchiveReader)
		if !ok {
			continue
		}
		refs := make([]string, len(list))
		for i, r := range list {
			refs[i] = r.ProviderRef
		}
		there := reader.Archived(refs)
		for _, r := range list {
			now, known := there[r.ProviderRef]
			if !known {
				continue
			}
			a.mu.Lock()
			was, had := a.seen[r.ProviderRef]
			a.mu.Unlock()
			if had && was == now {
				continue
			}
			// As the session is now (its stored row may lag a live change).
			inAide := r.Status == sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED
			if cur, err := s.Get(owner, r.ID); err == nil {
				inAide = cur.Status == sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED
			}
			if had && now != inAide {
				var err error
				if now {
					_, err = s.Archive(ctx, owner, r.ID)
				} else {
					_, err = s.Unarchive(ctx, owner, r.ID)
				}
				if err != nil {
					// A turn in progress, say: tried again at the next look.
					s.log.Info("following an archive in the agent's app", "session", r.ID, "archived", now, "err", err)
					continue
				}
				s.log.Info("followed an archive in the agent's app", "session", r.ID, "archived", now)
			}
			a.mu.Lock()
			a.seen[r.ProviderRef] = now
			a.mu.Unlock()
			changed = true
		}
	}
	if changed {
		a.mu.Lock()
		b, _ := json.Marshal(a.seen)
		a.mu.Unlock()
		writeFile(s.archiveSeenPath(), b)
	}
}

// archiveWatch runs SyncArchives every so often until ctx is done.
func (n *Machine) archiveWatch(ctx context.Context) {
	t := time.NewTicker(archiveSyncEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.sessions.SyncArchives(ctx)
		}
	}
}
