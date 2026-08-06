package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/repository"
)

// TokenPrefix marks personal access tokens so a leaked string is recognisable in logs and
// greppable in a codebase.
const TokenPrefix = "cram_pat_"

const maxTokenNameLen = 100

// MaxLiveTokens caps how many tokens can authenticate at once. Each one is a standing credential
// pasted into some client's config, so a long list is mostly forgotten copies — the limit forces
// a revoke instead of accumulation.
const MaxLiveTokens = 5

// maxRevokedKept is how much history the list carries. Revoked rows answer "did I revoke it";
// past a handful they are noise.
const maxRevokedKept = 10

// MaxTokensPerDay caps issuance, not survival: revoked tokens count too, or the limit would be
// reset by a create-then-revoke loop.
const MaxTokensPerDay = 10

const issuanceWindow = 24 * time.Hour

// revokedRetention is the other half of the bound: ten rows still linger for years on a quiet
// account, and a week is long enough to answer "did I revoke that yesterday".
const revokedRetention = 7 * 24 * time.Hour

var (
	// ErrInvalidToken covers every reason a presented token is unusable — unknown, revoked,
	// expired. Deliberately indistinguishable to the caller.
	ErrInvalidToken = errors.New("invalid token")
	ErrInvalidScope = errors.New("scope must be 'read' or 'read_write'")
	ErrTooManyTokens = fmt.Errorf("you already have %d active tokens; revoke one before creating another", MaxLiveTokens)
	ErrTokenRateLimited = fmt.Errorf("you have created %d tokens in the last 24 hours; try again later", MaxTokensPerDay)
	ErrTokenName    = errors.New("token name must be non-empty and at most 100 characters")
)

type tokenRepo interface {
	Create(ctx context.Context, userID, name, tokenHash, scope string, expiresAt *time.Time) (*model.PersonalAccessToken, error)
	CountLive(ctx context.Context, userID string) (int, error)
	CountCreatedSince(ctx context.Context, userID string, since time.Time) (int, error)
	TrimRevoked(ctx context.Context, userID string, keep int, protect time.Duration) error
	DeleteRevokedBefore(ctx context.Context, cutoff time.Time) (int64, error)
	ListByUser(ctx context.Context, userID string) ([]model.PersonalAccessToken, error)
	GetByHash(ctx context.Context, tokenHash string) (*model.TokenOwner, error)
	Revoke(ctx context.Context, tokenID, userID string) error
	TouchLastUsed(ctx context.Context, tokenID string) error
}

type TokenService struct {
	tokens tokenRepo
}

func NewTokenService(tokens tokenRepo) *TokenService {
	return &TokenService{tokens: tokens}
}

// HashToken is exported so tests and any future migration tooling hash identically.
func HashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// Create mints a token and returns it in plaintext exactly once, alongside the stored record.
func (s *TokenService) Create(ctx context.Context, userID, name, scope string, expiresAt *time.Time) (*model.PersonalAccessToken, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxTokenNameLen {
		return nil, "", ErrTokenName
	}
	if scope != model.ScopeRead && scope != model.ScopeReadWrite {
		return nil, "", ErrInvalidScope
	}
	live, err := s.tokens.CountLive(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	if live >= MaxLiveTokens {
		return nil, "", ErrTooManyTokens
	}
	issued, err := s.tokens.CountCreatedSince(ctx, userID, time.Now().Add(-issuanceWindow))
	if err != nil {
		return nil, "", err
	}
	if issued >= MaxTokensPerDay {
		return nil, "", ErrTokenRateLimited
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", fmt.Errorf("generate token: %w", err)
	}
	plaintext := TokenPrefix + base64.RawURLEncoding.EncodeToString(raw)

	token, err := s.tokens.Create(ctx, userID, name, HashToken(plaintext), scope, expiresAt)
	if err != nil {
		return nil, "", err
	}
	return token, plaintext, nil
}

func (s *TokenService) List(ctx context.Context, userID string) ([]model.PersonalAccessToken, error) {
	return s.tokens.ListByUser(ctx, userID)
}

func (s *TokenService) Revoke(ctx context.Context, tokenID, userID string) error {
	if err := s.tokens.Revoke(ctx, tokenID, userID); err != nil {
		if errors.Is(err, repository.ErrTokenNotFound) {
			return ErrNotFound
		}
		return err
	}
	// Bounded history: trimming here keeps the list self-maintaining, with no cleanup job to
	// forget about. Best effort — a failed trim must not fail the revoke the user asked for.
	_ = s.tokens.TrimRevoked(ctx, userID, maxRevokedKept, issuanceWindow)
	return nil
}

// Authenticate resolves a presented plaintext token to its owner. Returns ErrInvalidToken for
// anything unusable, without saying which reason — a caller has no legitimate use for the
// difference, and an attacker does.
func (s *TokenService) Authenticate(ctx context.Context, plaintext string) (*model.TokenOwner, error) {
	if !strings.HasPrefix(plaintext, TokenPrefix) {
		return nil, ErrInvalidToken
	}
	owner, err := s.tokens.GetByHash(ctx, HashToken(plaintext))
	if err != nil {
		if errors.Is(err, repository.ErrTokenNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	// Belt and braces: the SQL already filters these, but a repo change must not silently
	// widen what counts as a live token.
	if owner.Token.RevokedAt != nil {
		return nil, ErrInvalidToken
	}
	if owner.Token.ExpiresAt != nil && !owner.Token.ExpiresAt.After(time.Now()) {
		return nil, ErrInvalidToken
	}
	return owner, nil
}

// TouchLastUsed is bookkeeping only; failures are not worth failing a request over.
func (s *TokenService) TouchLastUsed(ctx context.Context, tokenID string) {
	_ = s.tokens.TouchLastUsed(ctx, tokenID)
}

// CleanupRevoked drops revoked tokens past the retention window. Called on a timer.
func (s *TokenService) CleanupRevoked(ctx context.Context) (int64, error) {
	return s.tokens.DeleteRevokedBefore(ctx, time.Now().Add(-revokedRetention))
}

// CanWrite reports whether a scope permits mutations.
func CanWrite(scope string) bool { return scope == model.ScopeReadWrite }
