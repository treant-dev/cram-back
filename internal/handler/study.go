package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/repository"
	"github.com/treant-dev/cram-go/internal/service"
)

type StudyHandler struct {
	svc *service.CollectionService
}

func NewStudyHandler(svc *service.CollectionService) *StudyHandler {
	return &StudyHandler{svc: svc}
}

func (h *StudyHandler) claims(r *http.Request) *auth.Claims {
	return r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
}

// Submit godoc
// @Summary      Submit study session results
// @Tags         study
// @Accept       json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        body body object{session_id=string,answers=array} true "Session results"
// @Success      204
// @Failure      400 {string} string
// @Router       /collections/{collectionID}/study [post]
func (h *StudyHandler) Submit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
		Answers   []struct {
			CardID  string `json:"card_id"`
			TQID    string `json:"tq_id"`
			Correct bool   `json:"correct"`
		} `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SessionID == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	answers := make([]repository.StudyAnswer, 0, len(body.Answers))
	for _, a := range body.Answers {
		if a.CardID == "" && a.TQID == "" {
			continue
		}
		answers = append(answers, repository.StudyAnswer{CardID: a.CardID, TQID: a.TQID, Correct: a.Correct})
	}
	if err := h.svc.SubmitStudySession(r.Context(), h.claims(r).UserID, body.SessionID, chi.URLParam(r, "collectionID"), answers); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetHistory godoc
// @Summary      Get daily answer history for a collection
// @Tags         study
// @Produce      json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        days         query int    false "Number of days to look back (default 30, max 365)"
// @Success      200 {object} repository.StudyHistoryData
// @Router       /collections/{collectionID}/history [get]
func (h *StudyHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	days := 30
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	history, err := h.svc.GetStudyHistory(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, days)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, history)
}
