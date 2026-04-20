package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/treant-dev/cram-go/internal/service"
)

type AdminHandler struct {
	svc *service.CollectionService
}

func NewAdminHandler(svc *service.CollectionService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// GET /admin/users
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.ListUsers(r.Context())
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

// PUT /admin/users/{userID}/role
func (h *AdminHandler) SetRole(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Role == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := h.svc.SetUserRole(r.Context(), chi.URLParam(r, "userID"), body.Role); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /admin/collections/{collectionID}
func (h *AdminHandler) DeleteCollection(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.AdminDeleteCollection(r.Context(), chi.URLParam(r, "collectionID")); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
