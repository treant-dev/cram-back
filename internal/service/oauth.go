package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/repository"
)

// Lifetimes. Access tokens are short because they travel to a third-party client and are
// replayable until they expire; refresh tokens are long but rotate on every use.
const (
	authCodeTTL      = 5 * time.Minute
	accessTokenTTL   = time.Hour
	refreshTokenTTL  = 90 * 24 * time.Hour
	maxRedirectURIs  = 10
	maxClientNameLen = 100
)

// resolveScope reads an OAuth scope parameter, which is a space-delimited set — not a single
// value. We advertise both "read" and "read_write" in scopes_supported, so a client asking for
// everything it saw sends "read read_write"; rejecting that means rejecting our own advertisement.
//
// Unknown entries are ignored rather than refused: a client that also asks for "openid" or
// "profile" is asking for something we simply do not have, which is not a reason to refuse the
// registration. The widest requested Cram scope wins, and nothing recognisable means read.
func resolveScope(requested string) string {
	widest := ""
	for _, s := range strings.Fields(requested) {
		switch s {
		case model.ScopeReadWrite:
			return model.ScopeReadWrite
		case model.ScopeRead:
			widest = model.ScopeRead
		}
	}
	return widest
}

var (
	ErrOAuthInvalidClient   = errors.New("unknown client")
	ErrOAuthInvalidRedirect = errors.New("redirect_uri does not match a registered one")
	ErrOAuthInvalidRequest  = errors.New("invalid request")
	ErrOAuthInvalidGrant    = errors.New("invalid or expired grant")
	ErrOAuthAccessDenied    = errors.New("access denied")
)

type oauthRepo interface {
	CreateClient(ctx context.Context, id, name string, redirectURIs []string, scope string) (*model.OAuthClient, error)
	GetClient(ctx context.Context, id string) (*model.OAuthClient, error)
	CreateAuthCode(ctx context.Context, hash string, c model.OAuthAuthCode) error
	ConsumeAuthCode(ctx context.Context, hash string) (*model.OAuthAuthCode, error)
	UpsertGrant(ctx context.Context, userID, clientID, scope string) (string, error)
	ListGrants(ctx context.Context, userID string) ([]model.OAuthGrant, error)
	RevokeGrant(ctx context.Context, grantID, userID string) error
	RevokeGrantByID(ctx context.Context, grantID string) error
	CreateAccessToken(ctx context.Context, hash, grantID, scope, resource string, expiresAt time.Time) error
	CreateRefreshToken(ctx context.Context, hash, grantID string, expiresAt time.Time) error
	GetAccessToken(ctx context.Context, hash string) (*model.OAuthToken, error)
	ConsumeRefreshToken(ctx context.Context, hash string) (*model.OAuthToken, error)
	TouchGrant(ctx context.Context, grantID string) error
	DeleteExpired(ctx context.Context) error
}

type OAuthService struct {
	repo oauthRepo
}

func NewOAuthService(repo oauthRepo) *OAuthService { return &OAuthService{repo: repo} }

func hash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomToken(prefix string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// ---------- registration ----------

// RegisterClient implements dynamic client registration. Registration is open, because MCP
// clients have no way to be provisioned in advance — which is safe only because a client can do
// nothing at all until a signed-in user approves it on the consent screen.
func (s *OAuthService) RegisterClient(ctx context.Context, name string, redirectURIs []string, scope string) (*model.OAuthClient, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Unnamed client"
	}
	if len(name) > maxClientNameLen {
		name = name[:maxClientNameLen]
	}
	if len(redirectURIs) == 0 || len(redirectURIs) > maxRedirectURIs {
		return nil, fmt.Errorf("%w: between 1 and %d redirect_uris are required", ErrOAuthInvalidRequest, maxRedirectURIs)
	}
	for _, raw := range redirectURIs {
		if err := validateRedirectURI(raw); err != nil {
			return nil, err
		}
	}
	scope = resolveScope(scope)
	if scope == "" {
		scope = model.ScopeRead
	}

	id, err := randomToken("cram_client_")
	if err != nil {
		return nil, err
	}
	return s.repo.CreateClient(ctx, id, name, redirectURIs, scope)
}

