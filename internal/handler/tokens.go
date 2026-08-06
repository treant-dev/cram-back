package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/service"
)

type TokensHandler struct {
	svc *service.TokenService
}

func NewTokensHandler(svc *service.TokenService) *TokensHandler {
	return &TokensHandler{svc: svc}
}

func (h *TokensHandler) claims(r *http.Request) *auth.Claims {
	return r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
}

// tokenView is the safe representation — never carries the token value or its hash.
type tokenView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Scope      string     `json:"scope"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

func toTokenView(t model.PersonalAccessToken) tokenView {
	return tokenView{
		ID:         t.ID,
		Name:       t.Name,
		Scope:      t.Scope,
		CreatedAt:  t.CreatedAt,
		LastUsedAt: t.LastUsedAt,
		ExpiresAt:  t.ExpiresAt,
		RevokedAt:  t.RevokedAt,
	}
}

// Create godoc
// @Summary      Create a personal access token
// @Description  Mints a long-lived bearer token for non-browser clients (the MCP server). The
// @Description  plaintext value is returned by this call only and cannot be retrieved afterwards.
// @Tags         tokens
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body body object{name=string,scope=string,expires_at=string} true "name, scope ('read' or 'read_write'), optional expires_at"
// @Success      201 {object} object{id=string,name=string,scope=string,created_at=string,token=string}
// @Failure      400 {string} string
// @Failure      401 {string} string
// @Failure      500 {string} string
// @Router       /account/tokens [post]
func (h *TokensHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := h.claims(r)

	var req struct {
		Name      string     `json:"name"`
		Scope     string     `json:"scope"`
		ExpiresAt *time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Scope == "" {
		req.Scope = model.ScopeReadWrite
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		http.Error(w, "expires_at must be in the future", http.StatusBadRequest)
		return
	}

	token, plaintext, err := h.svc.Create(r.Context(), claims.UserID, req.Name, req.Scope, req.ExpiresAt)
	if err != nil {
		if errors.Is(err, service.ErrTokenName) || errors.Is(err, service.ErrInvalidScope) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if errors.Is(err, service.ErrTokenRateLimited) {
			http.Error(w, err.Error(), http.StatusTooManyRequests)
			return
		}
		if errors.Is(err, service.ErrTooManyTokens) {
			// 409: the request is well-formed, the account state is what refuses it.
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		handleErr(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, struct {
		tokenView
		Token string `json:"token"`
	}{toTokenView(*token), plaintext})
}

// List godoc
// @Summary      List personal access tokens
// @Description  Metadata only — token values are never returned. Revoked tokens are included so
// @Description  the UI can show history.
// @Tags         tokens
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array} object{id=string,name=string,scope=string,created_at=string,last_used_at=string,expires_at=string,revoked_at=string}
// @Failure      401 {string} string
// @Failure      500 {string} string
// @Router       /account/tokens [get]
func (h *TokensHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := h.claims(r)
	tokens, err := h.svc.List(r.Context(), claims.UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	views := make([]tokenView, 0, len(tokens))
	for _, t := range tokens {
		views = append(views, toTokenView(t))
	}
	writeJSON(w, http.StatusOK, views)
}

// Revoke godoc
// @Summary      Revoke a personal access token
// @Description  Takes effect on the very next request; no restart, no waiting for expiry.
// @Tags         tokens
// @Security     BearerAuth
// @Param        tokenID path string true "Token ID"
// @Success      204 "revoked"
// @Failure      401 {string} string
// @Failure      404 {string} string
// @Router       /account/tokens/{tokenID} [delete]
func (h *TokensHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	claims := h.claims(r)
	if err := h.svc.Revoke(r.Context(), chi.URLParam(r, "tokenID"), claims.UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
