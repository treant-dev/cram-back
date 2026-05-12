package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/service"
)

type BlitzHandler struct {
	svc *service.CollectionService
}

func NewBlitzHandler(svc *service.CollectionService) *BlitzHandler {
	return &BlitzHandler{svc: svc}
}

func (h *BlitzHandler) claims(r *http.Request) *auth.Claims {
	return r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
}

func (h *BlitzHandler) Get(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.GetBlitz(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
