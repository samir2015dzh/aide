package machine

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/fake"
)

// archivingApp is the fake agent with an app of its own that keeps whether each conversation is archived.
type archivingApp struct {
	fake.Provider
	mu  sync.Mutex
	app map[string]bool
}

func (a *archivingApp) Archive(_ context.Context, ref string, archived bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.app[ref] = archived
	return nil
}

func (a *archivingApp) Archived(refs []string) map[string]bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]bool{}
	for _, r := range refs {
		out[r] = a.app[r]
	}
	return out
}

func (a *archivingApp) set(ref string, v bool) {
	a.mu.Lock()
	a.app[ref] = v
	a.mu.Unlock()
}

var _ provider.ArchiveReader = (*archivingApp)(nil)

// TestArchiveSync (工单 #67 rule 6): archived in Aide is archived in the agent's own app; archived or unarchived
// there by the owner, Aide follows at its next look; the first look only learns; what was seen is kept.
func TestArchiveSync(t *testing.T) {
	app := &archivingApp{app: map[string]bool{}}
	h := newHarnessWith(t, Limits{}, map[string]provider.Provider{"fake": app})
	ctx := context.Background()
	owner := &Principal{Kind: PrincipalOwner}
	id := h.start("hello")
	h.wait(id, sessionv1.SessionStatus_SESSION_STATUS_IDLE)
	ref := ""
	for deadline := time.Now().Add(5 * time.Second); ref == "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		rows, _ := h.s.st.Sessions(`session_id = ?`, id)
		if len(rows) == 1 {
			ref = rows[0].ProviderRef
		}
	}
	if ref == "" {
		t.Fatal("no provider ref")
	}
	status := func() sessionv1.SessionStatus {
		s, err := h.s.Get(owner, id)
		if err != nil {
			t.Fatal(err)
		}
		return s.Status
	}

	h.s.SyncArchives(ctx) // the first look: learns, changes nothing
	if status() == sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED {
		t.Fatal("archived at the first look")
	}
	// Archived in Aide: archived in the app too, and the next look leaves it so.
	if _, err := h.s.Archive(ctx, owner, id); err != nil {
		t.Fatal(err)
	}
	if !app.Archived([]string{ref})[ref] {
		t.Fatal("not archived in the app")
	}
	h.s.SyncArchives(ctx)
	if status() != sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED {
		t.Fatalf("after a look: %v", status())
	}
	// Unarchived in the app by the owner: Aide follows.
	app.set(ref, false)
	h.s.SyncArchives(ctx)
	if status() == sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED {
		t.Fatal("not unarchived after the app")
	}
	// Archived in the app: Aide follows again.
	app.set(ref, true)
	h.s.SyncArchives(ctx)
	if status() != sessionv1.SessionStatus_SESSION_STATUS_ARCHIVED {
		t.Fatalf("not archived after the app: %v", status())
	}
	if _, err := os.Stat(h.s.archiveSeenPath()); err != nil {
		t.Fatal("what was seen is not kept")
	}
}
