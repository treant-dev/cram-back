package handler

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/treant-dev/cram-go/internal/auth"
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

// GET /auth/google
func (h *AuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := auth.GenerateState()
	if err != nil {
		http.Error(w, "could not generate state", http.StatusInternalServerError)
		return
	}
	url := h.oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOnline)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
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

	token, err := auth.IssueToken(user.ID, user.Email)
	if err != nil {
		http.Error(w, "could not issue token", http.StatusInternalServerError)
		return
	}

	frontend := os.Getenv("FRONTEND_URL")
	if frontend == "" {
		frontend = "http://localhost:3000"
	}
	http.Redirect(w, r, frontend+"/auth/callback?token="+token, http.StatusTemporaryRedirect)
}
