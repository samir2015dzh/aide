//go:build !windows

package provider

// Elsewhere than Windows there is no uninstall list to read: apps are
// found by their paths.
func installedApps() []InstalledApp            { return nil }
func appPath(program string) string            { return "" }
func runningPrograms(names ...string) []string { return nil }
