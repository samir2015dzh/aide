package e2e

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

// TestDSHNativeSessionState: while a DSH process has a session open (it
// holds the flock on its session.lock, here the test does) the owner sees
// it running, or idle once its turn ended; once the process lets go, ended.
// Looking never takes the lock: DSH can take it again at once.
func TestDSHNativeSessionState(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	home := t.TempDir()
	w := newOwnerMac(t, []string{"DSH_HOME=" + home}, &localv1.GrantSpec{})
	ctx := context.Background()
	dir := writeDSHLog(t, home, map[string]any{"id": "open-1", "cwd": "/home/alex/app"}, dshEvents(0, dshEvent("turn/start", map[string]any{"turn": 1})))
	lock, err := os.Create(filepath.Join(dir, "session.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	state := func() machinev1.DSHSessionState {
		t.Helper()
		list, err := w.app.client.ListDSHSessions(ctx, &localv1.ListDSHSessionsRequest{})
		if err != nil || len(list.Sessions) != 1 {
			t.Fatalf("list: %v %v", list, err)
		}
		return list.Sessions[0].Session.State
	}

	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if s := state(); s != machinev1.DSHSessionState_DSH_SESSION_STATE_RUNNING {
		t.Fatalf("its turn under way: %v", s)
	}
	f, _ := os.OpenFile(filepath.Join(dir, "session.v4.jsonl.zstd"), os.O_APPEND|os.O_WRONLY, 0)
	f.Write(dshZstd.EncodeAll([]byte(`{"type":"turn/end","seq":1,"time":1,"data":{"turn":1,"reason":{"kind":"completed"}}}`+"\n"), nil))
	f.Close()
	if s := state(); s != machinev1.DSHSessionState_DSH_SESSION_STATE_IDLE {
		t.Fatalf("its turn done: %v", s)
	}
	syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if s := state(); s != machinev1.DSHSessionState_DSH_SESSION_STATE_ENDED {
		t.Fatalf("let go: %v", s)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("DSH can't take its lock again: %v", err)
	}
}
