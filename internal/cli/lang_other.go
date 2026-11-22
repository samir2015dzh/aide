//go:build !windows

package cli

// systemLanguage: off Windows the environment (LC_ALL, LC_MESSAGES, LANG)
// is the system's word, read in language.
func systemLanguage() string { return "" }
