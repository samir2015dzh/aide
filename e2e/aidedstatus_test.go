package e2e

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// Work order #36: on the app's PC the machine in use lives in the owner
// profile (owner/profiles/<id>/machine), and the record at the top of the
// data folder is from before it, never logged in. aided status and doctor,
// pointed at that data folder, report the machine in use: logged in, as
// the running app says — while it runs (they ask it), and after it stopped
// (they open the profile's record, not the top one).
func TestAidedStatusOnTheAppsDataFolder(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e")
	}
	w := newOwnerMac(t, []string{"AIDE_FAKE_PROVIDERS=1"}, &localv1.GrantSpec{})
	data := filepath.Join(w.tmp, "owner-mac")
	aided := filepath.Join(w.bin, "aided")
	env := append([]string{"AIDE_FAKE_PROVIDERS=1"}, w.daemonEnv...)
	if r := run(t, env, time.Minute, aided, "--data-dir", data, "status"); !strings.Contains(r.stdout, `logged in as "owner-mac"`) {
		t.Fatalf("status while the app runs:\n%s%s", r.stdout, r.stderr)
	}
	w.owner.stop()
	r := run(t, env, time.Minute, aided, "--data-dir", data, "status")
	if !strings.Contains(r.stdout, `logged in as "owner-mac"`) || strings.Contains(r.stdout, "not logged in") {
		t.Fatalf("status once the app stopped:\n%s%s", r.stdout, r.stderr)
	}
	d := run(t, env, time.Minute, aided, "--data-dir", data, "doctor")
	if !strings.Contains(d.stdout, `logged in    yes, as "owner-mac"`) {
		t.Fatalf("doctor once the app stopped:\n%s%s", d.stdout, d.stderr)
	}
}
