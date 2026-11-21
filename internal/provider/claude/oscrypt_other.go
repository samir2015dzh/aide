//go:build !windows

package claude

// dpapiUnprotect has no equivalent off Windows: the Claude desktop app's
// token cache is sealed to the Windows user's key store, which this OS
// cannot reach. The standard Claude Code credentials file (unencrypted)
// is the only token source here.
func dpapiUnprotect(b []byte) ([]byte, error) { return nil, errUnsupportedOS }

// desktopKeyStoreAvailable: this OS cannot unwrap the token cache key.
const desktopKeyStoreAvailable = false
