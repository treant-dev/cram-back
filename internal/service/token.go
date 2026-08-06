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

var (
	// ErrInvalidToken covers every reason a presented token is unusable — unknown, revoked,
	// expired. Deliberately indistinguishable to the caller.
	ErrInvalidToken = errors.New("invalid token")
	ErrInvalidScope = errors.New("scope must be 'read' or 'read_write'")
	ErrTokenName    = errors.New("token name must be non-empty and at most 100 characters")
)

type tokenRepo interface {
	Create(ctx context.Context, userID, name, tokenHash, scope string, expiresAt *time.Time) (*model.PersonalAccessToken, error)
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

// CanWrite reports whether a scope permits mutations.
func CanWrite(scope string) bool { return scope == model.ScopeReadWrite }
