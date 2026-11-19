// Package brandenv reads and writes the environment variables Aide takes,
// under the names they have since the product was called Aide (AIDE_X)
// and, as machines and containers set up before then have them, under the
// names they had (HERMESH_X): RENAME-AIDE.md §2.4. The new name wins when
// both are set; neither name is going away.
package brandenv

import "os"

// The prefixes: the name since the rename, and the one before.
const (
	Prefix    = "AIDE_"
	OldPrefix = "HERMESH_"
)

// Get is AIDE_<name>, or HERMESH_<name> if that is empty or unset.
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
