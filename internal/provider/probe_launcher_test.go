package provider

import (
	"context"
	"errors"
	"testing"
)

// A launcher with no agent behind it (VS Code's copilot shim, the agent
// uninstalled) is no agent here: not found, as when nothing is, so the
// app lists it as not installed rather than as one that is here and fails.
// Anything else that fails is still a failure.
func TestWhyFailedTellsALauncherWithNoAgent(t *testing.T) {
	exit := errors.New("exit status 1")
	cases := []struct {
		name, printed string
		want          Problem
	}{
		{"copilot shim", "GitHub Copilot CLI was not found. Run copilot in a terminal to install it, or see\nhttps://docs.github.com/copilot/how-tos/copilot-cli/set-up-copilot-cli/install-copilot-cli\n", ProblemNotFound},
		{"not installed", "The Foo CLI is not installed. To install it, run: npm i -g foo\n", ProblemNotFound},
		{"a real failure", "config.toml: bad value\n", ProblemFailed},
		{"a file not found while running", "error: config file was not found at ~/.foo/config.toml\n", ProblemFailed},
		{"nothing printed", "", ProblemFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if p, d := whyFailed(context.Background(), "", exit, c.printed); p != c.want {
				t.Fatalf("got %v %q, want %v", p, d, c.want)
			}
		})
	}
}
