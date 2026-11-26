package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/samir2015dzh/aide/internal/flock"
)

// A background update is pending until its new version connects to its
// relay (工单 #162). One that has not within verifyWithin of its first start
// is undone: the programs it replaced go back in place, aided exits, and its
// service starts them. The version it went back from is skipped until a
// newer one is out, and is a problem on the owner's screens until then.

// verifyWithin is how long a background update's new version has to connect.
// Ten times the minute `aided update` waits (command.go), which only waits
// for the new version to answer: this also covers opening and migrating a
// large store and the relay link's retries (at most 10 s apart, machine's
// link.go). A version that cannot come up leaves the machine offline about
// this long. A variable for tests.
var verifyWithin = 10 * time.Minute

// Starting is for aided's service as it starts, before anything that could
// fail: it notes the first start of a pending update's version, and when
// that was verifyWithin ago, undoes the update and returns true. A version
// that fails as it starts, again and again, is undone at its first start
// after that.
func Starting(dataDir, current string, log *slog.Logger) bool {
	s := Load(dataDir)
	switch {
	case s.Pending && s.To != current: // updated or installed since by other means
		Edit(dataDir, func(s *State) { s.Pending = false })
	case s.Pending && s.Started.IsZero():
		Edit(dataDir, func(s *State) { s.Started = time.Now() })
	case s.Pending && time.Since(s.Started) >= verifyWithin:
		return rollBack(dataDir, s, log)
	}
	if s.Failed != "" && !newer(s.Failed, current) {
		Edit(dataDir, func(s *State) { s.Failed = "" })
	}
	return false
}

// Confirm is for aided's service once it runs, after Starting: a pending
// update's version that is connected is no longer pending. When
// verifyWithin passes first, Confirm undoes the update and returns true.
func Confirm(ctx context.Context, dataDir, current string, connected func() bool, log *slog.Logger) bool {
	s := Load(dataDir)
	if !s.Pending || s.To != current {
		return false
	}
	if s.Started.IsZero() {
		s.Started = time.Now()
	}
	deadline := time.After(time.Until(s.Started.Add(verifyWithin)))
	tick := time.NewTicker(min(time.Second, verifyWithin/20))
	defer tick.Stop()
	for !connected() {
		select {
		case <-ctx.Done():
			return false
		case <-deadline:
			return rollBack(dataDir, s, log)
		case <-tick.C:
		}
	}
	Edit(dataDir, func(s *State) { s.Pending = false })
	log.Info("update confirmed: connected to the relay", "version", current)
	return false
}

// Failed is the version a background update went back from, while current
// is older: "" for none.
func Failed(dataDir, current string) string {
	if f := Load(dataDir).Failed; f != "" && newer(f, current) {
		return f
	}
	return ""
}

// rollBack puts back the programs s's update replaced and keeps that it
// failed. When it cannot, the update stays: it is no longer pending.
func rollBack(dataDir string, s State, log *slog.Logger) bool {
	dir, err := Dir()
	if err == nil {
		err = restore(dataDir, dir, s.From)
	}
	if err != nil {
		log.Error("update did not connect to the relay, and could not go back", "from", s.From, "to", s.To, "err", err)
		Edit(dataDir, func(s *State) { s.Pending = false })
		return false
	}
	Edit(dataDir, func(st *State) { st.Pending, st.Failed = false, s.To })
	log.Warn("update did not connect to the relay in time: went back; it is skipped until a newer one", "failed", s.To, "back_to", s.From, "within", verifyWithin)
	return true
}

// restore puts back in dir each program's previous version, the one that
// says version: .<name>.previous, or on Windows one moved beside it with a
// number (Install). The running program is moved aside on Windows, which
// can't replace it. Nothing changes unless each program has one.
func restore(dataDir, dir, version string) error {
	lock, err := flock.Acquire(filepath.Join(dataDir, "update"))
	if err != nil {
		return errors.New("another update is running")
	}
	defer lock.Release()
	back := map[string]string{}
	var names []string
	for _, name := range Programs {
		if !exists(filepath.Join(dir, name)) {
			continue
		}
		prev := previousPath(dir, name)
		older, _ := filepath.Glob(prev + ".*")
		for _, p := range append([]string{prev}, older...) {
			if v, err := versionOf(context.Background(), p); err == nil && v == version {
				back[name] = p
				break
			}
		}
		if back[name] == "" {
			return fmt.Errorf("no previous %s that is %s", name, version)
		}
		names = append(names, name)
	}
	if names == nil {
		return fmt.Errorf("no Aide programs in %s", dir)
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		if runtime.GOOS == "windows" {
			aside := sibling(dir, name, "failed")
			os.Remove(aside) // unless it runs
			if err := os.Rename(path, aside); err != nil {
				if err := os.Rename(path, fmt.Sprintf("%s.%d", aside, time.Now().UnixNano())); err != nil {
					return err
				}
			}
		}
		if err := os.Rename(back[name], path); err != nil {
			return err
		}
	}
	return placeLegacy(dir, names)
}
