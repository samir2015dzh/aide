package update

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"github.com/samir2015dzh/aide/internal/legacy"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// release is a signed release served the way GitHub serves one.
type release struct {
	t     *testing.T
	key   *ecdsa.PrivateKey
	files map[string][]byte // under /download/v<version>/ and /latest/download/
	srv   *httptest.Server
}

func newRelease(t *testing.T) *release {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	r := &release{t: t, key: key, files: map[string][]byte{}}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name := filepath.Base(req.URL.Path)
		if b, ok := r.files[name]; ok && (strings.HasPrefix(req.URL.Path, "/releases/latest/download/") || strings.HasPrefix(req.URL.Path, "/releases/download/v")) {
			w.Write(b)
			return
		}
		http.NotFound(w, req)
	}))
	t.Cleanup(r.srv.Close)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	setVar(t, &publicKey, base64.StdEncoding.EncodeToString(der))
	setVar(t, &releases, r.srv.URL+"/releases")
	return r
}

// fakePrograms are programs that print a version, by version: a shell
// script, or on Windows a program built once.
var fakePrograms sync.Map

func fakeProgram(t *testing.T, version string) []byte {
	if runtime.GOOS != "windows" {
		return []byte("#!/bin/sh\necho " + version + "\n")
	}
	if b, ok := fakePrograms.Load(version); ok {
		return b.([]byte)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\""+version+"\") }\n"), 0o644)
	exe := filepath.Join(dir, "p.exe")
	if out, err := exec.Command("go", "build", "-o", exe, filepath.Join(dir, "main.go")).CombinedOutput(); err != nil {
		t.Fatalf("go build: %v %s", err, out)
	}
	b, _ := os.ReadFile(exe)
	fakePrograms.Store(version, b)
	return b
}

func setVar(t *testing.T, v *string, to string) {
	old := *v
	*v = to
	t.Cleanup(func() { *v = old })
}

// publish makes version the latest, with programs that say it.
func (r *release) publish(version string) {
	r.files = map[string][]byte{"version": []byte(version + "\n")}
	for _, name := range Programs {
		r.files[asset(name)] = fakeProgram(r.t, version)
	}
	r.sign()
}

// aide is the agent CLI among Programs.
var aide = map[bool]string{true: "aide.exe", false: "aide"}[runtime.GOOS == "windows"]

func (r *release) sign() {
	var sums strings.Builder
	for name, b := range r.files {
		if !strings.HasPrefix(name, "checksums") {
			h := sha256.Sum256(b)
			fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(h[:]), name)
		}
	}
	r.files["checksums.txt"] = []byte(sums.String())
	digest := sha256.Sum256(r.files["checksums.txt"])
	r.files["checksums.txt.sig"], _ = ecdsa.SignASN1(rand.Reader, r.key, digest[:])
}

// programsAt is a folder with the programs at version.
func programsAt(t *testing.T, version string) string {
	dir := t.TempDir()
	for _, name := range Programs {
		os.WriteFile(filepath.Join(dir, name), fakeProgram(t, version), 0o755)
	}
	return dir
}

func TestLatest(t *testing.T) {
	r := newRelease(t)
	r.publish("0.2.0")
	rel, err := Latest(context.Background())
	if err != nil || rel.Version != "0.2.0" {
		t.Fatalf("%v %v", rel, err)
	}
	if !rel.Newer("0.1.1-beta.5") || rel.Newer("0.2.0") || rel.Newer("0.10.0") {
		t.Error("Newer compares versions")
	}

	// A version the list doesn't sign.
	r.files["version"] = []byte("9.9.9\n")
	if _, err := Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "not in its signed checksum list") {
		t.Errorf("unsigned version: %v", err)
	}
	// A list signed by another key.
	r.publish("0.2.0")
	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	digest := sha256.Sum256(r.files["checksums.txt"])
	r.files["checksums.txt.sig"], _ = ecdsa.SignASN1(rand.Reader, other, digest[:])
	if _, err := Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "does not verify") {
		t.Errorf("another key: %v", err)
	}
	// No release yet.
	r.files = map[string][]byte{}
	if _, err := Latest(context.Background()); err == nil {
		t.Error("no release: no error")
	}
}

// The built-in key is the release key.
func TestPublicKeyIsTheReleaseKey(t *testing.T) {
	b, err := os.ReadFile("../../scripts/release-pubkey.pem")
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(b)
	if base64.StdEncoding.EncodeToString(block.Bytes) != publicKey {
		t.Error("publicKey is not scripts/release-pubkey.pem")
	}
}

