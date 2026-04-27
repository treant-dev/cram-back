package handler

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/service"
)

const (
	maxTitleLen   = 200
	maxDescLen    = 1000
	maxFieldLen   = 2000
	maxOptionText = 500
	maxCSVRows    = 5000
)

type CardsHandler struct {
	svc *service.CollectionService
}

func NewCardsHandler(svc *service.CollectionService) *CardsHandler {
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
	log.Printf("internal error: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// CreateCollection godoc
// @Summary      Create a collection
// @Tags         collections
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body body object{title=string,description=string,is_public=bool} true "Collection data"
// @Success      201 {object} model.Collection
// @Failure      400 {string} string
// @Failure      500 {string} string
// @Router       /collections [post]
func (h *CardsHandler) CreateCollection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		IsPublic    bool   `json:"is_public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(body.Title) > maxTitleLen || len(body.Description) > maxDescLen {
		http.Error(w, "title or description too long", http.StatusBadRequest)
		return
	}
	col, err := h.svc.CreateCollection(r.Context(), h.claims(r).UserID, body.Title, body.Description, body.IsPublic)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, col)
}

// ListCollections godoc
// @Summary      List collections
// @Tags         collections
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array} model.Collection
// @Failure      500 {string} string
// @Router       /collections [get]
func (h *CardsHandler) ListCollections(w http.ResponseWriter, r *http.Request) {
	cols, err := h.svc.ListCollections(r.Context(), h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cols)
}

// Home godoc
// @Summary      Get home page data (own + followed collections)
// @Tags         collections
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} service.HomeData
// @Failure      500 {string} string
// @Router       /home [get]
func (h *CardsHandler) Home(w http.ResponseWriter, r *http.Request) {
	data, err := h.svc.GetHome(r.Context(), h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

// ListPublicCollections godoc
// @Summary      List all public collections with follow metadata
// @Tags         collections
// @Produce      json
// @Success      200 {array} service.PublicCollectionMeta
// @Router       /public/collections [get]
func (h *CardsHandler) ListPublicCollections(w http.ResponseWriter, r *http.Request) {
	userID := middleware.OptionalUserID(r)
	cols, err := h.svc.ListPublicWithMeta(r.Context(), userID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cols)
}

// GetOrCreateDraft godoc
// @Summary      Get or create a draft for a collection
// @Tags         drafts
// @Produce      json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Success      200 {object} model.Collection
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/draft [post]
func (h *CardsHandler) GetOrCreateDraft(w http.ResponseWriter, r *http.Request) {
	draft, err := h.svc.GetOrCreateDraft(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

// UpdateDraft godoc
// @Summary      Save draft content without publishing
// @Tags         drafts
// @Accept       json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        body body service.UpdateDraftReq true "Draft content"
// @Success      204
// @Failure      400 {string} string
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/draft [put]
func (h *CardsHandler) UpdateDraft(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title         string `json:"title"`
		Description   string `json:"description"`
		IsPublic      bool   `json:"is_public"`
		Cards         []struct {
			ID       string `json:"id"`
			Question string `json:"question"`
			Answer   string `json:"answer"`
			Image    string `json:"image"`
		} `json:"cards"`
		TestQuestions []struct {
			ID       string             `json:"id"`
			Question string             `json:"question"`
			Options  []model.TestOption `json:"options"`
			Image    string             `json:"image"`
		} `json:"test_questions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.Title == "" {
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}
	if len(body.Title) > maxTitleLen || len(body.Description) > maxDescLen {
		http.Error(w, "title or description too long", http.StatusBadRequest)
		return
	}
	req := service.UpdateDraftReq{
		Title:       body.Title,
		Description: body.Description,
		IsPublic:    body.IsPublic,
	}
	for _, c := range body.Cards {
		req.Cards = append(req.Cards, service.DraftCard{ID: c.ID, Question: c.Question, Answer: c.Answer, Image: c.Image})
	}
	for _, t := range body.TestQuestions {
		req.TestQuestions = append(req.TestQuestions, service.DraftQuestion{ID: t.ID, Question: t.Question, Options: t.Options, Image: t.Image})
	}
	if err := h.svc.UpdateDraft(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, req); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DiscardDraft godoc
// @Summary      Discard draft and restore the active version
// @Tags         drafts
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Success      204
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/draft [delete]
func (h *CardsHandler) DiscardDraft(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DiscardDraft(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PublishDraft godoc
// @Summary      Publish the draft as the active version
// @Tags         drafts
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Success      204
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/draft/publish [post]
func (h *CardsHandler) PublishDraft(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.PublishDraft(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Follow godoc
// @Summary      Follow a collection
// @Tags         follows
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Success      204
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/follow [post]
func (h *CardsHandler) Follow(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Follow(r.Context(), h.claims(r).UserID, chi.URLParam(r, "collectionID")); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Unfollow godoc
// @Summary      Unfollow a collection
// @Tags         follows
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Success      204
// @Router       /collections/{collectionID}/follow [delete]
func (h *CardsHandler) Unfollow(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Unfollow(r.Context(), h.claims(r).UserID, chi.URLParam(r, "collectionID")); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetCollection godoc
// @Summary      Get a collection with cards and test questions
// @Tags         collections
// @Produce      json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Success      200 {object} model.Collection
// @Failure      404 {string} string
// @Router       /collections/{collectionID} [get]
func (h *CardsHandler) GetCollection(w http.ResponseWriter, r *http.Request) {
	claims := h.claims(r)
	col, err := h.svc.GetCollection(r.Context(), chi.URLParam(r, "collectionID"), claims.UserID, claims.Role == "admin")
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, col)
}

// UpdateCollection godoc
// @Summary      Update a collection
// @Tags         collections
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        body body object{title=string,description=string,is_public=bool} true "Collection data"
// @Success      200 {object} model.Collection
// @Failure      404 {string} string
// @Router       /collections/{collectionID} [put]
func (h *CardsHandler) UpdateCollection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		IsPublic    bool   `json:"is_public"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Title == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(body.Title) > maxTitleLen || len(body.Description) > maxDescLen {
		http.Error(w, "title or description too long", http.StatusBadRequest)
		return
	}
	col, err := h.svc.UpdateCollection(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, body.Title, body.Description, body.IsPublic)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, col)
}

// DeleteCollection godoc
// @Summary      Delete a collection
// @Tags         collections
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Success      204
// @Failure      404 {string} string
// @Router       /collections/{collectionID} [delete]
func (h *CardsHandler) DeleteCollection(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteCollection(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddCard godoc
// @Summary      Add a card to a collection
// @Tags         cards
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        body body object{question=string,answer=string,position=int} true "Card data"
// @Success      201 {object} model.Card
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/cards [post]
func (h *CardsHandler) AddCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
		Image    string `json:"image"`
		Position int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Question == "" || body.Answer == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(body.Question) > maxFieldLen || len(body.Answer) > maxFieldLen {
		http.Error(w, "question or answer too long", http.StatusBadRequest)
		return
	}
	card, err := h.svc.AddCard(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, body.Question, body.Answer, body.Image, body.Position)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, card)
}

// UpdateCard godoc
// @Summary      Update a card
// @Tags         cards
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        cardID path string true "Card ID"
// @Param        body body object{question=string,answer=string,position=int} true "Card data"
// @Success      200 {object} model.Card
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/cards/{cardID} [put]
func (h *CardsHandler) UpdateCard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
		Image    string `json:"image"`
		Position int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Question == "" || body.Answer == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(body.Question) > maxFieldLen || len(body.Answer) > maxFieldLen {
		http.Error(w, "question or answer too long", http.StatusBadRequest)
		return
	}
	card, err := h.svc.UpdateCard(r.Context(), chi.URLParam(r, "cardID"), chi.URLParam(r, "collectionID"), h.claims(r).UserID, body.Question, body.Answer, body.Image, body.Position)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, card)
}

// DeleteCard godoc
// @Summary      Delete a card
// @Tags         cards
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        cardID path string true "Card ID"
// @Success      204
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/cards/{cardID} [delete]
func (h *CardsHandler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteCard(r.Context(), chi.URLParam(r, "cardID"), chi.URLParam(r, "collectionID"), h.claims(r).UserID); err != nil {
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
// @Param        collectionID path string true "Collection ID"
// @Param        file formData file true "CSV file"
// @Success      201 {object} object{imported=int}
// @Failure      400 {string} string
// @Router       /collections/{collectionID}/cards/import [post]
func (h *CardsHandler) ImportCSV(w http.ResponseWriter, r *http.Request) {
	const maxFileSize = 4 << 20 // 4 MB
	if err := r.ParseMultipartForm(maxFileSize); err != nil {
		http.Error(w, "file too large", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	r2 := csv.NewReader(file)
	r2.Comma = ';'
	records, err := r2.ReadAll()
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
	if len(cards) > maxCSVRows {
		http.Error(w, fmt.Sprintf("too many rows (max %d)", maxCSVRows), http.StatusBadRequest)
		return
	}

	if err := h.svc.ImportCards(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, cards); err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int{"imported": len(cards)})
}

// ImportTests godoc
// @Summary      Bulk import test questions from semicolon-separated text
// @Router       /collections/{collectionID}/tests/import [post]
func (h *CardsHandler) ImportTests(w http.ResponseWriter, r *http.Request) {
	const maxFileSize = 4 << 20
	if err := r.ParseMultipartForm(maxFileSize); err != nil {
		http.Error(w, "file too large", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	rd := csv.NewReader(file)
	rd.Comma = ';'
	rd.FieldsPerRecord = -1 // variable columns
	records, err := rd.ReadAll()
	if err != nil {
		http.Error(w, "invalid csv", http.StatusBadRequest)
		return
	}

	var tqs []model.TestQuestion
	for _, row := range records {
		if len(row) < 5 || (len(row)-1)%2 != 0 {
			continue
		}
		q := strings.TrimSpace(row[0])
		if q == "" {
			continue
		}
		var opts []model.TestOption
		for i := 1; i+1 < len(row); i += 2 {
			raw := strings.ToLower(strings.TrimSpace(row[i]))
			isCorrect := raw == "1" || raw == "t" || raw == "true"
			text := strings.TrimSpace(row[i+1])
			if text == "" {
				continue
			}
			opts = append(opts, model.TestOption{Text: text, IsCorrect: isCorrect})
		}
		if len(opts) < 2 {
			continue
		}
		hasCorrect := false
		for _, o := range opts {
			if o.IsCorrect {
				hasCorrect = true
				break
			}
		}
		if !hasCorrect {
			continue
		}
		tqs = append(tqs, model.TestQuestion{Question: q, Options: opts})
	}

	if len(tqs) == 0 {
		http.Error(w, "no valid rows", http.StatusBadRequest)
		return
	}
	if err := h.svc.ImportTests(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, tqs); err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int{"imported": len(tqs)})
}

// AddTestQuestion godoc
// @Summary      Add a test question to a collection
// @Tags         tests
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        body body object{question=string,options=[]model.TestOption,position=int} true "Question data"
// @Success      201 {object} model.TestQuestion
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/tests [post]
func (h *CardsHandler) AddTestQuestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string             `json:"question"`
		Options  []model.TestOption `json:"options"`
		Image    string             `json:"image"`
		Position int                `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Question == "" || len(body.Options) < 2 {
		http.Error(w, "invalid body: need question and at least 2 options", http.StatusBadRequest)
		return
	}
	if len(body.Question) > maxFieldLen {
		http.Error(w, "question too long", http.StatusBadRequest)
		return
	}
	for _, o := range body.Options {
		if o.Text == "" || len(o.Text) > maxOptionText {
			http.Error(w, "option text missing or too long", http.StatusBadRequest)
			return
		}
	}
	tq, err := h.svc.AddTestQuestion(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, body.Question, body.Options, body.Image, body.Position)
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
// @Param        collectionID path string true "Collection ID"
// @Param        tqID path string true "Test question ID"
// @Param        body body object{question=string,options=[]model.TestOption,position=int} true "Question data"
// @Success      200 {object} model.TestQuestion
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/tests/{tqID} [put]
func (h *CardsHandler) UpdateTestQuestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string             `json:"question"`
		Options  []model.TestOption `json:"options"`
		Image    string             `json:"image"`
		Position int                `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Question == "" || len(body.Options) < 2 {
		http.Error(w, "invalid body: need question and at least 2 options", http.StatusBadRequest)
		return
	}
	if len(body.Question) > maxFieldLen {
		http.Error(w, "question too long", http.StatusBadRequest)
		return
	}
	for _, o := range body.Options {
		if o.Text == "" || len(o.Text) > maxOptionText {
			http.Error(w, "option text missing or too long", http.StatusBadRequest)
			return
		}
	}
	tq, err := h.svc.UpdateTestQuestion(r.Context(), chi.URLParam(r, "tqID"), chi.URLParam(r, "collectionID"), h.claims(r).UserID, body.Question, body.Options, body.Image, body.Position)
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
// @Param        collectionID path string true "Collection ID"
// @Param        tqID path string true "Test question ID"
// @Success      204
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/tests/{tqID} [delete]
func (h *CardsHandler) DeleteTestQuestion(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.DeleteTestQuestion(r.Context(), chi.URLParam(r, "tqID"), chi.URLParam(r, "collectionID"), h.claims(r).UserID); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
