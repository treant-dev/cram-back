package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/repository"
)

// fakeTokenRepo stores tokens by hash and records what it was asked to do, so tests can assert
// on effects the return value alone does not show (e.g. that a lookup did not write).
type fakeTokenRepo struct {
	byHash    map[string]*model.TokenOwner
	created   []model.PersonalAccessToken
	revoked   []string
	touched   []string
	trimmedTo     int
	trimProtect   time.Duration
	deletedBefore *time.Time
	failsWith     error
}

func newFakeTokenRepo() *fakeTokenRepo {
	return &fakeTokenRepo{byHash: map[string]*model.TokenOwner{}}
}

func (f *fakeTokenRepo) Create(_ context.Context, userID, name, tokenHash, scope string, expiresAt *time.Time) (*model.PersonalAccessToken, error) {
	if f.failsWith != nil {
		return nil, f.failsWith
	}
	t := model.PersonalAccessToken{
		ID: "tok-" + name, UserID: userID, Name: name, Scope: scope,
		CreatedAt: time.Now(), ExpiresAt: expiresAt,
	}
	f.created = append(f.created, t)
	f.byHash[tokenHash] = &model.TokenOwner{Token: t, Email: "dev@example.com", Role: "user"}
	return &t, nil
}

func (f *fakeTokenRepo) CountLive(_ context.Context, userID string) (int, error) {
	n := 0
	for _, t := range f.created {
		if t.UserID == userID && t.RevokedAt == nil {
			n++
		}
	}
	return n, nil
}