// validateRedirectURI keeps the obvious footguns out: no fragments (they are invisible to the
// server and can smuggle a second target), and plain http only for loopback, which is how
// desktop clients receive the callback.
func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: redirect_uri %q is not a URL", ErrOAuthInvalidRequest, raw)
	}
	if u.Fragment != "" {
		return fmt.Errorf("%w: redirect_uri must not contain a fragment", ErrOAuthInvalidRequest)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "127.0.0.1" || host == "::1" || host == "localhost" {
			return nil
		}
		return fmt.Errorf("%w: plain http is only allowed for loopback redirect_uris", ErrOAuthInvalidRequest)
	case "":
		return fmt.Errorf("%w: redirect_uri needs a scheme", ErrOAuthInvalidRequest)
	default:
		// Custom schemes (myapp://) are how native apps get called back.
		return nil
	}
}

func (s *OAuthService) GetClient(ctx context.Context, id string) (*model.OAuthClient, error) {
	c, err := s.repo.GetClient(ctx, id)
	if errors.Is(err, repository.ErrOAuthNotFound) {
		return nil, ErrOAuthInvalidClient
	}
	return c, err
}

// ---------- authorization ----------

// AuthorizeRequest is a validated /authorize call, ready to be shown on the consent screen.
type AuthorizeRequest struct {
	Client        *model.OAuthClient
	RedirectURI   string
	State         string
	CodeChallenge string
	Scope         string
	Resource      string
}

