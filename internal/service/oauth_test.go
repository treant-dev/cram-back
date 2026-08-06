package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/repository"
)

// fakeOAuthRepo keeps everything in memory and records the calls whose *absence* would be the
// bug — a grant that should have been revoked, a token that should not have been issued.
type fakeOAuthRepo struct {
	clients  map[string]*model.OAuthClient
	codes    map[string]*model.OAuthAuthCode
	access   map[string]*model.OAuthToken
	refresh  map[string]*model.OAuthToken
	grants   map[string]*model.OAuthGrant
	revoked  []string
	issuedAT int
}

func newFakeOAuthRepo() *fakeOAuthRepo {
	return &fakeOAuthRepo{
		clients: map[string]*model.OAuthClient{},
		codes:   map[string]*model.OAuthAuthCode{},
		access:  map[string]*model.OAuthToken{},
		refresh: map[string]*model.OAuthToken{},
		grants:  map[string]*model.OAuthGrant{},
	}
}

func (f *fakeOAuthRepo) CreateClient(_ context.Context, id, name string, uris []string, scope string) (*model.OAuthClient, error) {
	c := &model.OAuthClient{ID: id, Name: name, RedirectURIs: uris, Scope: scope}
	f.clients[id] = c
	return c, nil
}

func (f *fakeOAuthRepo) GetClient(_ context.Context, id string) (*model.OAuthClient, error) {
	c, ok := f.clients[id]
	if !ok {
		return nil, repository.ErrOAuthNotFound
	}
	return c, nil
}

func (f *fakeOAuthRepo) CreateAuthCode(_ context.Context, hash string, c model.OAuthAuthCode) error {
	cp := c
	f.codes[hash] = &cp
	return nil
}

func (f *fakeOAuthRepo) ConsumeAuthCode(_ context.Context, hash string) (*model.OAuthAuthCode, error) {
	c, ok := f.codes[hash]
	if !ok {
		return nil, repository.ErrOAuthNotFound
	}
	snapshot := *c
	if c.UsedAt == nil {
		now := time.Now()
		c.UsedAt = &now
	}
	return &snapshot, nil
}

func (f *fakeOAuthRepo) UpsertGrant(_ context.Context, userID, clientID, scope string) (string, error) {
	id := userID + ":" + clientID
	f.grants[id] = &model.OAuthGrant{ID: id, UserID: userID, ClientID: clientID, Scope: scope}
	return id, nil
}

func (f *fakeOAuthRepo) ListGrants(_ context.Context, userID string) ([]model.OAuthGrant, error) {
	var out []model.OAuthGrant
	for _, g := range f.grants {
		if g.UserID == userID && g.RevokedAt == nil {
			out = append(out, *g)
		}
	}
	return out, nil
}

func (f *fakeOAuthRepo) RevokeGrant(_ context.Context, grantID, userID string) error {
	g, ok := f.grants[grantID]
	if !ok || g.UserID != userID {
		return repository.ErrOAuthNotFound
	}
	return f.revoke(grantID)
}

func (f *fakeOAuthRepo) RevokeGrantByID(_ context.Context, grantID string) error { return f.revoke(grantID) }

func (f *fakeOAuthRepo) revoke(grantID string) error {
	now := time.Now()
	if g, ok := f.grants[grantID]; ok {
		g.RevokedAt = &now
	}
	f.revoked = append(f.revoked, grantID)
	for h, t := range f.access {
		if t.GrantID == grantID {
			delete(f.access, h)
		}
	}
	for h, t := range f.refresh {
		if t.GrantID == grantID {
			delete(f.refresh, h)
		}
	}
	return nil
}

func (f *fakeOAuthRepo) CreateAccessToken(_ context.Context, hash, grantID, scope, resource string, exp time.Time) error {
	f.issuedAT++
	f.access[hash] = &model.OAuthToken{GrantID: grantID, Scope: scope, Resource: resource, ExpiresAt: exp}
	return nil
}

