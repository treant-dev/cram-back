package handler

import (
	"net/http"

	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/service"
)

type AccountHandler struct {
	svc *service.CollectionService
}

func NewAccountHandler(svc *service.CollectionService) *AccountHandler {
	return &AccountHandler{svc: svc}
}

// DELETE /account — permanently deletes the authenticated user and all their data.
func (h *AccountHandler) Delete(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
	if err := h.svc.DeleteAccount(r.Context(), claims.UserID); err != nil {
		http.Error(w, "failed to delete account", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "jwt",
		Value:    "",
		HttpOnly: true,
		Secure:   isSecure(),
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}
