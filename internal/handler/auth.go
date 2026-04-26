package handler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/model"
	"golang.org/x/oauth2"
)

type userUpseter interface {
	Upsert(ctx context.Context, googleID, email, name, picture string) (*model.User, error)
}

type AuthHandler struct {
	oauthCfg *oauth2.Config
	users    userUpseter
}

func NewAuthHandler(oauthCfg *oauth2.Config, users userUpseter) *AuthHandler {
	return &AuthHandler{oauthCfg: oauthCfg, users: users}
}

func frontendURL() string {
	if u := os.Getenv("FRONTEND_URL"); u != "" {
		return u
	}
	return "http://localhost:3000"
}

func isSecure() bool {
	return os.Getenv("ENV") == "production"
}

// GET /auth/google
func (h *AuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := auth.GenerateState()
	if err != nil {
		http.Error(w, "could not generate state", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, h.oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOnline), http.StatusTemporaryRedirect)
}

// GET /auth/google/callback
func (h *AuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	if !auth.ValidateState(r.URL.Query().Get("state")) {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	userInfo, err := auth.GetGoogleUserInfo(r.Context(), h.oauthCfg, code)
	if err != nil {
		http.Error(w, "failed to get user info", http.StatusInternalServerError)
		return
	}

	if !userInfo.VerifiedEmail {
		http.Error(w, "email not verified", http.StatusForbidden)
		return
	}

	user, err := h.users.Upsert(r.Context(), userInfo.ID, userInfo.Email, userInfo.Name, userInfo.Picture)
	if err != nil {
		log.Printf("upsert user: %v", err)
		http.Error(w, "could not save user", http.StatusInternalServerError)
		return
	}

	token, err := auth.IssueToken(user.ID, user.Email, user.Role, user.Picture)
	if err != nil {
		http.Error(w, "could not issue token", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "jwt",
		Value:    token,
		HttpOnly: true,
		Secure:   isSecure(),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   7 * 24 * 60 * 60,
	})
	http.Redirect(w, r, frontendURL()+"/auth/callback", http.StatusTemporaryRedirect)
}

// GET /auth/me — returns the current user's ID, email, and role from the JWT.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": claims.UserID, "email": claims.Email, "role": claims.Role, "picture": claims.Picture})
}

// GET /auth/logout — clears the JWT cookie and redirects to the frontend home page.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "jwt",
		Value:    "",
		HttpOnly: true,
		Secure:   isSecure(),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   -1,
	})
	http.Redirect(w, r, frontendURL(), http.StatusTemporaryRedirect)
}
