package dshnative

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
)

var createSemaphore = windows.NewLazySystemDLL("kernel32.dll").NewProc("CreateSemaphoreW")

// A session whose lock semaphore exists, as while DSH has the session
// open, is running or idle by its last turn; the probe leaves the
// semaphore's count as it is.
func TestLockProbeWindows(t *testing.T) {
	home := t.TempDir()
	idle := writeSession(t, home, map[string]any{"id": "idle", "cwd": `C:\Users\Alex\App`}, true, conversation(0, 1, "a", "b"))
	running := writeSession(t, home, map[string]any{"id": "running"}, true, events(0, ev("turn/start", map[string]any{"turn": 1}), userMsg("Go")))
	writeSession(t, home, map[string]any{"id": "closed"}, true, conversation(0, 1, "a", "b"))
	var held []windows.Handle
	for _, dir := range []string{idle, running} {
		// DSH's own name: sha256 of resolve(lock path).toLowerCase().
		sum := sha256.Sum256([]byte(strings.ToLower(filepath.Join(dir, "session.lock"))))
		name, _ := windows.UTF16PtrFromString(`Local\dsh-session-lock-` + hex.EncodeToString(sum[:]))
		r, _, err := createSemaphore.Call(0, 1, 1, uintptr(unsafe.Pointer(name)))
		if r == 0 {
			t.Fatal(err)
		}
		h := windows.Handle(r)
		defer windows.CloseHandle(h)
		if ev, err := windows.WaitForSingleObject(h, 0); ev != windows.WAIT_OBJECT_0 {
			t.Fatal(ev, err)
		}
		held = append(held, h)
	}
	list, err := List(home, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]machinev1.DSHSessionState{"idle": machinev1.DSHSessionState_DSH_SESSION_STATE_IDLE,
		"running": machinev1.DSHSessionState_DSH_SESSION_STATE_RUNNING, "closed": machinev1.DSHSessionState_DSH_SESSION_STATE_ENDED}
	for _, s := range list {
		if s.State != want[s.SessionId] {
			t.Errorf("%s: %v", s.SessionId, s.State)
		}
	}
	// Still taken: another wait times out.
	if ev, _ := windows.WaitForSingleObject(held[0], 0); ev != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("the semaphore's count changed: %d", ev)
	}
}
