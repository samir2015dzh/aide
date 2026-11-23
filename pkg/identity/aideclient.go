package identity

import "strings"

// aideClientPrefix starts the name the aide command line logs in under on a
// machine when it is given none: "client@<hostname>" (internal/cli login.go,
// applogin.go).
const aideClientPrefix = "client@"

// IsAideClient reports whether an agent's label is that name: Aide's own
// command line on a machine, a connection of the account, not an agent (工单
// #64, #79). The roster lists agents only; the apps' rule is the same
// (@aide/core isAideClient, the Android app's Roster.kt).
func IsAideClient(label string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(label)), aideClientPrefix)
}