func (f *fakeOAuthRepo) CreateRefreshToken(_ context.Context, hash, grantID string, exp time.Time) error {
	g := f.grants[grantID]
	f.refresh[hash] = &model.OAuthToken{GrantID: grantID, UserID: g.UserID, ClientID: g.ClientID, Scope: g.Scope, ExpiresAt: exp}
	return nil
}

func (f *fakeOAuthRepo) GetAccessToken(_ context.Context, hash string) (*model.OAuthToken, error) {
	t, ok := f.access[hash]
	if !ok || time.Now().After(t.ExpiresAt) {
		return nil, repository.ErrOAuthNotFound
	}
	if g, ok := f.grants[t.GrantID]; ok && g.RevokedAt != nil {
		return nil, repository.ErrOAuthNotFound
	}
	return t, nil
}

func (f *fakeOAuthRepo) ConsumeRefreshToken(_ context.Context, hash string) (*model.OAuthToken, error) {
	t, ok := f.refresh[hash]
	if !ok {
		return nil, repository.ErrOAuthNotFound
	}
	snapshot := *t
	if t.UsedAt == nil {
		now := time.Now()
		t.UsedAt = &now
	}
	return &snapshot, nil
}

func (f *fakeOAuthRepo) TouchGrant(context.Context, string) error { return nil }
func (f *fakeOAuthRepo) DeleteExpired(context.Context) error      { return nil }

// ---------- helpers ----------

