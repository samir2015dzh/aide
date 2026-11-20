//go:build !windows

package provider

// RunningExecutables are the paths of the programs named name running now:
// none off Windows, where nothing asks.
func RunningExecutables(name string) []string { return nil }

func appPath(exe string) string { return "" }

func uninstallEntries() []uninstallEntry { return nil }

// WindowsStorePackages are the Store packages registered for this user:
// none off Windows.
func WindowsStorePackages() []string { return nil }
