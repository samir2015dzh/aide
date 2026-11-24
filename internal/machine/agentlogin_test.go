package machine

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	machinev1 "github.com/samir2015dzh/aide/gen/go/aide/machine/v1"
	"github.com/samir2015dzh/aide/internal/provider"
	"github.com/samir2015dzh/aide/internal/provider/claude"
)

// fakeClaudeDir, set, makes this test binary a stand-in Claude Code (工单
// #118): signed in while <dir>/signed-in is there; `auth login` does what
// <dir>/mode says — "url" prints its address as the real one does when no
// browser opens, then reads the code and signs in only with "good-code";
// "silent" prints nothing and waits, as one whose browser opened on a
// desktop nobody sees.
const fakeClaudeDir = "AIDE_TEST_FAKE_CLAUDE_DIR"

// loginAddress is the address the stand-in prints (a placeholder host).
const loginAddress = "https://sign-in.example/oauth/authorize?code=true&client_id=x&state=abc"

func init() {
	dir := os.Getenv(fakeClaudeDir)
	if dir == "" {
		return
	}
	args := strings.Join(os.Args[1:], " ")
	switch {
	case args == "--version":
		fmt.Println("2.1.330 (Claude Code)")
	case args == "auth status":
		_, err := os.Stat(filepath.Join(dir, "signed-in"))
		fmt.Printf(`{"loggedIn":%v}`+"\n", err == nil)
	case args == "auth login":
		mode, _ := os.ReadFile(filepath.Join(dir, "mode"))
		switch strings.TrimSpace(string(mode)) {
		case "url":
			fmt.Print("Opening browser to sign in…\nBrowser didn't open? Use the url below to sign in (c to copy)\n\n")
			fmt.Print(loginAddress + "\n\nPaste code here if prompted > ")
			code, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if strings.TrimSpace(code) != "good-code" {
				fmt.Fprintln(os.Stderr, "OAuth error: Invalid code")
				os.Exit(1)
			}
			os.WriteFile(filepath.Join(dir, "signed-in"), nil, 0o600)
			fmt.Println("Login successful.")
		case "silent":
			time.Sleep(time.Minute)
		}
	}
	os.Exit(0)
}

// TestClaudeLoginByAddress (工单 #118): a sign-in from Aide never claims a
// browser it cannot see. It hands the owner the address Claude Code printed
// (only the owner: an agent's view of the machine has none), takes back the
// code the page showed, gives it to the sign-in, and is signed in only when
// Claude Code then says so; a wrong code fails, said why; one that prints no
// address is stopped and failed in time, said why, not left waiting; a code
// with no sign-in under way is refused.
func TestClaudeLoginByAddress(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(fakeClaudeDir, dir)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := newHarnessWith(t, DefaultLimits(), map[string]provider.Provider{"claude": &claude.Provider{Binary: self}})
	mode := func(m string) { os.WriteFile(filepath.Join(dir, "mode"), []byte(m), 0o600) }
	login := func() *machinev1.AgentLogin {
		h.n.mu.Lock()
		defer h.n.mu.Unlock()
		return h.n.loginOfLocked("claude")
	}
	until := func(what string, ok func(*machinev1.AgentLogin) bool) *machinev1.AgentLogin {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if l := login(); l != nil && ok(l) {
				return l
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("%s: %v", what, login())
		return nil
	}
	waiting := machinev1.AgentLoginState_AGENT_LOGIN_STATE_WAITING

	if _, err := h.n.StartAgentLogin("claude", false, "good-code"); err == nil {
		t.Fatal("a code with no sign-in under way was taken")
	}

	// A wrong code: failed, in its own words.
	mode("url")
	if _, err := h.n.StartAgentLogin("claude", false); err != nil {
		t.Fatal(err)
	}
	l := until("its address", func(l *machinev1.AgentLogin) bool { return l.Url != "" })
	if l.State != waiting || l.Url != loginAddress || l.CodeSent {
		t.Fatalf("waiting for the owner: %v", l)
	}
	// The owner's alone: an agent's view of the machine carries no sign-in.
	for _, a := range h.n.statusFor(&Principal{Kind: PrincipalClient}).Agents {
		if a.Name == "claude" && a.Login != nil {
			t.Fatalf("a client sees the sign-in: %v", a.Login)
		}
	}
	if l, err := h.n.StartAgentLogin("claude", false, "bad-code"); err != nil || !l.CodeSent {
		t.Fatalf("code given: %v %v", l, err)
	}
	until("failed", func(l *machinev1.AgentLogin) bool {
		return l.State == machinev1.AgentLoginState_AGENT_LOGIN_STATE_FAILED && strings.Contains(l.Detail, "Invalid code")
	})

	// The right code: signed in, as Claude Code itself then says.
	if _, err := h.n.StartAgentLogin("claude", false); err != nil {
		t.Fatal(err)
	}
	until("its address again", func(l *machinev1.AgentLogin) bool { return l.State == waiting && l.Url != "" })
	if _, err := h.n.StartAgentLogin("claude", false, "  good-code \n"); err != nil {
		t.Fatal(err)
	}
	until("signed in", func(l *machinev1.AgentLogin) bool {
		return l.State == machinev1.AgentLoginState_AGENT_LOGIN_STATE_SUCCEEDED && l.Url == ""
	})
	if !h.n.Discover(context.Background())["claude"].LoggedIn {
		t.Fatal("not signed in after the sign-in succeeded")
	}

	// No address printed: stopped and failed within its wait, said why.
	os.Remove(filepath.Join(dir, "signed-in"))
	mode("silent")
	defer func(w time.Duration) { LoginURLWait = w }(LoginURLWait)
	LoginURLWait = 2 * time.Second
	if _, err := h.n.StartAgentLogin("claude", false); err != nil {
		t.Fatal(err)
	}
	until("failed without an address", func(l *machinev1.AgentLogin) bool {
		return l.State == machinev1.AgentLoginState_AGENT_LOGIN_STATE_FAILED && l.Detail == loginNoURL
	})

	// Cancelled while it waits for the code: cancelled, the process ended.
	mode("url")
	if _, err := h.n.StartAgentLogin("claude", false); err != nil {
		t.Fatal(err)
	}
	until("its address to cancel", func(l *machinev1.AgentLogin) bool { return l.State == waiting && l.Url != "" })
	if _, err := h.n.StartAgentLogin("claude", true); err != nil {
		t.Fatal(err)
	}
	until("cancelled", func(l *machinev1.AgentLogin) bool {
		return l.State == machinev1.AgentLoginState_AGENT_LOGIN_STATE_CANCELLED
	})
}
