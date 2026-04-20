package handler

import (
	"net/http"

	"github.com/treant-dev/cram-go/internal/service"
)

type UsersHandler struct {
	svc *service.CollectionService
}

func NewUsersHandler(svc *service.CollectionService) *UsersHandler {
	return &UsersHandler{svc: svc}
}

// ListUsers godoc
// @Summary      List all users with their public collections
// @Tags         users
// @Produce      json
// @Success      200 {array} service.UserWithCollections
// @Failure      500 {string} string
// @Router       /users [get]
func (h *UsersHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.svc.ListUsers(r.Context())
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}
