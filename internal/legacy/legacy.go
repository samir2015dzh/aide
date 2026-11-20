// Package legacy holds the bytes of the product's name before it was called
// Aide (docs/aide-rename.md): every value here is in data already stored,
// signed or encrypted, or names what an installation made before the rename
// set up on a computer. Changing one makes that data unreadable or that
// installation unfindable, so nothing here is renamed; nothing else in the
// code may say the old name.
package legacy

import "strings"

// Signing and encryption labels (domain separation): what was signed,
// sealed or hashed with them verifies, opens or matches only with them.
const (
	// SignPrefix starts the input of every signature (pkg/identity).
	SignPrefix = "hermesh-sig-v1\x00"
	// Admit starts a device's, machine's or agent's admission proof to the
	// relay (pkg/transport).
	Admit = "hermesh-admit-v1\x00"
	// Notice is the HPKE info of a notice sealed to a phone (pkg/identity).
	Notice = "hermesh-notice-v1"
	// Message is the HPKE info of an agent inbox message (pkg/identity).
	Message = "hermesh-message-v1\x00"
	// Audit starts each signed record of a machine's audit log.
	Audit = "hermesh-audit-v1\x00"
	// AgentSecretEntropy is the DPAPI entropy of custom agents' secrets (Windows).
	AgentSecretEntropy = "hermesh custom agent environment"
	// PasswordKey is the AAD of a password login from before accounts (pkg/relay).
	PasswordKey = "hermesh password key\x00"
	// MCPKey, MCPDoorbell and MCPInbox are the AAD of the MCP gateway's sealed keys.
	MCPKey      = "hermesh-mcp-key-v1\x00"
	MCPDoorbell = "hermesh-mcp-doorbell-v1\x00"
	MCPInbox    = "hermesh-mcp-inbox-v1\x00"
	// MCPSecret starts the hash of a secret the gateway handed out (with its domain).
	MCPSecret = "hermesh-mcp-"
	// WakeLink starts the hash of a wake link's code.
	WakeLink = "hermesh-wake-link-v1\x00"
)

// protoPackage is the proto packages' prefix in signed type names.
const protoPackage = "hermesh."

// SignedType is the name a message type is signed under: its protobuf full
// name, with the packages' name from before the rename ("aide.identity.v1.Grant"
// is signed as it always was, "hermesh.identity.v1.Grant").
func SignedType(fullName string) string {
	if rest, ok := strings.CutPrefix(fullName, "aide."); ok {
		return protoPackage + rest
	}
	return fullName
}

// What an installation from before the rename made on a computer, looked for
// so that it keeps working or is cleaned up.
const (
	// DataDir is the data directory's name (~/.local/share/hermesh, …).
	DataDir = "hermesh"
	// DataDirTitle is its name on macOS and Windows (Application Support,
	// %LOCALAPPDATA%).
	DataDirTitle = "Hermesh"
	// Daemon, DaemonWindowless and CLI are the programs' names, which its
	// services, logon tasks and agents' hooks run; Daemon also named the
	// machine's database (<Daemon>.db) and the service's task definition
	// (<Daemon>.xml).
	Daemon           = "hermeshd"
	DaemonWindowless = "hermeshdw"
	CLI              = "hermesh"
	// SystemdUnit, LaunchAgent and LogonTask name its background service.
	SystemdUnit = "hermeshd.service"
	LaunchAgent = "com.hermesh.daemon"
	LogonTask   = "Hermesh"
	// Branch is the prefix of its sessions' git branches (hermesh/<session>).
	Branch = "hermesh/"
	// DSHBridgeDir is where its plugin in DSH Desktop meets the machine, in
	// the DSH home, while that plugin is the one installed there.
	DSHBridgeDir = "hermesh-bridge"
	// DSHRequest starts the request id of a message it sent DSH, as DSH's
	// session logs keep it.
	DSHRequest = "hermesh-"
	// EnvPrefix starts the environment variables it read (HERMESH_X).
	EnvPrefix = "HERMESH_"
)
