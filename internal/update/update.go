// Package update installs newer Aide releases over the programs in
// place, for `aided update`, `aide update` and aided's background
// updater (docs/machine-updates.md).
package update

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/samir2015dzh/aide/internal/legacy"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/samir2015dzh/aide/internal/flock"
)

// Build-time constants: test builds set them with -ldflags -X.
var (
	// releases is where the releases are.
	releases = "https://github.com/samir2015dzh/aide/releases"
	// publicKey is the release signing key, scripts/release-pubkey.pem as
	// base64 DER.
	publicKey = "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEzD/o6QtTf/uoNlaxWkJ9d/wLLUMGMmUzv8882oxECt9fQtjjhZqDaRuq6w1x4fz5tNHzpwE6nTRZUitMp7VkuA=="
	// testInterval, when set, replaces every wait of the background updater.
	testInterval = ""
)

// Programs are the programs an update replaces, where they are (Windows:
// with aidedw, the windowless aided its service runs).
var Programs = programs()

func programs() []string {
	if runtime.GOOS == "windows" {
		return []string{"aided.exe", "aidedw.exe", "aide.exe"}
	}
	return []string{"aided", "aide"}
}

// legacyNames are the names the programs had before the rename
// (internal/legacy), which the services, logon tasks and agents' hooks of an
// installation from before it run. Where one is in the programs' folder, it
// is kept up to date as a link to the program (on Windows a copy of it);
// none is made where there was none.
var legacyNames = map[string]string{"aided": legacy.Daemon, "aidedw": legacy.DaemonWindowless, "aide": legacy.CLI}

// legacyOf is the name name had before the rename, "" for none.
func legacyOf(name string) string {
	base := strings.TrimSuffix(name, ".exe")
	if l := legacyNames[base]; l != "" {
		return l + name[len(base):]
	}
	return ""
}

// present tells whether the program name is in dir, under its name or the
// one it had: a program from before the rename is updated to its new name.
// An alias of it under its new name (a link to the old file) gives way to
// the program itself.
func present(dir, name string) bool {
	path := filepath.Join(dir, name)
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		if l := legacyOf(name); l != "" && exists(filepath.Join(dir, l)) {
			os.Remove(path)
			return true
		}
	}
	if exists(path) {
		return true
	}
	l := legacyOf(name)
	return l != "" && exists(filepath.Join(dir, l))
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