func (f *fakeTokenRepo) CountCreatedSince(_ context.Context, userID string, since time.Time) (int, error) {
	n := 0
	for _, t := range f.created {
		if t.UserID == userID && !t.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (f *fakeTokenRepo) TrimRevoked(_ context.Context, userID string, keep int, protect time.Duration) error {
	f.trimmedTo, f.trimProtect = keep, protect
	return nil
}

func (f *fakeTokenRepo) DeleteRevokedBefore(_ context.Context, cutoff time.Time) (int64, error) {
	f.deletedBefore = &cutoff
	return 0, nil
}

func (f *fakeTokenRepo) ListByUser(_ context.Context, userID string) ([]model.PersonalAccessToken, error) {
	var out []model.PersonalAccessToken
	for _, t := range f.created {
		if t.UserID == userID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeTokenRepo) GetByHash(_ context.Context, tokenHash string) (*model.TokenOwner, error) {
	owner, ok := f.byHash[tokenHash]
	if !ok {
		return nil, repository.ErrTokenNotFound
	}
	return owner, nil
}

func (f *fakeTokenRepo) Revoke(_ context.Context, tokenID, userID string) error {
	for i := range f.created {
		if f.created[i].ID == tokenID && f.created[i].UserID == userID {
			f.revoked = append(f.revoked, tokenID)
			now := time.Now()
			f.created[i].RevokedAt = &now
			for _, owner := range f.byHash {
				if owner.Token.ID == tokenID {
					owner.Token.RevokedAt = &now
				}
			}
			return nil
		}
	}
	return repository.ErrTokenNotFound
}

func (f *fakeTokenRepo) TouchLastUsed(_ context.Context, tokenID string) error {
	f.touched = append(f.touched, tokenID)
	return nil
}

func mint(t *testing.T, svc *TokenService, scope string) string {
	t.Helper()
	_, plaintext, err := svc.Create(context.Background(), "user-1", "test", scope, nil)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	return plaintext
}

func TestCreateTokenShape(t *testing.T) {
	repo := newFakeTokenRepo()
	svc := NewTokenService(repo)

	token, plaintext, err := svc.Create(context.Background(), "user-1", "  my laptop  ", model.ScopeRead, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(plaintext, TokenPrefix) {
		t.Errorf("plaintext %q lacks prefix %q", plaintext, TokenPrefix)
	}
	if token.Name != "my laptop" {
		t.Errorf("name not trimmed: %q", token.Name)
	}
	// The stored key must be the hash, never the plaintext.
	if _, ok := repo.byHash[plaintext]; ok {
		t.Error("repo was given the plaintext token as its key")
	}
	if _, ok := repo.byHash[HashToken(plaintext)]; !ok {
		t.Error("repo was not given the token hash")
	}
	// Two tokens must not collide.
	_, second, _ := svc.Create(context.Background(), "user-1", "other", model.ScopeRead, nil)
	if second == plaintext {
		t.Error("two tokens minted with identical values")
	}
}

func TestCreateTokenValidation(t *testing.T) {
	svc := NewTokenService(newFakeTokenRepo())
	cases := []struct {
		name, tokenName, scope string
		want                   error
	}{
		{"empty name", "   ", model.ScopeRead, ErrTokenName},
		{"name too long", strings.Repeat("x", maxTokenNameLen+1), model.ScopeRead, ErrTokenName},
		{"unknown scope", "ok", "admin", ErrInvalidScope},
		{"empty scope", "ok", "", ErrInvalidScope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := svc.Create(context.Background(), "user-1", tc.tokenName, tc.scope, nil); !errors.Is(err, tc.want) {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAuthenticateValidToken(t *testing.T) {
	repo := newFakeTokenRepo()
	svc := NewTokenService(repo)
	plaintext := mint(t, svc, model.ScopeReadWrite)

	owner, err := svc.Authenticate(context.Background(), plaintext)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if owner.Token.UserID != "user-1" || owner.Email != "dev@example.com" || owner.Role != "user" {
		t.Errorf("claims fields wrong: %+v", owner)
	}
}

func TestAuthenticateRejects(t *testing.T) {
	t.Run("unknown token", func(t *testing.T) {
		svc := NewTokenService(newFakeTokenRepo())
		if _, err := svc.Authenticate(context.Background(), TokenPrefix+"nonexistent"); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("got %v, want ErrInvalidToken", err)
		}
	})

	t.Run("missing prefix short-circuits the lookup", func(t *testing.T) {
		repo := newFakeTokenRepo()
		svc := NewTokenService(repo)
		plaintext := mint(t, svc, model.ScopeRead)
		stripped := strings.TrimPrefix(plaintext, TokenPrefix)
		if _, err := svc.Authenticate(context.Background(), stripped); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("got %v, want ErrInvalidToken", err)
		}
	})

	t.Run("revoked token", func(t *testing.T) {
		repo := newFakeTokenRepo()
		svc := NewTokenService(repo)
		plaintext := mint(t, svc, model.ScopeReadWrite)
		if err := svc.Revoke(context.Background(), "tok-test", "user-1"); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if _, err := svc.Authenticate(context.Background(), plaintext); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("revoked token still authenticates: %v", err)
		}
	})

	t.Run("expired token", func(t *testing.T) {
		repo := newFakeTokenRepo()
		svc := NewTokenService(repo)
		past := time.Now().Add(-time.Hour)
		_, plaintext, err := svc.Create(context.Background(), "user-1", "expired", model.ScopeRead, &past)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		// The repo query filters expired tokens in SQL; the fake does not, which is exactly
		// what makes this a test of the service's own guard.
		if _, err := svc.Authenticate(context.Background(), plaintext); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expired token still authenticates: %v", err)
		}
	})
}

func TestRevokeUnknownTokenIsNotFound(t *testing.T) {
	svc := NewTokenService(newFakeTokenRepo())
	if err := svc.Revoke(context.Background(), "tok-missing", "user-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestRevokeIsScopedToOwner(t *testing.T) {
	repo := newFakeTokenRepo()
	svc := NewTokenService(repo)
	mint(t, svc, model.ScopeRead)

	if err := svc.Revoke(context.Background(), "tok-test", "someone-else"); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user revoked this token: %v", err)
	}
	if len(repo.revoked) != 0 {
		t.Errorf("repo performed a revoke for a non-owner: %v", repo.revoked)
	}
}

func TestCanWrite(t *testing.T) {
	if CanWrite(model.ScopeRead) {
		t.Error("read scope must not permit writes")
	}
	if !CanWrite(model.ScopeReadWrite) {
		t.Error("read_write scope must permit writes")
	}
}

func TestLiveTokenLimit(t *testing.T) {
	repo := newFakeTokenRepo()
	svc := NewTokenService(repo)
	ctx := context.Background()

	for i := 0; i < MaxLiveTokens; i++ {
		if _, _, err := svc.Create(ctx, "user-1", fmt.Sprintf("t%d", i), model.ScopeRead, nil); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if _, _, err := svc.Create(ctx, "user-1", "one too many", model.ScopeRead, nil); !errors.Is(err, ErrTooManyTokens) {
		t.Fatalf("got %v, want ErrTooManyTokens", err)
	}

	// The limit counts live tokens, so revoking one makes room again — otherwise a user who
	// rotates tokens would be locked out by their own history.
	if err := svc.Revoke(ctx, "tok-t0", "user-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, _, err := svc.Create(ctx, "user-1", "after revoke", model.ScopeRead, nil); err != nil {
		t.Errorf("create after revoking one: %v", err)
	}

	// Another user is unaffected by this one's tokens.
	if _, _, err := svc.Create(ctx, "user-2", "theirs", model.ScopeRead, nil); err != nil {
		t.Errorf("other user blocked by our limit: %v", err)
	}
}

func TestRevokeTrimsHistory(t *testing.T) {
	repo := newFakeTokenRepo()
	svc := NewTokenService(repo)
	ctx := context.Background()

	if _, _, err := svc.Create(ctx, "user-1", "test", model.ScopeRead, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Revoke(ctx, "tok-test", "user-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if repo.trimmedTo != maxRevokedKept {
		t.Errorf("history trimmed to %d, want %d", repo.trimmedTo, maxRevokedKept)
	}
}

func TestCleanupRevokedUsesTheRetentionWindow(t *testing.T) {
	repo := newFakeTokenRepo()
	svc := NewTokenService(repo)

	if _, err := svc.CleanupRevoked(context.Background()); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if repo.deletedBefore == nil {
		t.Fatal("cleanup did not delete anything")
	}
	age := time.Since(*repo.deletedBefore)
	if age < revokedRetention-time.Minute || age > revokedRetention+time.Minute {
		t.Errorf("cutoff is %v old, want ~%v", age, revokedRetention)
	}
}

func TestDailyIssuanceLimit(t *testing.T) {
	repo := newFakeTokenRepo()
	svc := NewTokenService(repo)
	ctx := context.Background()

	// Create and revoke in a loop: revoked tokens still count, or the cap would be trivially
	// bypassed by freeing a slot after every create.
	for i := 0; i < MaxTokensPerDay; i++ {
		name := fmt.Sprintf("t%d", i)
		if _, _, err := svc.Create(ctx, "user-1", name, model.ScopeRead, nil); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if err := svc.Revoke(ctx, "tok-"+name, "user-1"); err != nil {
			t.Fatalf("revoke %d: %v", i, err)
		}
	}
	if _, _, err := svc.Create(ctx, "user-1", "one too many", model.ScopeRead, nil); !errors.Is(err, ErrTokenRateLimited) {
		t.Errorf("got %v, want ErrTokenRateLimited", err)
	}

	// Yesterday's tokens are outside the window.
	repo.created[0].CreatedAt = time.Now().Add(-25 * time.Hour)
	if _, _, err := svc.Create(ctx, "user-1", "next day", model.ScopeRead, nil); err != nil {
		t.Errorf("a token that aged out of the window still counted: %v", err)
	}

	// And the cap is per user.
	if _, _, err := svc.Create(ctx, "user-2", "theirs", model.ScopeRead, nil); err != nil {
		t.Errorf("other user hit our daily cap: %v", err)
	}
}

// Trimming must not delete the rows the daily cap is counted from.
func TestTrimProtectsTheIssuanceWindow(t *testing.T) {
	repo := newFakeTokenRepo()
	svc := NewTokenService(repo)
	ctx := context.Background()

	if _, _, err := svc.Create(ctx, "user-1", "test", model.ScopeRead, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := svc.Revoke(ctx, "tok-test", "user-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if repo.trimProtect != issuanceWindow {
		t.Errorf("trim protects %v, want the issuance window %v", repo.trimProtect, issuanceWindow)
	}
}
