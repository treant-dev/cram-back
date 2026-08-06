package model

import "time"

// OAuthClient is a client that registered itself (RFC 7591). MCP clients cannot be provisioned
// by hand, so registration is open — which is why a client is nothing but a name and a set of
// redirect URIs until a user actually grants it something.
type OAuthClient struct {
	ID           string
	Name         string
	RedirectURIs []string
	Scope        string
	CreatedAt    time.Time
	LastUsedAt   *time.Time
}

// Allows reports whether uri is one of the client's registered redirect URIs. Comparison is
// exact: prefix or substring matching is the classic way to turn an authorization server into
// an open redirector.
func (c *OAuthClient) Allows(uri string) bool {
	for _, u := range c.RedirectURIs {
		if u == uri {
			return true
		}
	}
	return false
}

// OAuthGrant is one user's standing consent for one client — the unit the settings page shows
// and "disconnect" revokes. Tokens hang off it.
type OAuthGrant struct {
	ID         string
	UserID     string
	ClientID   string
	ClientName string
	Scope      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// OAuthAuthCode is a single-use authorization code with its PKCE challenge.
type OAuthAuthCode struct {
	ClientID      string
	UserID        string
	RedirectURI   string
	CodeChallenge string
	Scope         string
	Resource      string
	ExpiresAt     time.Time
	UsedAt        *time.Time
}

// OAuthToken is an issued access or refresh token, resolved from its hash.
type OAuthToken struct {
	GrantID   string
	UserID    string
	ClientID  string
	Scope     string
	Resource  string
	ExpiresAt time.Time
	UsedAt    *time.Time // refresh tokens only: set once rotated
}
