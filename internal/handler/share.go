package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/service"
)

type ShareHandler struct {
	svc *service.CollectionService
}

func NewShareHandler(svc *service.CollectionService) *ShareHandler {
	return &ShareHandler{svc: svc}
}

// POST /collections/{collectionID}/share — generates a share token for a private collection.
func (h *ShareHandler) Generate(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	collectionID := chi.URLParam(r, "collectionID")
	token, err := h.svc.GenerateShareToken(r.Context(), collectionID, claims.UserID)
	if err != nil {
		http.Error(w, "failed to generate share link", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

// DELETE /collections/{collectionID}/share — revokes the share token.
func (h *ShareHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	collectionID := chi.URLParam(r, "collectionID")
	if err := h.svc.RevokeShareToken(r.Context(), collectionID, claims.UserID); err != nil {
		http.Error(w, "failed to revoke share link", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /shared/{token} — public endpoint; returns collection for anyone with the token.
func (h *ShareHandler) View(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	col, err := h.svc.GetByShareToken(r.Context(), token)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(col)
}
