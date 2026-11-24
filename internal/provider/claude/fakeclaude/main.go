// Command fakeclaude is a stand-in Claude Code for end-to-end tests of
// signing it in from Aide (工单 #118), on every platform. Its state is in
// FAKE_CLAUDE_DIR: signed in while signed-in is there. Its `auth login`
// prints the address to open, as Claude Code does when no browser opens,
// reads the code the page would show from its input, and signs in only with
// "good-code"; any other fails, "OAuth error: Invalid code". Anything else
// it is asked (its models, its usage) it answers with nothing.
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// URL is the address its sign-in prints (a placeholder host).
const URL = "https://sign-in.example/oauth/authorize?code=true&client_id=aide-e2e&state=batchk"

func main() {
	dir := os.Getenv("FAKE_CLAUDE_DIR")
	signed := filepath.Join(dir, "signed-in")
	switch strings.Join(os.Args[1:], " ") {
	case "--version":
		fmt.Println("2.1.330 (Claude Code)")
	case "auth status":
		_, err := os.Stat(signed)
		fmt.Printf("{\"loggedIn\":%v}\n", err == nil)
	case "auth login":
		fmt.Print("Browser didn't open? Use the url below to sign in\n\n" + URL + "\n\nPaste code here if prompted > ")
		code, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(code) != "good-code" {
			fmt.Fprintln(os.Stderr, "OAuth error: Invalid code")
			os.Exit(1)
		}
		if err := os.WriteFile(signed, nil, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("Login successful.")
	}
}
