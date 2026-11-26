package update

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

// updated is a machine the background updater has just updated from
// 0.1.0 to 0.2.0, within a short verifyWithin: its data directory and
// programs' folder.
func updated(t *testing.T) (data, dir string) {
	r := newRelease(t)
	r.publish("0.2.0")
	rel, _ := Latest(context.Background())
	dir = programsAt(t, "0.1.0")
	executable = func() (string, error) { return filepath.Join(dir, Programs[0]), nil }
	t.Cleanup(func() { executable = os.Executable })
	old := verifyWithin
	verifyWithin = 300 * time.Millisecond
	t.Cleanup(func() { verifyWithin = old })
	data = t.TempDir()
	if !installed(context.Background(), data, "0.1.0", rel, discard) {
		t.Fatal("not installed")
	}
	if s := Load(data); !s.Pending || s.From != "0.1.0" || s.To != "0.2.0" {
		t.Fatalf("state after the update: %+v", s)
	}
	return data, dir
}

// programsAre fails unless each program in dir says version.
func programsAre(t *testing.T, dir, version string) {
	t.Helper()
	for _, name := range Programs {
		if v, err := versionOf(context.Background(), filepath.Join(dir, name)); err != nil || v != version {
			t.Errorf("%s is %q (%v), not %s", name, v, err, version)
		}
	}
}

// The new version connects in time: it stays, and is no longer pending.
func TestConfirmKeepsAnUpdateThatConnects(t *testing.T) {
	data, dir := updated(t)
	if Starting(data, "0.2.0", discard) {
		t.Fatal("went back at the first start")
	}
	if s := Load(data); !s.Pending || s.Started.IsZero() {
		t.Fatalf("first start not noted: %+v", s)
	}
	var on atomic.Bool
	time.AfterFunc(50*time.Millisecond, func() { on.Store(true) })
	if Confirm(context.Background(), data, "0.2.0", on.Load, discard) {
		t.Fatal("went back though connected")
	}
	if s := Load(data); s.Pending || s.Failed != "" {
		t.Errorf("state once connected: %+v", s)
	}
	time.Sleep(verifyWithin)
	if Starting(data, "0.2.0", discard) {
		t.Error("went back at a later start")
	}
	programsAre(t, dir, "0.2.0")
}

// The new version runs but never connects: once verifyWithin has passed,
// it goes back to 0.1.0 and keeps that 0.2.0 failed.
func TestConfirmGoesBackWhenNotConnected(t *testing.T) {
	data, dir := updated(t)
	Starting(data, "0.2.0", discard)
	began := time.Now()
	if !Confirm(context.Background(), data, "0.2.0", func() bool { return false }, discard) {
		t.Fatal("did not go back")
	}
	if d := time.Since(began); d < verifyWithin/2 {
		t.Errorf("went back after %v, before verifyWithin", d)
	}
	programsAre(t, dir, "0.1.0")
	if s := Load(data); s.Pending || s.Failed != "0.2.0" {
		t.Errorf("state after going back: %+v", s)
	}
	// The version before, started again, says so and keeps saying it.
	if Starting(data, "0.1.0", discard) {
		t.Error("went back again")
	}
	if f := Failed(data, "0.1.0"); f != "0.2.0" {
		t.Errorf("Failed: %q", f)
	}
	if line := Line(data); !strings.Contains(line, "went back from 0.2.0") {
		t.Errorf("Line: %q", line)
	}
}

// The new version fails as it starts, again and again: its first start
// after verifyWithin goes back, before anything else.
func TestStartingGoesBackFromACrashLoop(t *testing.T) {
	data, dir := updated(t)
	for range 3 {
		if Starting(data, "0.2.0", discard) {
			t.Fatal("went back too soon")
		}
	}
	time.Sleep(verifyWithin)
	if !Starting(data, "0.2.0", discard) {
		t.Fatal("did not go back")
	}
	programsAre(t, dir, "0.1.0")
	if s := Load(data); s.Pending || s.Failed != "0.2.0" {
		t.Errorf("state after going back: %+v", s)
	}
}

// With nothing to go back to, the update stays and is no longer pending.
func TestStartingWithoutAPrevious(t *testing.T) {
	data, dir := updated(t)
	for _, name := range Programs {
		os.Remove(previousPath(dir, name))
	}
	Starting(data, "0.2.0", discard)
	time.Sleep(verifyWithin)
	if Starting(data, "0.2.0", discard) {
		t.Fatal("went back to nothing")
	}
	programsAre(t, dir, "0.2.0")
	if s := Load(data); s.Pending || s.Failed != "" {
		t.Errorf("state: %+v", s)
	}
}

// The version it went back from is not installed again; a newer one is,
// and the skip ends with it.
func TestBackgroundSkipsAFailedVersion(t *testing.T) {
	r := newRelease(t)
	r.publish("0.2.0")
	dir := programsAt(t, "0.1.0")
	executable = func() (string, error) { return filepath.Join(dir, Programs[0]), nil }
	t.Cleanup(func() { executable = os.Executable })
	setVar(t, &testInterval, "20ms")
	data := t.TempDir()
	Edit(data, func(s *State) { s.From, s.To, s.Failed = "0.1.0", "0.2.0", "0.2.0" })

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if Background(ctx, data, "0.1.0", func() int { return 0 }, discard) {
		t.Fatal("installed the version it went back from")
	}
	programsAre(t, dir, "0.1.0")
	if s := Load(data); s.Available != "" || s.Checked.IsZero() {
		t.Errorf("state: %+v", s)
	}

	r.publish("0.3.0")
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !Background(ctx, data, "0.1.0", func() int { return 0 }, discard) {
		t.Fatal("did not install a newer version")
	}
	programsAre(t, dir, "0.3.0")
	if s := Load(data); !s.Pending || s.To != "0.3.0" {
		t.Errorf("state after the update: %+v", s)
	}
	Starting(data, "0.3.0", discard)
	if s := Load(data); s.Failed != "" {
		t.Errorf("still skips %s on 0.3.0", s.Failed)
	}
}