// placeLegacy makes each of names' old names that is in dir the program
// again: a link to it, or on Windows a copy, the running one renamed aside
// first.
func placeLegacy(dir string, names []string) error {
	for _, name := range names {
		old := legacyOf(name)
		if old == "" {
			continue
		}
		path := filepath.Join(dir, old)
		if _, err := os.Lstat(path); err != nil {
			continue
		}
		if runtime.GOOS != "windows" {
			if target, err := os.Readlink(path); err == nil && target == name {
				continue
			}
			tmp := sibling(dir, old, "link")
			os.Remove(tmp)
			if err := os.Symlink(name, tmp); err != nil {
				return err
			}
			if err := os.Rename(tmp, path); err != nil {
				return err
			}
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		prev := previousPath(dir, old)
		if older, _ := filepath.Glob(prev + ".*"); older != nil {
			for _, o := range older {
				os.Remove(o)
			}
		}
		if err := os.Rename(path, fmt.Sprintf("%s.%d", prev, time.Now().UnixNano())); err != nil {
			return err
		}
		if err := os.WriteFile(path, body, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// herLeftovers are what versions with Her placed beside aided: her, Her's
// kernel, and the folders beside her (her console, her computer use, her
// PDF worker), with what an update kept of each as its previous. Aide no
// longer has Her: an update removes them (docs/machine-updates.md).
var herLeftovers = []string{"her", "her.exe", "her-console", "her-computer", "her-pdf"}

// removeHerLeftovers removes herLeftovers from dir. One that runs (an
// old her on Windows) stays until the next update.
func removeHerLeftovers(dir string) {
	for _, name := range herLeftovers {
		for _, p := range []string{filepath.Join(dir, name), previousPath(dir, name), newPath(dir, name)} {
			os.RemoveAll(p)
		}
	}
}

// asset is a program's file in a release: aided-linux-arm64,
// aided-windows-amd64.exe.
func asset(name string) string {
	base := strings.TrimSuffix(name, ".exe")
	return base + "-" + runtime.GOOS + "-" + runtime.GOARCH + name[len(base):]
}

// newPath and previousPath are where a program's next and last versions
// wait beside it, keeping its extension (Windows runs .exe files only).
func newPath(dir, name string) string { return sibling(dir, name, "new") }

func previousPath(dir, name string) string { return sibling(dir, name, "previous") }

func sibling(dir, name, what string) string {
	base := strings.TrimSuffix(name, ".exe")
	return filepath.Join(dir, "."+base+"."+what+name[len(base):])
}

// Release is a signed release: its version and its files' hashes.
type Release struct {
	Version string
	sums    map[string]string
}

var client = &http.Client{Timeout: 5 * time.Minute}

// Latest reads the latest release's signed checksum list and version.
func Latest(ctx context.Context) (*Release, error) {
	base := releases + "/latest/download/"
	sums, err := fetch(ctx, base+"checksums.txt", 1<<20)
	if err != nil {
		return nil, err
	}
	sig, err := fetch(ctx, base+"checksums.txt.sig", 4<<10)
	if err != nil {
		return nil, err
	}
	version, err := fetch(ctx, base+"version", 1<<10)
	if err != nil {
		return nil, err
	}
	return verify(sums, sig, version)
}

// verify checks the list's signature and the version against the list.
func verify(sums, sig, version []byte) (*Release, error) {
	der, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pub, ok := key.(*ecdsa.PublicKey)
	digest := sha256.Sum256(sums)
	if !ok || !ecdsa.VerifyASN1(pub, digest[:], sig) {
		return nil, errors.New("the release's checksum signature does not verify")
	}
	r := &Release{sums: map[string]string{}}
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			r.sums[f[1]] = f[0]
		}
	}
	if h := sha256.Sum256(version); r.sums["version"] != hex.EncodeToString(h[:]) {
		return nil, errors.New("the release's version is not in its signed checksum list")
	}
	r.Version = strings.TrimSpace(string(version))
	if !semver.IsValid("v" + r.Version) {
		return nil, fmt.Errorf("the release's version %q is not a version", r.Version)
	}
	return r, nil
}

// Newer reports whether r is a higher version than current.
func (r *Release) Newer(current string) bool { return newer(r.Version, current) }

// newer reports whether version a is higher than b.
func newer(a, b string) bool { return semver.Compare("v"+a, "v"+b) > 0 }

func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, limit))
}

var executable = os.Executable

// Dir is the folder of the running program, which an update writes.
func Dir() (string, error) {
	exe, err := executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	if strings.Contains(exe, ".app/Contents/") {
		return "", errors.New("this aided is part of the Aide app, which updates itself")
	}
	// The Windows app copies its programs into the folder install.ps1 uses,
	// and updates them itself (docs/windows.md §6.2).
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(filepath.Join(os.Getenv("LOCALAPPDATA"), "AideApp", "Update.exe")); err == nil {
			return "", errors.New("this aided is the Aide app's, which updates it")
		}
	}
	return filepath.Dir(exe), nil
}

