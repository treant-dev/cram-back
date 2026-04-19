package handler

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/service"
)

type CardsHandler struct {
	svc *service.CardService
}

func NewCardsHandler(svc *service.CardService) *CardsHandler {
	return &CardsHandler{svc: svc}
}

func (h *CardsHandler) claims(r *http.Request) *auth.Claims {
	return r.Context().Value(middleware.ClaimsKey).(*auth.Claims)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func handleErr(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// POST /sets
func (h *CardsHandler) CreateSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	set, err := h.svc.CreateSet(r.Context(), h.claims(r).UserID, body.Title, body.Description)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, set)
}

// GET /sets
func (h *CardsHandler) ListSets(w http.ResponseWriter, r *http.Request) {
	sets, err := h.svc.ListSets(r.Context(), h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sets)
}

// GET /sets/{setID}
func (h *CardsHandler) GetSet(w http.ResponseWriter, r *http.Request) {
	set, err := h.svc.GetSet(r.Context(), chi.URLParam(r, "setID"), h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// PUT /sets/{setID}
func (h *CardsHandler) UpdateSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	set, err := h.svc.UpdateSet(r.Context(), chi.URLParam(r, "setID"), h.claims(r).UserID, body.Title, body.Description)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// DELETE /sets/{setID}
func (h *CardsHandler) DeleteSet(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteSet(r.Context(), chi.URLParam(r, "setID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /sets/{setID}/cards
func (h *CardsHandler) AddCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
		Position int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Question == "" || body.Answer == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	card, err := h.svc.AddCard(r.Context(), chi.URLParam(r, "setID"), h.claims(r).UserID, body.Question, body.Answer, body.Position)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, card)
}

// PUT /sets/{setID}/cards/{cardID}
func (h *CardsHandler) UpdateCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
		Position int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Question == "" || body.Answer == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	card, err := h.svc.UpdateCard(r.Context(), chi.URLParam(r, "cardID"), chi.URLParam(r, "setID"), h.claims(r).UserID, body.Question, body.Answer, body.Position)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

// DELETE /sets/{setID}/cards/{cardID}
func (h *CardsHandler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteCard(r.Context(), chi.URLParam(r, "cardID"), chi.URLParam(r, "setID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /sets/{setID}/cards/import
// CSV format: question,answer
func (h *CardsHandler) ImportCSV(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		http.Error(w, "file too large", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		http.Error(w, "invalid csv", http.StatusBadRequest)
		return
	}

	var cards []model.Card
	for _, row := range records {
		if len(row) < 2 {
			continue
		}
		q := strings.TrimSpace(row[0])
		a := strings.TrimSpace(row[1])
		if q == "" || a == "" {
			continue
		}
		cards = append(cards, model.Card{Question: q, Answer: a})
	}

	if len(cards) == 0 {
		http.Error(w, "no valid rows in csv", http.StatusBadRequest)
		return
	}

	if err := h.svc.ImportCards(r.Context(), chi.URLParam(r, "setID"), h.claims(r).UserID, cards); err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int{"imported": len(cards)})
}

// POST /sets/{setID}/tests
func (h *CardsHandler) AddTestQuestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string            `json:"question"`
		Options  []model.TestOption `json:"options"`
		Position int               `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Question == "" || len(body.Options) < 2 {
		http.Error(w, "invalid body: need question and at least 2 options", http.StatusBadRequest)
		return
	}
	tq, err := h.svc.AddTestQuestion(r.Context(), chi.URLParam(r, "setID"), h.claims(r).UserID, body.Question, body.Options, body.Position)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tq)
}

// PUT /sets/{setID}/tests/{tqID}
func (h *CardsHandler) UpdateTestQuestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string            `json:"question"`
		Options  []model.TestOption `json:"options"`
		Position int               `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Question == "" || len(body.Options) < 2 {
		http.Error(w, "invalid body: need question and at least 2 options", http.StatusBadRequest)
		return
	}
	tq, err := h.svc.UpdateTestQuestion(r.Context(), chi.URLParam(r, "tqID"), chi.URLParam(r, "setID"), h.claims(r).UserID, body.Question, body.Options, body.Position)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tq)
}

// DELETE /sets/{setID}/tests/{tqID}
func (h *CardsHandler) DeleteTestQuestion(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteTestQuestion(r.Context(), chi.URLParam(r, "tqID"), chi.URLParam(r, "setID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
