package main

import "testing"

// The split matters: everything here is reached by third-party clients from a browser, except
// /oauth/approve — the one OAuth route that acts on the user's session cookie. Opening that one
// would let any page make a signed-in browser approve a client the attacker just registered.
func TestOpenToAnyOrigin(t *testing.T) {
	open := []string{
		"/mcp",
		"/mcp/messages",
		"/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-authorization-server",
		"/oauth/register",
		"/oauth/token",
		"/oauth/authorize",
	}
	for _, p := range open {
		if !openToAnyOrigin(p) {
			t.Errorf("%s should be callable from any origin", p)
		}
	}

	closed := []string{
		"/oauth/approve",
		"/account/tokens",
		"/account/connections",
		"/collections",
		"/auth/me",
		"/",
	}
	for _, p := range closed {
		if openToAnyOrigin(p) {
			t.Errorf("%s must stay on the origin allowlist", p)
		}
	}
}