func TestInstall(t *testing.T) {
	r := newRelease(t)
	r.publish("0.2.0")
	rel, _ := Latest(context.Background())
	dir := programsAt(t, "0.1.0")
	names, err := Install(context.Background(), t.TempDir(), dir, rel)
	if err != nil || strings.Join(names, " ") != strings.Join(Programs, " ") {
		t.Fatalf("%v %v", names, err)
	}
	for _, name := range Programs {
		if b, _ := os.ReadFile(filepath.Join(dir, name)); !strings.Contains(string(b), "0.2.0") {
			t.Errorf("%s: %s", name, b)
		}
		if b, _ := os.ReadFile(previousPath(dir, name)); !strings.Contains(string(b), "0.1.0") {
			t.Errorf("%s's previous: %.40q", name, b)
		}
		if _, err := os.Stat(newPath(dir, name)); err == nil {
			t.Errorf("%s's new left behind", name)
		}
	}
	// No program's name from before the rename is made where there was none.
	for _, name := range Programs {
		if old := legacyOf(name); old != "" {
			if _, err := os.Lstat(filepath.Join(dir, old)); err == nil {
				t.Errorf("%s made beside %s", old, name)
			}
		}
	}

	// Only the programs there: an agent CLI alone.
	cli := t.TempDir()
	os.WriteFile(filepath.Join(cli, aide), fakeProgram(t, "0.1.0"), 0o755)
	if names, err := Install(context.Background(), t.TempDir(), cli, rel); err != nil || strings.Join(names, " ") != aide {
		t.Errorf("CLI only: %v %v", names, err)
	}

}

// A machine from a version with Her: her and the folders beside her, with
// what an update kept of them, go with the update; the programs are updated.
func TestInstallRemovesHerLeftovers(t *testing.T) {
	r := newRelease(t)
	r.publish("0.2.0")
	rel, _ := Latest(context.Background())
	dir := programsAt(t, "0.1.0")
	var left []string
	for _, name := range []string{"her", "her.exe"} {
		os.WriteFile(filepath.Join(dir, name), fakeProgram(t, "0.1.0"), 0o755)
		os.WriteFile(previousPath(dir, name), fakeProgram(t, "0.0.9"), 0o755)
		left = append(left, filepath.Join(dir, name), previousPath(dir, name))
	}
	for _, name := range []string{"her-console", "her-computer", "her-pdf"} {
		os.MkdirAll(filepath.Join(dir, name, "assets"), 0o755)
		os.WriteFile(filepath.Join(dir, name, "assets", "a"), []byte("0.1.0"), 0o644)
		os.MkdirAll(previousPath(dir, name), 0o755)
		left = append(left, filepath.Join(dir, name), previousPath(dir, name))
	}
	// Not Her's: stays.
	os.WriteFile(filepath.Join(dir, "hermit"), []byte("mine"), 0o644)
	names, err := Install(context.Background(), t.TempDir(), dir, rel)
	if err != nil || strings.Join(names, " ") != strings.Join(Programs, " ") {
		t.Fatalf("%v %v", names, err)
	}
	for _, p := range left {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s left", filepath.Base(p))
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "hermit")); err != nil {
		t.Errorf("a file not Her's removed: %v", err)
	}
	for _, name := range Programs {
		if b, _ := os.ReadFile(filepath.Join(dir, name)); !strings.Contains(string(b), "0.2.0") {
			t.Errorf("%s: %s", name, b)
		}
	}

	// A failed update leaves them as they are.
	r.publish("0.3.0")
	r.files[asset(aide)] = append(fakeProgram(t, "0.3.0"), ' ')
	rel, _ = Latest(context.Background())
	os.WriteFile(filepath.Join(dir, "her"), fakeProgram(t, "0.1.0"), 0o755)
	if _, err := Install(context.Background(), t.TempDir(), dir, rel); err == nil {
		t.Fatal("a program that doesn't match its hash: no error")
	}
	if _, err := os.Stat(filepath.Join(dir, "her")); err != nil {
		t.Errorf("her removed by a failed update: %v", err)
	}
}

// A program that doesn't match its hash, or doesn't say its version,
// replaces nothing.
func TestInstallRefuses(t *testing.T) {
	r := newRelease(t)
	cli := asset(aide)
	changes := map[string]func(){
		"checksum": func() { r.files[cli] = append(fakeProgram(t, "0.2.0"), ' ') },
		"start as": func() {
			r.files[cli] = fakeProgram(t, "0.1.9")
			r.sign()
		},
	}
	for what, change := range changes {
		r.publish("0.2.0")
		change()
		rel, _ := Latest(context.Background())
		dir := programsAt(t, "0.1.0")
		if _, err := Install(context.Background(), t.TempDir(), dir, rel); err == nil || !strings.Contains(err.Error(), what) {
			t.Errorf("%s: %v", what, err)
		}
		for _, name := range Programs {
			if b, _ := os.ReadFile(filepath.Join(dir, name)); !strings.Contains(string(b), "0.1.0") {
				t.Errorf("%s: %s replaced", what, name)
			}
			if _, err := os.Stat(newPath(dir, name)); err == nil {
				t.Errorf("%s: %s's new left behind", what, name)
			}
		}
	}
}

