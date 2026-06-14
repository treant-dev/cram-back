package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/service"
)

type ProgressHandler struct {
	svc *service.CollectionService
}

func NewProgressHandler(svc *service.CollectionService) *ProgressHandler {
	return &ProgressHandler{svc: svc}
}

func (h *ProgressHandler) claims(r *http.Request) *auth.Claims {
	return r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
}

func (h *ProgressHandler) Get(w http.ResponseWriter, r *http.Request) {
	collectionID := chi.URLParam(r, "collectionID")
	data, err := h.svc.GetProgress(r.Context(), collectionID, h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// ResetCollection clears all of the caller's progress for a collection.
func (h *ProgressHandler) ResetCollection(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ResetCollectionProgress(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ResetCard clears the caller's progress for a single card.
func (h *ProgressHandler) ResetCard(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ResetItemProgress(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, "card", chi.URLParam(r, "cardID")); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ResetTQ clears the caller's progress for a single test question.
func (h *ProgressHandler) ResetTQ(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ResetItemProgress(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, "tq", chi.URLParam(r, "tqID")); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ResetExercise clears the caller's progress for all sentences of one exercise.
func (h *ProgressHandler) ResetExercise(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ResetExerciseProgress(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, chi.URLParam(r, "exID")); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProgressHandler) Update(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ItemType        string `json:"item_type"`
		ItemID          string `json:"item_id"`
		Correct         bool   `json:"correct"`
		ConfidenceDelta int    `json:"confidence_delta"`
		Retry           bool   `json:"retry"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.ItemType != "card" && body.ItemType != "tq" {
		http.Error(w, "item_type must be card or tq", http.StatusBadRequest)
		return
	}
	if body.ItemID == "" {
		http.Error(w, "item_id required", http.StatusBadRequest)
		return
	}
	if body.ConfidenceDelta < -1 || body.ConfidenceDelta > 1 {
		http.Error(w, "confidence_delta must be -1, 0, or 1", http.StatusBadRequest)
		return
	}

	level, nextReview, err := h.svc.UpdateProgress(
		r.Context(),
		h.claims(r).UserID,
		chi.URLParam(r, "collectionID"),
		body.ItemType,
		body.ItemID,
		body.Correct,
		body.ConfidenceDelta,
		body.Retry,
	)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"level":          level,
		"next_review_at": nextReview,
	})
}