// Install puts r's programs in dir over the ones there, keeping each old
// one as .<name>.previous. Each new program must start and say r's version
// before any is replaced. On Windows a running program can't be replaced,
// only renamed: the old one moves aside instead. Once they are in place,
// what versions with Her left in dir goes (herLeftovers).
func Install(ctx context.Context, dataDir, dir string, r *Release) ([]string, error) {
	lock, err := flock.Acquire(filepath.Join(dataDir, "update"))
	if err != nil {
		return nil, errors.New("another update is running")
	}
	defer lock.Release()
	var names []string
	for _, name := range Programs {
		if present(dir, name) {
			names = append(names, name)
		}
	}
	if names == nil {
		return nil, fmt.Errorf("no Aide programs in %s", dir)
	}
	for _, name := range names {
		if err := download(ctx, dir, name, r); err != nil {
			for _, n := range names {
				os.Remove(newPath(dir, n))
			}
			return nil, err
		}
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		prev := previousPath(dir, name)
		os.Remove(prev)
		if !exists(path) {
			// Only under its old name until now: nothing to move aside.
		} else if runtime.GOOS == "windows" {
			older, _ := filepath.Glob(prev + ".*")
			for _, o := range older {
				os.Remove(o) // unless it runs
			}
			// The one before may still run (the service's supervisor):
			// it stays, and this one moves beside it.
			if err := os.Rename(path, prev); err != nil {
				if err := os.Rename(path, fmt.Sprintf("%s.%d", prev, time.Now().UnixNano())); err != nil {
					return nil, err
				}
			}
		} else {
			os.Link(path, prev)
		}
		if err := os.Rename(newPath(dir, name), path); err != nil {
			return nil, err
		}
	}
	removeHerLeftovers(dir)
	if err := placeLegacy(dir, names); err != nil {
		return names, fmt.Errorf("updated, but not the programs' old names: %w", err)
	}
	return names, nil
}

// download puts name's next version beside it, checked.
func download(ctx context.Context, dir, name string, r *Release) error {
	file := asset(name)
	want := r.sums[file]
	if want == "" {
		return fmt.Errorf("%s is not in the release's signed checksum list", file)
	}
	body, err := fetch(ctx, releases+"/download/v"+r.Version+"/"+file, 512<<20)
	if err != nil {
		return err
	}
	if h := sha256.Sum256(body); hex.EncodeToString(h[:]) != want {
		return fmt.Errorf("%s does not match its checksum", file)
	}
	tmp := newPath(dir, name)
	if err := os.WriteFile(tmp, body, 0o755); err != nil {
		return fmt.Errorf("cannot write %s: %w", dir, err)
	}
	if v, err := versionOf(ctx, tmp); err != nil || v != r.Version {
		return fmt.Errorf("the downloaded %s does not start as %s: %v %s", name, r.Version, err, v)
	}
	return nil
}

// versionOf is the version the program at path says it is.
func versionOf(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	return strings.TrimSpace(string(out)), err
}

// State is what the updater keeps in <data dir>/update/update.json.
type State struct {
	Off       bool      `json:"off,omitempty"`       // automatic updates turned off
	Checked   time.Time `json:"checked,omitzero"`    // the last successful check
	Available string    `json:"available,omitempty"` // a newer version it found
	From      string    `json:"from,omitempty"`      // the last update's versions
	To        string    `json:"to,omitempty"`
	Updated   time.Time `json:"updated,omitzero"`
	// Pending: the last update was the background updater's, and To has not
	// connected to its relay yet; Started is when To first started. Failed:
	// the version it went back from, skipped until a newer one (verify.go).
	Pending bool      `json:"pending,omitempty"`
	Started time.Time `json:"started,omitzero"`
	Failed  string    `json:"failed,omitempty"`
}

func statePath(dataDir string) string { return filepath.Join(dataDir, "update", "update.json") }

// Load reads the state; none yet is the zero State (automatic updates on).
func Load(dataDir string) State {
	var s State
	if b, err := os.ReadFile(statePath(dataDir)); err == nil {
		json.Unmarshal(b, &s)
	}
	return s
}

// Edit changes the state.
func Edit(dataDir string, f func(*State)) error {
	s := Load(dataDir)
	f(&s)
	b, _ := json.MarshalIndent(s, "", "  ")
	path := statePath(dataDir)
	os.MkdirAll(filepath.Dir(path), 0o700)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