func pkce() (verifier, challenge string) {
	verifier = "verifier-that-is-long-enough-to-be-realistic-0123456789"
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func setup(t *testing.T) (*OAuthService, *fakeOAuthRepo, *model.OAuthClient) {
	t.Helper()
	repo := newFakeOAuthRepo()
	svc := NewOAuthService(repo)
	client, err := svc.RegisterClient(context.Background(), "Test client", []string{"http://127.0.0.1:1234/cb"}, model.ScopeReadWrite)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return svc, repo, client
}

func approve(t *testing.T, svc *OAuthService, client *model.OAuthClient, challenge string) string {
	t.Helper()
	req, err := svc.ValidateAuthorize(context.Background(), client.ID, "http://127.0.0.1:1234/cb", "code", challenge, "S256", model.ScopeReadWrite, "")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	code, err := svc.Approve(context.Background(), req, "user-1")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	return code
}

// ---------- registration ----------

func TestRegisterClientValidatesRedirects(t *testing.T) {
	svc := NewOAuthService(newFakeOAuthRepo())
	cases := []struct {
		name string
		uris []string
		ok   bool
	}{
		{"https", []string{"https://app.example/cb"}, true},
		{"loopback http", []string{"http://127.0.0.1:1234/cb"}, true},
		{"localhost http", []string{"http://localhost:1234/cb"}, true},
		{"custom scheme", []string{"myapp://callback"}, true},
		{"public http", []string{"http://evil.example/cb"}, false},
		{"with fragment", []string{"https://app.example/cb#x"}, false},
		{"no scheme", []string{"/cb"}, false},
		{"none at all", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.RegisterClient(context.Background(), "c", tc.uris, model.ScopeRead)
			if (err == nil) != tc.ok {
				t.Errorf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

// ---------- authorize ----------

func TestValidateAuthorize(t *testing.T) {
	svc, _, client := setup(t)
	_, challenge := pkce()
	ctx := context.Background()

	t.Run("unregistered redirect_uri", func(t *testing.T) {
		_, err := svc.ValidateAuthorize(ctx, client.ID, "https://evil.example/steal", "code", challenge, "S256", "", "")
		if !errors.Is(err, ErrOAuthInvalidRedirect) {
			t.Errorf("got %v, want ErrOAuthInvalidRedirect", err)
		}
	})

	t.Run("unknown client", func(t *testing.T) {
		_, err := svc.ValidateAuthorize(ctx, "nope", "http://127.0.0.1:1234/cb", "code", challenge, "S256", "", "")
		if !errors.Is(err, ErrOAuthInvalidClient) {
			t.Errorf("got %v, want ErrOAuthInvalidClient", err)
		}
	})

	t.Run("PKCE is mandatory", func(t *testing.T) {
		if _, err := svc.ValidateAuthorize(ctx, client.ID, "http://127.0.0.1:1234/cb", "code", "", "", "", ""); err == nil {
			t.Error("a request without PKCE was accepted")
		}
	})

	t.Run("plain PKCE is refused", func(t *testing.T) {
		if _, err := svc.ValidateAuthorize(ctx, client.ID, "http://127.0.0.1:1234/cb", "code", challenge, "plain", "", ""); err == nil {
			t.Error("code_challenge_method=plain was accepted")
		}
	})

	t.Run("implicit flow is refused", func(t *testing.T) {
		if _, err := svc.ValidateAuthorize(ctx, client.ID, "http://127.0.0.1:1234/cb", "token", challenge, "S256", "", ""); err == nil {
			t.Error("response_type=token was accepted")
		}
	})
}

// ---------- code exchange ----------

func TestExchangeCode(t *testing.T) {
	ctx := context.Background()

	t.Run("happy path", func(t *testing.T) {
		svc, _, client := setup(t)
		verifier, challenge := pkce()
		code := approve(t, svc, client, challenge)

		tokens, err := svc.ExchangeCode(ctx, client.ID, code, "http://127.0.0.1:1234/cb", verifier)
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		if !strings.HasPrefix(tokens.AccessToken, "cram_at_") || !strings.HasPrefix(tokens.RefreshToken, "cram_rt_") {
			t.Errorf("unexpected token shapes: %+v", tokens)
		}
		if tokens.Scope != model.ScopeReadWrite {
			t.Errorf("scope = %q", tokens.Scope)
		}
	})

	t.Run("wrong verifier", func(t *testing.T) {
		svc, repo, client := setup(t)
		_, challenge := pkce()
		code := approve(t, svc, client, challenge)

		if _, err := svc.ExchangeCode(ctx, client.ID, code, "http://127.0.0.1:1234/cb", "some-other-verifier"); !errors.Is(err, ErrOAuthInvalidGrant) {
			t.Errorf("got %v, want ErrOAuthInvalidGrant", err)
		}
		if repo.issuedAT != 0 {
			t.Error("a token was issued despite a failed PKCE check")
		}
	})

	t.Run("another client cannot redeem the code", func(t *testing.T) {
		svc, _, client := setup(t)
		verifier, challenge := pkce()
		code := approve(t, svc, client, challenge)

		if _, err := svc.ExchangeCode(ctx, "someone-else", code, "http://127.0.0.1:1234/cb", verifier); !errors.Is(err, ErrOAuthInvalidGrant) {
			t.Errorf("got %v, want ErrOAuthInvalidGrant", err)
		}
	})

	t.Run("redirect_uri must match the one the code was issued for", func(t *testing.T) {
		svc, _, client := setup(t)
		verifier, challenge := pkce()
		code := approve(t, svc, client, challenge)

		if _, err := svc.ExchangeCode(ctx, client.ID, code, "http://127.0.0.1:9999/other", verifier); !errors.Is(err, ErrOAuthInvalidGrant) {
			t.Errorf("got %v, want ErrOAuthInvalidGrant", err)
		}
	})

	t.Run("replay kills the grant", func(t *testing.T) {
		svc, repo, client := setup(t)
		verifier, challenge := pkce()
		code := approve(t, svc, client, challenge)

		tokens, err := svc.ExchangeCode(ctx, client.ID, code, "http://127.0.0.1:1234/cb", verifier)
		if err != nil {
			t.Fatalf("first exchange: %v", err)
		}
		if _, err := svc.ExchangeCode(ctx, client.ID, code, "http://127.0.0.1:1234/cb", verifier); !errors.Is(err, ErrOAuthInvalidGrant) {
			t.Fatalf("replay: got %v, want ErrOAuthInvalidGrant", err)
		}
		// A code seen twice means someone else has it; refusing the second call is not enough,
		// the tokens from the first must die too.
		if len(repo.revoked) == 0 {
			t.Error("replayed code did not revoke the grant")
		}
		if _, err := svc.Authenticate(ctx, tokens.AccessToken); !errors.Is(err, ErrInvalidToken) {
			t.Error("the access token from the first exchange still works")
		}
	})
}

// ---------- refresh ----------

func TestRefreshRotation(t *testing.T) {
	ctx := context.Background()
	svc, repo, client := setup(t)
	verifier, challenge := pkce()
	code := approve(t, svc, client, challenge)
	first, err := svc.ExchangeCode(ctx, client.ID, code, "http://127.0.0.1:1234/cb", verifier)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	second, err := svc.Refresh(ctx, client.ID, first.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.AccessToken == first.AccessToken || second.RefreshToken == first.RefreshToken {
		t.Error("refresh returned the same values instead of rotating")
	}
	if _, err := svc.Authenticate(ctx, second.AccessToken); err != nil {
		t.Errorf("the rotated access token does not work: %v", err)
	}

	// Replaying the spent refresh token means it leaked: refusing it is not enough, everything
	// issued under the grant has to go, including the tokens the legitimate client holds now.
	if _, err := svc.Refresh(ctx, client.ID, first.RefreshToken); !errors.Is(err, ErrOAuthInvalidGrant) {
		t.Fatalf("replayed refresh: got %v, want ErrOAuthInvalidGrant", err)
	}
	if len(repo.revoked) == 0 {
		t.Error("replayed refresh token did not revoke the grant")
	}
	if _, err := svc.Authenticate(ctx, second.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Error("tokens survived a detected refresh-token leak")
	}
}

// ---------- resource server ----------

func TestAuthenticateAccessToken(t *testing.T) {
	ctx := context.Background()
	svc, _, client := setup(t)
	verifier, challenge := pkce()
	code := approve(t, svc, client, challenge)
	tokens, err := svc.ExchangeCode(ctx, client.ID, code, "http://127.0.0.1:1234/cb", verifier)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	tok, err := svc.Authenticate(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if tok.Scope != model.ScopeReadWrite {
		t.Errorf("scope = %q", tok.Scope)
	}

	t.Run("a refresh token is not an access token", func(t *testing.T) {
		if _, err := svc.Authenticate(ctx, tokens.RefreshToken); !errors.Is(err, ErrInvalidToken) {
			t.Error("a refresh token authenticated against the resource")
		}
	})

	t.Run("a personal access token is not one either", func(t *testing.T) {
		if _, err := svc.Authenticate(ctx, "cram_pat_something"); !errors.Is(err, ErrInvalidToken) {
			t.Error("a PAT was accepted by the OAuth path")
		}
	})

	t.Run("revoking the grant kills the token", func(t *testing.T) {
		grants, _ := svc.ListGrants(ctx, "user-1")
		if len(grants) != 1 {
			t.Fatalf("grants = %d, want 1", len(grants))
		}
		if err := svc.RevokeGrant(ctx, grants[0].ID, "user-1"); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if _, err := svc.Authenticate(ctx, tokens.AccessToken); !errors.Is(err, ErrInvalidToken) {
			t.Error("the token still works after the user disconnected the app")
		}
	})
}

func TestRevokeGrantIsScopedToOwner(t *testing.T) {
	ctx := context.Background()
	svc, _, client := setup(t)
	_, challenge := pkce()
	approve(t, svc, client, challenge)

	grants, _ := svc.ListGrants(ctx, "user-1")
	if err := svc.RevokeGrant(ctx, grants[0].ID, "someone-else"); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user revoked this grant: %v", err)
	}
}

// Re-authorizing must not leave the user with a pile of near-identical entries to revoke.
func TestReapprovalReusesTheGrant(t *testing.T) {
	ctx := context.Background()
	svc, _, client := setup(t)
	_, challenge := pkce()
	approve(t, svc, client, challenge)
	approve(t, svc, client, challenge)

	grants, _ := svc.ListGrants(ctx, "user-1")
	if len(grants) != 1 {
		t.Errorf("grants = %d, want 1 after re-approving the same client", len(grants))
	}
}