// ValidateAuthorize checks everything that must be right before a human is asked anything.
//
// The order matters: client and redirect_uri are verified first, because every later error is
// reported by redirecting to that URI — sending an error to an unverified redirect target is how
// authorization servers become open redirectors.
func (s *OAuthService) ValidateAuthorize(ctx context.Context, clientID, redirectURI, responseType, codeChallenge, codeChallengeMethod, scope, resource string) (*AuthorizeRequest, error) {
	client, err := s.GetClient(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if redirectURI == "" {
		if len(client.RedirectURIs) != 1 {
			return nil, fmt.Errorf("%w: redirect_uri is required", ErrOAuthInvalidRequest)
		}
		redirectURI = client.RedirectURIs[0]
	}
	if !client.Allows(redirectURI) {
		return nil, ErrOAuthInvalidRedirect
	}
	if responseType != "code" {
		return nil, fmt.Errorf("%w: only response_type=code is supported", ErrOAuthInvalidRequest)
	}
	// OAuth 2.1: PKCE is mandatory, and only S256 — "plain" adds nothing against an attacker
	// who can read the request.
	if codeChallengeMethod != "S256" {
		return nil, fmt.Errorf("%w: code_challenge_method must be S256", ErrOAuthInvalidRequest)
	}
	if len(codeChallenge) < 43 {
		return nil, fmt.Errorf("%w: code_challenge is missing or too short", ErrOAuthInvalidRequest)
	}
	// Same list handling as at registration. An authorize request asking only for scopes we do
	// not have falls back to what the client registered with, rather than failing the flow.
	if resolved := resolveScope(scope); resolved != "" {
		scope = resolved
	} else {
		scope = client.Scope
	}

	return &AuthorizeRequest{
		Client: client, RedirectURI: redirectURI, CodeChallenge: codeChallenge, Scope: scope, Resource: resource,
	}, nil
}

// Approve records the user's consent and mints a one-time authorization code.
func (s *OAuthService) Approve(ctx context.Context, req *AuthorizeRequest, userID string) (string, error) {
	if _, err := s.repo.UpsertGrant(ctx, userID, req.Client.ID, req.Scope); err != nil {
		return "", err
	}
	code, err := randomToken("cram_code_")
	if err != nil {
		return "", err
	}
	err = s.repo.CreateAuthCode(ctx, hash(code), model.OAuthAuthCode{
		ClientID:      req.Client.ID,
		UserID:        userID,
		RedirectURI:   req.RedirectURI,
		CodeChallenge: req.CodeChallenge,
		Scope:         req.Scope,
		Resource:      req.Resource,
		ExpiresAt:     time.Now().Add(authCodeTTL),
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// ---------- token endpoint ----------

// TokenSet is what /token returns.
type TokenSet struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	Scope        string
}

// ExchangeCode redeems an authorization code for tokens.
func (s *OAuthService) ExchangeCode(ctx context.Context, clientID, code, redirectURI, codeVerifier string) (*TokenSet, error) {
	if code == "" || codeVerifier == "" {
		return nil, fmt.Errorf("%w: code and code_verifier are required", ErrOAuthInvalidRequest)
	}
	rec, err := s.repo.ConsumeAuthCode(ctx, hash(code))
	if errors.Is(err, repository.ErrOAuthNotFound) {
		return nil, ErrOAuthInvalidGrant
	}
	if err != nil {
		return nil, err
	}

	// A code presented twice means someone other than the client has it. Kill the grant rather
	// than merely refusing: the legitimate client can ask the user to reconnect.
	if rec.UsedAt != nil {
		if grantID, gerr := s.repo.UpsertGrant(ctx, rec.UserID, rec.ClientID, rec.Scope); gerr == nil {
			_ = s.repo.RevokeGrantByID(ctx, grantID)
		}
		return nil, ErrOAuthInvalidGrant
	}
	if time.Now().After(rec.ExpiresAt) || rec.ClientID != clientID {
		return nil, ErrOAuthInvalidGrant
	}
	// redirect_uri must match the one the code was issued for (RFC 6749 §4.1.3).
	if redirectURI != "" && redirectURI != rec.RedirectURI {
		return nil, ErrOAuthInvalidGrant
	}
	// PKCE: S256(verifier) must equal the stored challenge.
	sum := sha256.Sum256([]byte(codeVerifier))
	if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(rec.CodeChallenge)) != 1 {
		return nil, ErrOAuthInvalidGrant
	}

	grantID, err := s.repo.UpsertGrant(ctx, rec.UserID, rec.ClientID, rec.Scope)
	if err != nil {
		return nil, err
	}
	return s.issue(ctx, grantID, rec.Scope, rec.Resource)
}

// Refresh rotates a refresh token: the presented one is spent and a new pair is issued.
func (s *OAuthService) Refresh(ctx context.Context, clientID, refreshToken string) (*TokenSet, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("%w: refresh_token is required", ErrOAuthInvalidRequest)
	}
	rec, err := s.repo.ConsumeRefreshToken(ctx, hash(refreshToken))
	if errors.Is(err, repository.ErrOAuthNotFound) {
		return nil, ErrOAuthInvalidGrant
	}
	if err != nil {
		return nil, err
	}
	// Reuse of a rotated token means the value leaked — the holder of the current one cannot be
	// distinguished from the thief, so the whole grant goes.
	if rec.UsedAt != nil {
		_ = s.repo.RevokeGrantByID(ctx, rec.GrantID)
		return nil, ErrOAuthInvalidGrant
	}
	if time.Now().After(rec.ExpiresAt) || rec.ClientID != clientID {
		return nil, ErrOAuthInvalidGrant
	}
	return s.issue(ctx, rec.GrantID, rec.Scope, "")
}

func (s *OAuthService) issue(ctx context.Context, grantID, scope, resource string) (*TokenSet, error) {
	access, err := randomToken("cram_at_")
	if err != nil {
		return nil, err
	}
	refresh, err := randomToken("cram_rt_")
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateAccessToken(ctx, hash(access), grantID, scope, resource, time.Now().Add(accessTokenTTL)); err != nil {
		return nil, err
	}
	if err := s.repo.CreateRefreshToken(ctx, hash(refresh), grantID, time.Now().Add(refreshTokenTTL)); err != nil {
		return nil, err
	}
	return &TokenSet{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(accessTokenTTL.Seconds()),
		Scope:        scope,
	}, nil
}

// ---------- resource server ----------

// Authenticate resolves an access token presented to a protected resource.
func (s *OAuthService) Authenticate(ctx context.Context, accessToken string) (*model.OAuthToken, error) {
	if !strings.HasPrefix(accessToken, "cram_at_") {
		return nil, ErrInvalidToken
	}
	t, err := s.repo.GetAccessToken(ctx, hash(accessToken))
	if errors.Is(err, repository.ErrOAuthNotFound) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// TouchGrant is bookkeeping for the settings page; failures are not worth failing a request.
func (s *OAuthService) TouchGrant(ctx context.Context, grantID string) {
	_ = s.repo.TouchGrant(ctx, grantID)
}

// ---------- grants, for the settings page ----------

func (s *OAuthService) ListGrants(ctx context.Context, userID string) ([]model.OAuthGrant, error) {
	return s.repo.ListGrants(ctx, userID)
}

func (s *OAuthService) RevokeGrant(ctx context.Context, grantID, userID string) error {
	if err := s.repo.RevokeGrant(ctx, grantID, userID); err != nil {
		if errors.Is(err, repository.ErrOAuthNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// CleanupExpired drops rows that can no longer be redeemed.
func (s *OAuthService) CleanupExpired(ctx context.Context) { _ = s.repo.DeleteExpired(ctx) }
