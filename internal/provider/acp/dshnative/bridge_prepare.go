package dshnative

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	dshbridge "github.com/samir2015dzh/aide/apps/dsh-bridge"
)

// Installing the bridge plugin is DSH's own job: its plugin manager (DSH
// Desktop › Plugins) checks the plugin against the running DSH, restores
// the profile when an install fails, keeps a plugin that won't load from
// taking the Host down, and turns it off or removes it. Aide prepares
// for it: it backs up DSH's configuration, then writes the plugin's
// package where it stays (docs/dsh-takeover.md §4).

// Prepared is what PrepareBridge did.
type Prepared struct {
	// Package is the plugin's npm tarball, to install from.
	Package string
	Version string
	// Backup is the folder the configuration was copied to; BackedUp,
	// the files copied, relative to the DSH home.
	Backup   string
	BackedUp []string
	// DesktopProfile: DSH Desktop's profile was found at
	// <home>/profiles/desktop (and backed up with the rest).
	DesktopProfile bool
}

// PrepareBridge backs up the DSH home's configuration (its own and each
// profile's package.json, cordis.patch.yml, lock and compatibility files:
// not sessions, not installed packages) into BridgeDir(home)/backups/<time>,
// and writes the plugin's package into BridgeDir(home). It changes nothing
// DSH reads.
// ShippedVersion is the plugin's version this build ships (and prepares).
func ShippedVersion() string { return dshbridge.Version() }

// PreparedPackage is the plugin's package PrepareBridge wrote in home, of
// the version this build ships; "" when there is none.
func PreparedPackage(home string) string {
	p := filepath.Join(BridgeDir(home), "aide-dsh-bridge-"+dshbridge.Version()+".tgz")
	if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		return ""
	}
	return p
}

func PrepareBridge(home string, now time.Time) (*Prepared, error) {
	if st, err := os.Stat(home); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("no DSH home at %s: open DSH Desktop once first", home)
	}
	dir := BridgeDir(home)
	backup := filepath.Join(dir, "backups", now.UTC().Format("20060102-150405"))
	if err := os.MkdirAll(backup, 0o700); err != nil {
		return nil, err
	}
	p := &Prepared{Backup: backup, Version: dshbridge.Version()}
	copyConfig := func(rel string) error {
		entries, err := os.ReadDir(filepath.Join(home, rel))
		if err != nil {
			return nil // a folder that isn't there has nothing to copy
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || !isConfig(e.Name()) {
				continue
			}
			b, err := os.ReadFile(filepath.Join(home, rel, e.Name()))
			if err != nil {
				return err
			}
			to := filepath.Join(backup, rel, e.Name())
			if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(to, b, 0o600); err != nil {
				return err
			}
			p.BackedUp = append(p.BackedUp, filepath.Join(rel, e.Name()))
		}
		return nil
	}
	if err := copyConfig("."); err != nil {
		return nil, err
	}
	profiles, _ := os.ReadDir(filepath.Join(home, "profiles"))
	for _, pr := range profiles {
		if !pr.IsDir() {
			continue
		}
		if pr.Name() == "desktop" {
			p.DesktopProfile = true
		}
		if err := copyConfig(filepath.Join("profiles", pr.Name())); err != nil {
			return nil, err
		}
	}
	sort.Strings(p.BackedUp)
	tgz, err := dshbridge.Tarball()
	if err != nil {
		return nil, err
	}
	p.Package = filepath.Join(dir, "aide-dsh-bridge-"+p.Version+".tgz")
	tmp := p.Package + ".tmp"
	if err := os.WriteFile(tmp, tgz, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, p.Package); err != nil {
		return nil, err
	}
	return p, nil
}

func isConfig(name string) bool {
	for _, ext := range []string{".json", ".yml", ".yaml"} {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}
