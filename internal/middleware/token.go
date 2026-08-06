package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/service"
)

// ScopeKey holds the authenticated token's scope. Present only on requests authenticated by a
// personal access token.
const ScopeKey contextKey = "token_scope"

// TokenIDKey holds the authenticated token's id, for audit logging. Never the token value.
const TokenIDKey contextKey = "token_id"

type tokenAuthenticator interface {
	Authenticate(ctx context.Context, plaintext string) (*model.TokenOwner, error)
	TouchLastUsed(ctx context.Context, tokenID string)
}

// oauthAuthenticator resolves an OAuth access token. Optional: pass nil before the
// authorization server exists.
type oauthAuthenticator interface {
	Authenticate(ctx context.Context, accessToken string) (*model.OAuthToken, error)
	TouchGrant(ctx context.Context, grantID string)
}

// RequireToken authenticates with a personal access token from the Authorization header and
// nothing else.
//
// It deliberately does NOT fall back to the `jwt` cookie the way RequireAuth does. This
// middleware guards /mcp, which lives on api.cram.club — the same host the browser sends that
// cookie to — so a cookie fallback would let any open browser tab drive the MCP tools.
func RequireToken(authn tokenAuthenticator, oauthn oauthAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				unauthorized(w)
				return
			}
			presented := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			if presented == "" {
				unauthorized(w)
				return
			}

			// Two kinds of bearer reach this endpoint: a personal access token pasted into a
			// client's config, and an OAuth access token obtained through the consent flow.
			// Both end up as the same claims, so nothing downstream has to care which it was.
			var (
				claims  *auth.Claims
				scope   string
				tokenID string
			)
			owner, err := authn.Authenticate(r.Context(), presented)
			switch {
			case err == nil:
				authn.TouchLastUsed(r.Context(), owner.Token.ID)
				// Picture is empty: it exists for the web UI.
				claims = &auth.Claims{UserID: owner.Token.UserID, Email: owner.Email, Role: owner.Role}
				scope, tokenID = owner.Token.Scope, owner.Token.ID
			case errors.Is(err, service.ErrInvalidToken) && oauthn != nil:
				tok, oerr := oauthn.Authenticate(r.Context(), presented)
				if oerr != nil {
					if errors.Is(oerr, service.ErrInvalidToken) {
						unauthorized(w)
						return
					}
					http.Error(w, "internal error", http.StatusInternalServerError)
					return
				}
				oauthn.TouchGrant(r.Context(), tok.GrantID)
				claims = &auth.Claims{UserID: tok.UserID}
				scope, tokenID = tok.Scope, tok.GrantID
			case errors.Is(err, service.ErrInvalidToken):
				unauthorized(w)
				return
			default:
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}

			ctx := context.WithValue(r.Context(), ClaimsKey, claims)
			ctx = context.WithValue(ctx, ScopeKey, scope)
			ctx = context.WithValue(ctx, TokenIDKey, tokenID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Scope returns the authenticated token's scope, or "" when the request was not authenticated
// by a token.
func Scope(r *http.Request) string {
	scope, _ := r.Context().Value(ScopeKey).(string)
	return scope
}

// TokenID returns the authenticated token's id, or "" when not token-authenticated.
func TokenID(r *http.Request) string {
	id, _ := r.Context().Value(TokenIDKey).(string)
	return id
}

// unauthorized points the client at the protected-resource metadata, which is how an MCP client
// discovers where to run the OAuth flow (RFC 9728 §5.1). Without this header a client that has
// no token has no way to find the authorization server.
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate",
		`Bearer realm="cram", resource_metadata="`+publicOrigin()+`/.well-known/oauth-protected-resource"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// publicOrigin is what clients call us, which is not what the process binds to.
func publicOrigin() string {
	if u := os.Getenv("PUBLIC_API_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	if u := os.Getenv("GOOGLE_REDIRECT_URL"); u != "" {
		if parsed, err := url.Parse(u); err == nil && parsed.Scheme != "" {
			return parsed.Scheme + "://" + parsed.Host
		}
	}
	return "http://localhost:8080"
}
