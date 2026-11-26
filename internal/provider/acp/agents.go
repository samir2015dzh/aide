package acp

import (
	"path"
	"path/filepath"
	"runtime"

	"github.com/samir2015dzh/aide/internal/provider"
)

// Agents are the ACP agents Aide knows how to start. Commands follow
// each vendor's documented ACP entry point; flags that pre-approve tools
// are not needed because the gate answers permission requests.
var Agents = []Agent{
	{Name: "gemini", Command: "gemini", Args: []string{"--acp"}},
	antigravityAgent(runtime.GOOS),
	{Name: "cursor", Command: "cursor-agent", Args: []string{"acp"}, DesktopApps: []string{"Cursor.app"}, Auth: []string{"cursor_login"}},
	{Name: "copilot", Command: "copilot", Args: []string{"--acp"}},
	{Name: "opencode", Command: "opencode", Args: []string{"acp"}, Paths: []string{".opencode/bin/opencode"}},
	{Name: "qwen", Command: "qwen", Args: []string{"--acp"}},
	{Name: "goose", Command: "goose", Args: []string{"acp"}},
	{Name: "auggie", Command: "auggie", Args: []string{"--acp"}},
	{Name: "kimi", Command: "kimi", Args: []string{"acp"}},
	{Name: "kiro", Command: "kiro-cli", Args: []string{"acp"}, DesktopApps: []string{"Kiro.app"}, Names: []string{"Kiro"}},
	{Name: "qoder", Command: "qodercli", Args: []string{"--acp"}, DesktopApps: []string{"Qoder.app"}, Names: []string{"Qoder"}},
	// The DSH desktop apps keep their own dsh, with no `dsh` command; the
	// Windows one's script is inside its app.asar (tested on 0.2.1-rc.5),
	// and it is found wherever Windows says it is installed.
	{Name: "deepseek", Command: "dsh", Args: []string{"--profile", "acp"}, DSHPresets: true, Apps: []App{
		{Bundle: "DSH Desktop.app", Exec: "Contents/MacOS/DSH Desktop", Script: "Contents/Resources/app.asar.unpacked/lib/desktop-cli.js"},
		{Bundle: "DeepSeek Harness", Exec: "DeepSeek Harness.exe", Script: "resources/app.asar/dsh/node_modules/@deepseek-ai/dsh/lib/bin.js",
			Windows: provider.WindowsApp{Names: []string{"DeepSeek Harness", "DSH Desktop"}, Exes: []string{"DeepSeek Harness.exe", "DSH Desktop.exe"},
				GUIDs: []string{"1bf39983-50d0-5fe0-9ef4-cece76f67c5e"}}}, // com.deepseek.dsh
	}},
	{Name: "codebuddy", Command: "codebuddy", Args: []string{"--acp"}, Names: []string{"CodeBuddy", "WorkBuddy"}},
	{Name: "grok", Command: "grok", Args: []string{"--no-auto-update", "agent", "stdio"}, Auth: []string{"cached_token", "xai.api_key"}},
}

// Google's ACP distribution is separate from both the desktop app and agy.
// Keep localharness_external beside the server when installing the archive.
func antigravityAgent(goos string) Agent {
	command := "agy_acp_server.par"
	var args []string
	paths := []string{".local/share/antigravity-acp/" + command}
	if goos == "windows" {
		command = "agy_acp_server.exe"
		paths = []string{
			".local/share/antigravity-acp/" + command,
			"AppData/Local/antigravity-acp/" + command,
		}
	} else if goos == "linux" {
		args = []string{"--uid="} // the entry point in Google's ACP registry
	}
	return Agent{Name: "antigravity", Command: command, Args: args,
		Paths:       paths,
		DesktopApps: []string{"Antigravity.app"}}
}

// AntigravityDir is where Aide installs Google's Antigravity ACP server
// (with what its archive keeps beside it) for the built-in agent to find it
// (工单 #133): the last of the places it looks, under home.
func AntigravityDir(home, goos string) string {
	paths := antigravityAgent(goos).Paths
	return filepath.Join(home, filepath.FromSlash(path.Dir(paths[len(paths)-1])))
}

// Providers returns a provider for every known ACP agent.
func Providers(pathEnv string) []*Provider {
	ps := make([]*Provider, len(Agents))
	for i, a := range Agents {
		ps[i] = &Provider{Agent: a, PathEnv: pathEnv}
	}
	return ps
}
