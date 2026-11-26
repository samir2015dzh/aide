package machine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/samir2015dzh/aide/gen/go/aide/identity/v1"
	sessionv1 "github.com/samir2015dzh/aide/gen/go/aide/session/v1"
	"github.com/samir2015dzh/aide/internal/provider"
)

const (
	// checkpointRefRoot stores per-turn workspace checkpoints in a hidden git
	// namespace (refs/aide/checkpoints/<session_id>/turn-<N>), completely
	// separate from user branches (refs/heads/*). Each ref points at a tag
	// object for the turn's snapshot commit, whose tagger date is when the
	// checkpoint was written: a turn that changed nothing reuses an older
	// commit, so its committer date says nothing of the checkpoint's age.
	checkpointRefRoot = "refs/aide/checkpoints/"

	// DefaultCheckpointMaxCount is the default maximum number of turn
	// checkpoints retained per session.
	DefaultCheckpointMaxCount uint32 = 20

	// DefaultCheckpointMaxAgeDays is the default maximum age in days before
	// older turn checkpoints are pruned.
	DefaultCheckpointMaxAgeDays uint32 = 14

	// CheckpointUntrackedPolicy documents how untracked files are handled:
	// non-gitignored untracked files are staged and captured in each turn
	// checkpoint; gitignored files are left untouched.
	CheckpointUntrackedPolicy = "included_non_ignored"
)

// CheckpointRetention controls automatic pruning of hidden checkpoint refs.
type CheckpointRetention struct {
	MaxCount   uint32
	MaxAgeDays uint32
}

func defaultCheckpointRetention() CheckpointRetention {
	return CheckpointRetention{
		MaxCount:   DefaultCheckpointMaxCount,
		MaxAgeDays: DefaultCheckpointMaxAgeDays,
	}
}

func checkpointSessionPrefix(sessionID string) string {
	return checkpointRefRoot + sessionID + "/"
}

func checkpointRefName(sessionID string, turn uint32) string {
	return fmt.Sprintf("%s%s/turn-%d", checkpointRefRoot, sessionID, turn)
}

