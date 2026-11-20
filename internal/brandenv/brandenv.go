// Package brandenv reads and writes the environment variables Aide takes,
// under the names they have since the product was called Aide (AIDE_X)
// and, as machines and containers set up before then have them, under the
// names they had (internal/legacy: docs/aide-rename.md). The new name wins
// when both are set.
package brandenv

import (
	"os"

	"github.com/hermeshai/hermesh/internal/legacy"
)

// The prefixes: the name since the rename, and the one before.
const (
	Prefix    = "AIDE_"
	OldPrefix = legacy.EnvPrefix
)

// Get is AIDE_<name>, or the old name's if that is empty or unset.
func Get(name string) string {
	if v := os.Getenv(Prefix + name); v != "" {
		return v
	}
	return os.Getenv(OldPrefix + name)
}

// Both is name=value under both names, for a process of ours to read
// whichever it knows: one from before the rename reads only the old name.
func Both(name, value string) []string {
	return []string{Prefix + name + "=" + value, OldPrefix + name + "=" + value}
}