func TestDirRefusesTheApp(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The Windows app's programs, in the folder install.ps1 uses.
		local := t.TempDir()
		t.Setenv("LOCALAPPDATA", local)
		os.MkdirAll(filepath.Join(local, "AideApp"), 0o755)
		os.WriteFile(filepath.Join(local, "AideApp", "Update.exe"), nil, 0o755)
		executable = func() (string, error) { return filepath.Join(local, "Programs", "Aide", "bin", "aided.exe"), nil }
		t.Cleanup(func() { executable = os.Executable })
		os.MkdirAll(filepath.Join(local, "Programs", "Aide", "bin"), 0o755)
		os.WriteFile(filepath.Join(local, "Programs", "Aide", "bin", "aided.exe"), nil, 0o755)
		if _, err := Dir(); err == nil || !strings.Contains(err.Error(), "the Aide app") {
			t.Errorf("%v", err)
		}
		return
	}
	app := filepath.Join(t.TempDir(), "Aide.app", "Contents", "Resources")
	os.MkdirAll(app, 0o755)
	os.WriteFile(filepath.Join(app, "aided"), nil, 0o755)
	executable = func() (string, error) { return filepath.Join(app, "aided"), nil }
	t.Cleanup(func() { executable = os.Executable })
	if _, err := Dir(); err == nil || !strings.Contains(err.Error(), "the Aide app") {
		t.Errorf("%v", err)
	}
}

// The background updater waits while a session runs, installs once none
// does, and does nothing while automatic updates are off.
func TestBackground(t *testing.T) {
	r := newRelease(t)
	r.publish("0.2.0")
	dir := programsAt(t, "0.1.0")
	executable = func() (string, error) { return filepath.Join(dir, Programs[0]), nil }
	t.Cleanup(func() { executable = os.Executable })
	setVar(t, &testInterval, "20ms")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	data := t.TempDir()

	Edit(data, func(s *State) { s.Off = true })
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	if Background(ctx, data, "0.1.0", func() int { return 0 }, log) {
		t.Error("installed while off")
	}
	cancel()
	if s := Load(data); !s.Checked.IsZero() {
		t.Errorf("checked while off: %+v", s)
	}

	Edit(data, func(s *State) { s.Off = false })
	var busy atomic.Int32
	busy.Store(1)
	done := make(chan bool)
	go func() {
		done <- Background(context.Background(), data, "0.1.0", func() int { return int(busy.Load()) }, log)
	}()
	time.Sleep(300 * time.Millisecond)
	if b, _ := os.ReadFile(filepath.Join(dir, Programs[0])); !strings.Contains(string(b), "0.1.0") {
		t.Fatal("installed while a session runs")
	}
	if s := Load(data); s.Available != "0.2.0" || s.Checked.IsZero() {
		t.Errorf("state while waiting: %+v", s)
	}
	busy.Store(0)
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("Background returned false")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not installed once idle")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, Programs[0])); !strings.Contains(string(b), "0.2.0") {
		t.Error("not installed")
	}
	if s := Load(data); s.From != "0.1.0" || s.To != "0.2.0" || s.Available != "" {
		t.Errorf("state after: %+v", s)
	}
	if line := Line(data); line != "automatic · checked just now" {
		t.Errorf("Line: %q", line)
	}
}

// her-computer/ and her-pdf/, where the install command put agent-desktop
// for Her's computer use and her PDF worker (docs/linux.md §4, §5), are
// replaced whole with her from the same release, their programs still
// programs; a machine without the folder gets none.
// An installation from before the rename: its programs under their old
// names, the new names links to them (Windows: copies), and its service
// running the old name. The update writes the programs under their new names
// and keeps the old ones as links to them (copies), up to date.
func TestInstallOverAnInstallationFromBeforeTheRename(t *testing.T) {
	r := newRelease(t)
	r.publish("0.2.0")
	rel, _ := Latest(context.Background())
	dir := t.TempDir()
	exe := filepath.Ext(Programs[0])
	for _, name := range Programs {
		old := legacyOf(name)
		if old == "" {
			os.WriteFile(filepath.Join(dir, name), fakeProgram(t, "0.1.0"), 0o755)
			continue
		}
		os.WriteFile(filepath.Join(dir, old), fakeProgram(t, "0.1.0"), 0o755)
		if runtime.GOOS == "windows" {
			os.WriteFile(filepath.Join(dir, name), fakeProgram(t, "0.1.0"), 0o755)
		} else {
			os.Symlink(old, filepath.Join(dir, name))
		}
	}
	if _, err := Install(context.Background(), t.TempDir(), dir, rel); err != nil {
		t.Fatal(err)
	}
	for _, name := range Programs {
		fi, err := os.Lstat(filepath.Join(dir, name))
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s is not the program itself: %v %v", name, fi, err)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, name)); !strings.Contains(string(b), "0.2.0") {
			t.Errorf("%s: %.40q", name, b)
		}
		old := legacyOf(name)
		if old == "" {
			continue
		}
		if b, _ := os.ReadFile(filepath.Join(dir, old)); !strings.Contains(string(b), "0.2.0") {
			t.Errorf("%s, the old name of %s: %.40q", old, name, b)
		}
		if target, err := os.Readlink(filepath.Join(dir, old)); runtime.GOOS != "windows" && target != name {
			t.Errorf("%s links to %q (%v), not %s", old, target, err, name)
		}
	}
	if legacyOf("aided"+exe) != legacy.Daemon+exe {
		t.Errorf("aided's old name: %q", legacyOf("aided"+exe))
	}
}
