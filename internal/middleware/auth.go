package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/treant-dev/cram-go/internal/auth"
)

type contextKey string

const ClaimsKey contextKey = "claims"

// OptionalUserID extracts the user ID from the request JWT if present; returns empty string otherwise.
func OptionalUserID(r *http.Request) string {
	var raw string
	if cookie, err := r.Cookie("jwt"); err == nil {
		raw = cookie.Value
	} else if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		raw = strings.TrimPrefix(h, "Bearer ")
	}
	if raw == "" {
		return ""
	}
	claims, err := auth.ParseToken(raw)
	if err != nil {
		return ""
	}
	return claims.UserID
}

// RequireRole returns middleware that allows only requests whose JWT role matches one of the given roles.
// Must be used after RequireAuth (assumes claims are already in context).
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := r.Context().Value(ClaimsKey).(*auth.Claims)
			for _, role := range roles {
				if claims.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}

// RequireAuth validates the JWT from the HttpOnly cookie.
// Falls back to the Authorization: Bearer header for API clients (Swagger, curl).
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw string
		if cookie, err := r.Cookie("jwt"); err == nil {
			raw = cookie.Value
		} else if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			raw = strings.TrimPrefix(h, "Bearer ")
		}

		if raw == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		claims, err := auth.ParseToken(raw)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), ClaimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
