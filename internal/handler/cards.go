package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/treant-dev/cram-go/internal/auth"
	"github.com/treant-dev/cram-go/internal/middleware"
	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/service"
	"gopkg.in/yaml.v3"
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
	if errors.Is(err, service.ErrForbidden) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if errors.Is(err, service.ErrInvalidType) {
		http.Error(w, "invalid collection type for this operation", http.StatusBadRequest)
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
		Type        string `json:"type"`
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
	col, err := h.svc.CreateCollection(r.Context(), h.claims(r).UserID, body.Title, body.Description, body.Type, body.IsPublic)
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
			ID         string `json:"id"`
			Term       string `json:"term"`
			Definition string `json:"definition"`
			Image      string `json:"image"`
		} `json:"cards"`
		TestQuestions []struct {
			ID       string             `json:"id"`
			Question string             `json:"question"`
			Options  []model.TestAnswer `json:"options"`
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
		req.Cards = append(req.Cards, service.DraftCardInput{ID: c.ID, Term: c.Term, Definition: c.Definition, Image: c.Image})
	}
	for _, t := range body.TestQuestions {
		req.TestQuestions = append(req.TestQuestions, service.DraftTestInput{ID: t.ID, Question: t.Question, Options: t.Options, Image: t.Image})
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

// GetDraftDiff godoc
// @Summary      Diff the staged draft against the published collection
// @Tags         drafts
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Success      200 {object} service.DraftDiff
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/draft/diff [get]
func (h *CardsHandler) GetDraftDiff(w http.ResponseWriter, r *http.Request) {
	diff, err := h.svc.GetDraftDiff(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

type draftItemBody struct {
	Type     string         `json:"type"`
	ParentID *string        `json:"parent_id"`
	Content  map[string]any `json:"content"`
	Rank     string         `json:"rank"`
}

// AddDraftItem godoc
// @Summary      Stage a new item into the draft
// @Tags         drafts
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        body body draftItemBody true "Item to stage"
// @Success      200 {object} model.Item
// @Failure      400 {string} string
// @Router       /collections/{collectionID}/draft/items [post]
func (h *CardsHandler) AddDraftItem(w http.ResponseWriter, r *http.Request) {
	var body draftItemBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Type == "" {
		http.Error(w, "invalid item body", http.StatusBadRequest)
		return
	}
	it, err := h.svc.StageDraftItem(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, "",
		service.DraftItemInput{Type: body.Type, ParentID: body.ParentID, Content: body.Content, Rank: body.Rank})
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// UpdateDraftItem godoc
// @Summary      Stage an edit to a single item in the draft
// @Tags         drafts
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        itemID path string true "Item ID"
// @Param        body body draftItemBody true "New item state"
// @Success      200 {object} model.Item
// @Failure      400 {string} string
// @Router       /collections/{collectionID}/draft/items/{itemID} [put]
func (h *CardsHandler) UpdateDraftItem(w http.ResponseWriter, r *http.Request) {
	var body draftItemBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Type == "" {
		http.Error(w, "invalid item body", http.StatusBadRequest)
		return
	}
	it, err := h.svc.StageDraftItem(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, chi.URLParam(r, "itemID"),
		service.DraftItemInput{Type: body.Type, ParentID: body.ParentID, Content: body.Content, Rank: body.Rank})
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

// DeleteDraftItem godoc
// @Summary      Stage deletion of a single item in the draft
// @Tags         drafts
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        itemID path string true "Item ID"
// @Success      204
// @Router       /collections/{collectionID}/draft/items/{itemID} [delete]
func (h *CardsHandler) DeleteDraftItem(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.StageDraftDelete(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, chi.URLParam(r, "itemID")); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RevertDraftItem godoc
// @Summary      Revert one item's staged change back to the published state
// @Tags         drafts
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        itemID path string true "Item ID"
// @Success      204
// @Router       /collections/{collectionID}/draft/items/{itemID}/revert [post]
func (h *CardsHandler) RevertDraftItem(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RevertDraftItem(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, chi.URLParam(r, "itemID")); err != nil {
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

func (h *CardsHandler) GetPublicCollection(w http.ResponseWriter, r *http.Request) {
	col, err := h.svc.GetPublicCollection(r.Context(), chi.URLParam(r, "collectionID"))
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
		Term       string `json:"term"`
		Definition string `json:"definition"`
		Image      string `json:"image"`
		Position   int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Term == "" || body.Definition == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(body.Term) > maxFieldLen || len(body.Definition) > maxFieldLen {
		http.Error(w, "term or definition too long", http.StatusBadRequest)
		return
	}
	card, err := h.svc.AddCard(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, body.Term, body.Definition, body.Image, body.Position)
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
		Term       string `json:"term"`
		Definition string `json:"definition"`
		Image      string `json:"image"`
		Position   int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Term == "" || body.Definition == "" {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(body.Term) > maxFieldLen || len(body.Definition) > maxFieldLen {
		http.Error(w, "term or definition too long", http.StatusBadRequest)
		return
	}
	card, err := h.svc.UpdateCard(r.Context(), chi.URLParam(r, "cardID"), chi.URLParam(r, "collectionID"), h.claims(r).UserID, body.Term, body.Definition, body.Image, body.Position)
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

// readImportCSV parses a semicolon-separated import file. LazyQuotes tolerates
// bare " characters common in language content, and FieldsPerRecord = -1 lets
// rows have varying column counts so one ragged row can't fail the whole file.
// looksStructured reports whether an import body is JSON/YAML rather than CSV,
// based on the first non-space byte: '[' / '{' (JSON or flow YAML), or a YAML
// block-list item "- " / document marker "---". A CSV value that merely starts
// with '-' (e.g. "-5 degrees;cold") is not a list item, so it stays CSV. The two
// formats are unambiguous in practice.
func looksStructured(raw []byte) bool {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return false
	}
	switch t[0] {
	case '[', '{':
		return true
	case '-':
		return len(t) > 1 && (t[1] == ' ' || t[1] == '\t' || t[1] == '\n' || t[1] == '\r' || t[1] == '-')
	}
	return false
}

// cardImport / testImport mirror the JSON/YAML import shapes (yaml.v3 parses both,
// since JSON is a subset of YAML). They use the user-facing question/answer
// vocabulary rather than the model's term/definition.
type cardImport struct {
	Question string `yaml:"question"`
	Answer   string `yaml:"answer"`
}

type testImport struct {
	Question string `yaml:"question"`
	Options  []struct {
		Text    string `yaml:"text"`
		Correct bool   `yaml:"correct"`
	} `yaml:"options"`
}

// parseCardImport reads cards from a CSV (term;definition) or JSON/YAML list of
// {question, answer}. Invalid entries are skipped (counted), matching the
// exercises importer rather than failing the whole file.
func parseCardImport(raw []byte) (cards []model.Card, skipped int, err error) {
	if looksStructured(raw) {
		var items []cardImport
		if err := yaml.Unmarshal(raw, &items); err != nil {
			return nil, 0, errors.New("invalid json/yaml")
		}
		for _, it := range items {
			q := strings.TrimSpace(it.Question)
			a := strings.TrimSpace(it.Answer)
			if q == "" || a == "" {
				skipped++
				continue
			}
			cards = append(cards, model.Card{Term: q, Definition: a})
		}
		return cards, skipped, nil
	}

	return nil, 0, errors.New("import must be a JSON or YAML list of {question, answer}")
}

// parseTestImport reads test questions from a CSV (question;flag;option;flag;option;…)
// or JSON/YAML list of {question, options:[{text, correct}]}. A valid question needs
// ≥2 options and at least one marked correct; invalid ones are skipped (counted).
func parseTestImport(raw []byte) (tqs []model.TestQuestion, skipped int, err error) {
	add := func(q string, opts []model.TestAnswer) {
		if q == "" || len(opts) < 2 || !hasCorrectOption(opts) {
			skipped++
			return
		}
		tqs = append(tqs, model.TestQuestion{Question: q, Options: opts})
	}

	if looksStructured(raw) {
		var items []testImport
		if err := yaml.Unmarshal(raw, &items); err != nil {
			return nil, 0, errors.New("invalid json/yaml")
		}
		for _, it := range items {
			var opts []model.TestAnswer
			for _, o := range it.Options {
				text := strings.TrimSpace(o.Text)
				if text == "" {
					continue
				}
				opts = append(opts, model.TestAnswer{Text: text, IsCorrect: o.Correct})
			}
			add(strings.TrimSpace(it.Question), opts)
		}
		return tqs, skipped, nil
	}

	return nil, 0, errors.New("import must be a JSON or YAML list of {question, options}")
}

func hasCorrectOption(opts []model.TestAnswer) bool {
	for _, o := range opts {
		if o.IsCorrect {
			return true
		}
	}
	return false
}

// readImportBody reads the uploaded "file" form field up to maxFileSize bytes.
func readImportBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	const maxFileSize = 4 << 20 // 4 MB
	if err := r.ParseMultipartForm(maxFileSize); err != nil {
		http.Error(w, "file too large", http.StatusBadRequest)
		return nil, false
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return nil, false
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxFileSize))
	if err != nil {
		http.Error(w, "could not read file", http.StatusBadRequest)
		return nil, false
	}
	return raw, true
}

// ImportCSV godoc
// @Summary      Bulk import cards from CSV (question;answer) or a JSON/YAML list
// @Tags         cards
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        file formData file true "CSV / JSON / YAML file"
// @Success      201 {object} object{imported=int,skipped=int}
// @Failure      400 {string} string
// @Router       /collections/{collectionID}/cards/import [post]
func (h *CardsHandler) ImportCSV(w http.ResponseWriter, r *http.Request) {
	raw, ok := readImportBody(w, r)
	if !ok {
		return
	}
	cards, skipped, err := parseCardImport(raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(cards) == 0 {
		http.Error(w, "no valid cards in file", http.StatusBadRequest)
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
	writeJSON(w, http.StatusCreated, map[string]int{"imported": len(cards), "skipped": skipped})
}

// ImportTests godoc
// @Summary      Bulk import test questions from CSV or a JSON/YAML list
// @Router       /collections/{collectionID}/tests/import [post]
func (h *CardsHandler) ImportTests(w http.ResponseWriter, r *http.Request) {
	raw, ok := readImportBody(w, r)
	if !ok {
		return
	}
	tqs, skipped, err := parseTestImport(raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(tqs) == 0 {
		http.Error(w, "no valid questions in file", http.StatusBadRequest)
		return
	}
	if len(tqs) > maxCSVRows {
		http.Error(w, fmt.Sprintf("too many rows (max %d)", maxCSVRows), http.StatusBadRequest)
		return
	}
	if err := h.svc.ImportTests(r.Context(), chi.URLParam(r, "collectionID"), h.claims(r).UserID, tqs); err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int{"imported": len(tqs), "skipped": skipped})
}

// AddTestQuestion godoc
// @Summary      Add a test question to a collection
// @Tags         tests
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        collectionID path string true "Collection ID"
// @Param        body body object{question=string,options=[]model.TestAnswer,position=int} true "Question data"
// @Success      201 {object} model.TestQuestion
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/tests [post]
func (h *CardsHandler) AddTestQuestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string             `json:"question"`
		Options  []model.TestAnswer `json:"options"`
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
// @Param        body body object{question=string,options=[]model.TestAnswer,position=int} true "Question data"
// @Success      200 {object} model.TestQuestion
// @Failure      404 {string} string
// @Router       /collections/{collectionID}/tests/{tqID} [put]
func (h *CardsHandler) UpdateTestQuestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Question string             `json:"question"`
		Options  []model.TestAnswer `json:"options"`
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
