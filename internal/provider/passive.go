package provider

import "context"

// Checks are passive (工单 #112, after t3code's providers rule): a check of a
// coding agent reads what needs no agent started — its version, its own
// sign-in status command, its files — and never opens a session, starts an
// agent's MCP servers or hooks, or begins a sign-in. What only a running
// agent can tell (the models it offers here) is asked only when the owner
// checks on purpose (Check Again), or once for an agent (a version) never
// asked before; every other check keeps what that found.

type onPurposeKey struct{}

// OnPurpose marks ctx as a check the owner asked for, or one that may ask an
// agent what only it can tell.
func OnPurpose(ctx context.Context) context.Context {
	return context.WithValue(ctx, onPurposeKey{}, true)
}

// IsOnPurpose: a check may start the agent to ask it (OnPurpose).
func IsOnPurpose(ctx context.Context) bool {
	on, _ := ctx.Value(onPurposeKey{}).(bool)
	return on
}
