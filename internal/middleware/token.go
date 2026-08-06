package middleware

import (
	"context"
	"errors"
	"net/http"
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

// RequireToken authenticates with a personal access token from the Authorization header and
// nothing else.
//
// It deliberately does NOT fall back to the `jwt` cookie the way RequireAuth does. This
// middleware guards /mcp, which lives on api.cram.club — the same host the browser sends that
// cookie to — so a cookie fallback would let any open browser tab drive the MCP tools.
func RequireToken(authn tokenAuthenticator) func(http.Handler) http.Handler {
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

			owner, err := authn.Authenticate(r.Context(), presented)
			if err != nil {
				if errors.Is(err, service.ErrInvalidToken) {
					unauthorized(w)
					return
				}
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}

			authn.TouchLastUsed(r.Context(), owner.Token.ID)

			// Same claims shape the JWT path produces, so service-layer ownership checks and
			// handlers cannot tell the two apart. Picture is empty: it exists for the web UI.
			claims := &auth.Claims{
				UserID: owner.Token.UserID,
				Email:  owner.Email,
				Role:   owner.Role,
			}
			ctx := context.WithValue(r.Context(), ClaimsKey, claims)
			ctx = context.WithValue(ctx, ScopeKey, owner.Token.Scope)
			ctx = context.WithValue(ctx, TokenIDKey, owner.Token.ID)
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

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="cram"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}
