package model

import "time"

// Scopes for a personal access token.
const (
	ScopeRead      = "read"
	ScopeReadWrite = "read_write"
)

// PersonalAccessToken is a long-lived bearer credential for non-browser clients.
// The plaintext value is never stored — only TokenHash.
type PersonalAccessToken struct {
	ID         string
	UserID     string
	Name       string
	Scope      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	RevokedAt  *time.Time
}

// TokenOwner is a token joined with the fields needed to build auth claims.
type TokenOwner struct {
	Token PersonalAccessToken
	Email string
	Role  string
}
