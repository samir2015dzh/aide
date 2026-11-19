//go:build !windows

package machine

// sealSecret keeps a custom agent's secret as it is: outside Windows the
// machine has no credential store it can use without a person present (a
// LaunchAgent or systemd user unit reaches no unlocked keyring for sure),
// so the value is in the keys folder beside the machine's own private key,
// readable by this user only, as that key is.
func sealSecret(b []byte) ([]byte, error) { return b, nil }

func openSecret(b []byte) ([]byte, error) { return b, nil }

const secretStore = "the machine's keys folder (this user only)"
