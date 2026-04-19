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

// CreateSet godoc
// @Summary      Create a study set
// @Tags         sets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body body object{title=string,description=string} true "Set data"
// @Success      201 {object} model.StudySet
// @Failure      400 {string} string
// @Failure      500 {string} string
// @Router       /sets [post]
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

// ListSets godoc
// @Summary      List study sets
// @Tags         sets
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array}  model.StudySet
// @Failure      500 {string} string
// @Router       /sets [get]
func (h *CardsHandler) ListSets(w http.ResponseWriter, r *http.Request) {
	sets, err := h.svc.ListSets(r.Context(), h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sets)
}

// GetSet godoc
// @Summary      Get a study set with cards and test questions
// @Tags         sets
// @Produce      json
// @Security     BearerAuth
// @Param        setID path string true "Set ID"
// @Success      200 {object} model.StudySet
// @Failure      404 {string} string
// @Router       /sets/{setID} [get]
func (h *CardsHandler) GetSet(w http.ResponseWriter, r *http.Request) {
	set, err := h.svc.GetSet(r.Context(), chi.URLParam(r, "setID"), h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// UpdateSet godoc
// @Summary      Update a study set
// @Tags         sets
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        setID path string true "Set ID"
// @Param        body  body object{title=string,description=string} true "Set data"
// @Success      200 {object} model.StudySet
// @Failure      404 {string} string
// @Router       /sets/{setID} [put]
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

// DeleteSet godoc
// @Summary      Delete a study set
// @Tags         sets
// @Security     BearerAuth
// @Param        setID path string true "Set ID"
// @Success      204
// @Failure      404 {string} string
// @Router       /sets/{setID} [delete]
func (h *CardsHandler) DeleteSet(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteSet(r.Context(), chi.URLParam(r, "setID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddCard godoc
// @Summary      Add a flashcard to a set
// @Tags         cards
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        setID path string true "Set ID"
// @Param        body  body object{question=string,answer=string,position=int} true "Card data"
// @Success      201 {object} model.Card
// @Failure      404 {string} string
// @Router       /sets/{setID}/cards [post]
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

// UpdateCard godoc
// @Summary      Update a flashcard
// @Tags         cards
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        setID  path string true "Set ID"
// @Param        cardID path string true "Card ID"
// @Param        body   body object{question=string,answer=string,position=int} true "Card data"
// @Success      200 {object} model.Card
// @Failure      404 {string} string
// @Router       /sets/{setID}/cards/{cardID} [put]
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

// DeleteCard godoc
// @Summary      Delete a flashcard
// @Tags         cards
// @Security     BearerAuth
// @Param        setID  path string true "Set ID"
// @Param        cardID path string true "Card ID"
// @Success      204
// @Failure      404 {string} string
// @Router       /sets/{setID}/cards/{cardID} [delete]
func (h *CardsHandler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteCard(r.Context(), chi.URLParam(r, "cardID"), chi.URLParam(r, "setID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ImportCSV godoc
// @Summary      Bulk import cards from CSV (question,answer)
// @Tags         cards
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        setID path string true "Set ID"
// @Param        file  formData file true "CSV file"
// @Success      201 {object} object{imported=int}
// @Failure      400 {string} string
// @Router       /sets/{setID}/cards/import [post]
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

// AddTestQuestion godoc
// @Summary      Add a test question to a set
// @Tags         tests
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        setID path string true "Set ID"
// @Param        body  body object{question=string,options=[]model.TestOption,position=int} true "Question data"
// @Success      201 {object} model.TestQuestion
// @Failure      404 {string} string
// @Router       /sets/{setID}/tests [post]
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

// UpdateTestQuestion godoc
// @Summary      Update a test question
// @Tags         tests
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        setID path string true "Set ID"
// @Param        tqID  path string true "Test question ID"
// @Param        body  body object{question=string,options=[]model.TestOption,position=int} true "Question data"
// @Success      200 {object} model.TestQuestion
// @Failure      404 {string} string
// @Router       /sets/{setID}/tests/{tqID} [put]
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

// DeleteTestQuestion godoc
// @Summary      Delete a test question
// @Tags         tests
// @Security     BearerAuth
// @Param        setID path string true "Set ID"
// @Param        tqID  path string true "Test question ID"
// @Success      204
// @Failure      404 {string} string
// @Router       /sets/{setID}/tests/{tqID} [delete]
func (h *CardsHandler) DeleteTestQuestion(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteTestQuestion(r.Context(), chi.URLParam(r, "tqID"), chi.URLParam(r, "setID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