func parseCheckpointTurn(sessionID, ref string) (uint32, bool) {
	prefix := checkpointSessionPrefix(sessionID) + "turn-"
	raw, ok := strings.CutPrefix(ref, prefix)
	if !ok || raw == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

type gitCheckpoint struct {
	Turn      uint32
	Ref       string
	Commit    string
	CreatedAt time.Time
}

// WriteCheckpoint records a hidden git ref for a turn's snapshot commit,
// written now, without touching any user branch.
func (w *Workspace) WriteCheckpoint(ctx context.Context, repo, sessionID string, turn uint32, commit string) error {
	return w.writeCheckpointAt(ctx, repo, sessionID, turn, commit, time.Now())
}

func (w *Workspace) writeCheckpointAt(ctx context.Context, repo, sessionID string, turn uint32, commit string, at time.Time) error {
	if w.none || repo == "" || commit == "" || turn == 0 {
		return nil
	}
	full, err := git(ctx, repo, "rev-parse", "--verify", "--end-of-options", commit+"^{commit}")
	if err != nil {
		return err
	}
	tag := fmt.Sprintf("object %s\ntype commit\ntag aide-checkpoint-turn-%d\ntagger Aide <aide@localhost> %d +0000\n\nAide checkpoint: session %s, turn %d\n",
		strings.TrimSpace(full), turn, at.Unix(), sessionID, turn)
	f, err := os.CreateTemp("", "aide-checkpoint-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(tag)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	obj, err := git(ctx, repo, "hash-object", "-t", "tag", "-w", f.Name())
	if err != nil {
		return err
	}
	_, err = git(ctx, repo, "update-ref", checkpointRefName(sessionID, turn), strings.TrimSpace(obj))
	return err
}

// ListCheckpoints returns all hidden checkpoint refs for sessionID sorted by
// turn ascending.
func (w *Workspace) ListCheckpoints(ctx context.Context, repo, sessionID string) ([]gitCheckpoint, error) {
	if w.none || repo == "" {
		return nil, nil
	}
	prefix := checkpointSessionPrefix(sessionID)
	out, err := git(ctx, repo, "for-each-ref", "--format=%(refname)\t%(objecttype)\t%(objectname)\t%(*objectname)\t%(taggerdate:unix)", prefix)
	if err != nil {
		return nil, err
	}
	var cps []gitCheckpoint
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t") // a commit's has no peeled object or tagger date (trimmed)
		if len(parts) < 3 {
			continue
		}
		parts = append(parts, "", "")
		turn, ok := parseCheckpointTurn(sessionID, parts[0])
		if !ok {
			continue
		}
		// A tag: its commit, and when it was written. A ref straight to a
		// commit (written before checkpoints were tagged): its write time
		// is unknown (zero), so it is never pruned for its age.
		commit := strings.TrimSpace(parts[2])
		var createdAt time.Time
		if strings.TrimSpace(parts[1]) == "tag" {
			commit = strings.TrimSpace(parts[3])
			if sec, err := strconv.ParseInt(strings.TrimSpace(parts[4]), 10, 64); err == nil && sec > 0 {
				createdAt = time.Unix(sec, 0).UTC()
			}
		}
		cps = append(cps, gitCheckpoint{
			Turn:      turn,
			Ref:       parts[0],
			Commit:    commit,
			CreatedAt: createdAt,
		})
	}
	slices.SortFunc(cps, func(a, b gitCheckpoint) int {
		if a.Turn < b.Turn {
			return -1
		}
		if a.Turn > b.Turn {
			return 1
		}
		return 0
	})
	return cps, nil
}

// PruneCheckpoints deletes hidden checkpoint refs that exceed retention limits.
func (w *Workspace) PruneCheckpoints(ctx context.Context, repo, sessionID string, ret CheckpointRetention, now time.Time) (int, error) {
	if w.none || repo == "" {
		return 0, nil
	}
	cps, err := w.ListCheckpoints(ctx, repo, sessionID)
	if err != nil || len(cps) == 0 {
		return 0, err
	}
	var cutoff time.Time
	if ret.MaxAgeDays > 0 {
		cutoff = now.UTC().Add(-time.Duration(ret.MaxAgeDays) * 24 * time.Hour)
	}
	var drop []gitCheckpoint
	var kept []gitCheckpoint
	for _, cp := range cps {
		if !cutoff.IsZero() && !cp.CreatedAt.IsZero() && cp.CreatedAt.Before(cutoff) {
			drop = append(drop, cp)
		} else {
			kept = append(kept, cp)
		}
	}
	if ret.MaxCount > 0 && len(kept) > int(ret.MaxCount) {
		overflow := len(kept) - int(ret.MaxCount)
		drop = append(drop, kept[:overflow]...)
	}
	removed := 0
	var errs []error
	for _, cp := range drop {
		if _, err := git(ctx, repo, "update-ref", "-d", cp.Ref); err != nil {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}

// DropCheckpointsAfter removes hidden checkpoint refs with turn > targetTurn
// after a rollback discards later turns.
func (w *Workspace) DropCheckpointsAfter(ctx context.Context, repo, sessionID string, targetTurn uint32) error {
	if w.none || repo == "" {
		return nil
	}
	cps, err := w.ListCheckpoints(ctx, repo, sessionID)
	if err != nil {
		return err
	}
	var errs []error
	for _, cp := range cps {
		if cp.Turn > targetTurn {
			if _, err := git(ctx, repo, "update-ref", "-d", cp.Ref); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// ClearCheckpoints removes hidden checkpoint refs for sessionID, optionally
// retaining the keepLatest most recent checkpoints (0 removes all).
func (w *Workspace) ClearCheckpoints(ctx context.Context, repo, sessionID string, keepLatest uint32) (removed, remaining uint32, err error) {
	if w.none || repo == "" {
		return 0, 0, nil
	}
	cps, err := w.ListCheckpoints(ctx, repo, sessionID)
	if err != nil {
		return 0, 0, err
	}
	if keepLatest >= uint32(len(cps)) {
		return 0, uint32(len(cps)), nil
	}
	cut := len(cps) - int(keepLatest)
	for _, cp := range cps[:cut] {
		if _, delErr := git(ctx, repo, "update-ref", "-d", cp.Ref); delErr != nil {
			return removed, uint32(len(cps)) - removed, delErr
		}
		removed++
	}
	return removed, uint32(len(cps)) - removed, nil
}

func (s *Sessions) checkpointRetention() CheckpointRetention {
	r := s.checkpoints
	if r.MaxCount == 0 && r.MaxAgeDays == 0 {
		return defaultCheckpointRetention()
	}
	return r
}

func (s *Sessions) checkpointPolicyProto() *sessionv1.CheckpointPolicy {
	r := s.checkpointRetention()
	return &sessionv1.CheckpointPolicy{
		MaxCount:        r.MaxCount,
		MaxAgeDays:      r.MaxAgeDays,
		UntrackedPolicy: CheckpointUntrackedPolicy,
	}
}

// canRollbackConversation reports whether the session's provider supports
// paired conversation rollback so files and conversation never diverge.
func (s *Sessions) canRollbackConversation(r *sessionRow) (bool, string) {
	p := s.provider(r.Agent)
	if p == nil {
		return false, fmt.Sprintf("provider %q is not available on this machine; refusing to roll back files without conversation rollback", r.Agent)
	}
	if rb, ok := p.(provider.ConversationRollbacker); ok {
		return rb.CanRollbackConversation()
	}
	return true, ""
}

// checkpointStatus explains whether git hidden-ref checkpoints are enabled for
// this session, and gives an explicit reason when disabled (never silent).
func (s *Sessions) checkpointStatus(ctx context.Context, r *sessionRow, proj *Project) (enabled bool, disabledReason string) {
	if s.ws.none {
		return false, "workspace snapshots are disabled in this daemon configuration"
	}
	if r.Workspace == identityv1.Workspace_WORKSPACE_IN_PLACE {
		if proj != nil {
			if _, err := ValidateRepo(ctx, proj.Path); err == nil {
				return false, "session runs in place (in-place mode): Aide does not modify the project's working tree or git refs, so per-turn checkpoints and rollback are disabled"
			}
		}
		return false, "non-git project directory: git hidden-ref checkpoints are disabled (not silently skipped); open a git repository root to enable per-turn checkpoints and rollback"
	}
	if proj == nil {
		return false, fmt.Sprintf("project %q no longer exists on this machine", r.ProjectID)
	}
	return true, ""
}

// ListCheckpoints returns the per-turn hidden git ref checkpoints, capability
// status, non-git/in-place explanation, and retention policy for a session.
func (s *Sessions) ListCheckpoints(ctx context.Context, p *Principal, id string) (*sessionv1.ListCheckpointsResponse, error) {
	_, r, err := s.find(p, id, false)
	if err != nil {
		return nil, err
	}
	proj, _ := s.st.Project(r.ProjectID)
	enabled, disabledReason := s.checkpointStatus(ctx, r, proj)
	canRollback, refusedReason := s.canRollbackConversation(r)
	if !enabled { // why there are no checkpoints comes first (in place, not git)
		canRollback, refusedReason = false, disabledReason
	}
	resp := &sessionv1.ListCheckpointsResponse{
		SessionId:             id,
		Enabled:               enabled,
		DisabledReason:        disabledReason,
		CanRollback:           canRollback,
		RollbackRefusedReason: refusedReason,
		Policy:                s.checkpointPolicyProto(),
	}
	if !enabled {
		return resp, nil
	}
	gitCPs, err := s.ws.ListCheckpoints(ctx, proj.Path, id)
	if err != nil {
		return nil, fmt.Errorf("list checkpoints: %w", err)
	}
	turnMeta := s.checkpointTurnMeta(id)
	prevCommit := r.BaseCommit
	for _, cp := range gitCPs {
		meta := turnMeta[cp.Turn]
		ts := cp.CreatedAt
		if ts.IsZero() && !meta.at.IsZero() {
			ts = meta.at
		}
		item := &sessionv1.Checkpoint{
			Turn:          cp.Turn,
			Ref:           cp.Ref,
			Commit:        cp.Commit,
			DiffStat:      meta.diffStat,
			Changed:       cp.Commit != "" && cp.Commit != prevCommit,
			PromptExcerpt: meta.prompt,
		}
		if !ts.IsZero() {
			item.CreatedAt = timestamppb.New(ts.UTC())
		}
		resp.Checkpoints = append(resp.Checkpoints, item)
		if cp.Commit != "" {
			prevCommit = cp.Commit
		}
	}
	return resp, nil
}

// ClearCheckpoints manually prunes hidden git ref checkpoints for a session.
func (s *Sessions) ClearCheckpoints(ctx context.Context, p *Principal, id string, keepLatest uint32) (*sessionv1.ClearCheckpointsResponse, error) {
	_, r, err := s.find(p, id, p.Kind != PrincipalOwner)
	if err != nil {
		return nil, err
	}
	proj, err := s.st.Project(r.ProjectID)
	if err != nil || proj == nil {
		return nil, cerr(connect.CodeFailedPrecondition, "project %q no longer exists", r.ProjectID)
	}
	enabled, disabledReason := s.checkpointStatus(ctx, r, proj)
	if !enabled {
		return nil, cerr(connect.CodeFailedPrecondition, "%s", disabledReason)
	}
	removed, remaining, err := s.ws.ClearCheckpoints(ctx, proj.Path, id, keepLatest)
	if err != nil {
		return nil, fmt.Errorf("clear checkpoints: %w", err)
	}
	s.audit.Log(AuditRecord{Kind: "session.checkpoints.clear", Subject: p.ID(), Session: id, Decision: "allow"})
	return &sessionv1.ClearCheckpointsResponse{Removed: removed, Remaining: remaining}, nil
}

type cpTurnMeta struct {
	prompt   string
	diffStat *sessionv1.DiffStat
	at       time.Time
}

// checkpointTurnMeta extracts the latest prompt excerpt and diff stat per turn
// from stored session events (ignoring turns discarded by an earlier rollback).
func (s *Sessions) checkpointTurnMeta(id string) map[uint32]cpTurnMeta {
	out := map[uint32]cpTurnMeta{}
	var all []*sessionv1.SessionEvent
	var before uint64
	for {
		evs, more, _, err := s.st.EventPage(id, 0, before, 500)
		if err != nil || len(evs) == 0 {
			break
		}
		all = append(evs, all...)
		if !more {
			break
		}
		before = evs[0].Seq
	}
	var currentTurn uint32
	for _, ev := range all {
		switch k := ev.Kind.(type) {
		case *sessionv1.SessionEvent_RolledBack:
			for t := range out {
				if t > k.RolledBack.Turn {
					delete(out, t)
				}
			}
			currentTurn = k.RolledBack.Turn
		case *sessionv1.SessionEvent_TurnStarted:
			currentTurn = k.TurnStarted.Turn
		case *sessionv1.SessionEvent_UserMessage:
			t := currentTurn
			if t == 0 {
				t = 1
			}
			m := out[t]
			if m.prompt == "" && k.UserMessage.Text != "" {
				m.prompt, _ = provider.Truncate(strings.TrimSpace(k.UserMessage.Text), 120)
				out[t] = m
			}
		case *sessionv1.SessionEvent_TurnCompleted:
			m := out[k.TurnCompleted.Turn]
			m.diffStat = k.TurnCompleted.DiffStat
			if ev.Ts != nil {
				m.at = ev.Ts.AsTime().UTC()
			}
			out[k.TurnCompleted.Turn] = m
		}
	}
	return out
}
