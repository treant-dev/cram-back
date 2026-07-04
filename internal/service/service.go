package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/rank"
	"github.com/treant-dev/cram-go/internal/repository"
)

var ErrNotFound = errors.New("not found")
var ErrForbidden = errors.New("forbidden")
var ErrInvalidType = errors.New("invalid collection type for this operation")

// validCollectionTypes gates collection creation. Free-text in the DB (like roles) so a
// future "exercises" type only needs an entry here, no schema change.
var validCollectionTypes = map[string]bool{"cards": true, "tests": true, "exercises": true}

// PublicCollectionMeta is a collection enriched with follow data for the market page.
type PublicCollectionMeta struct {
	model.Collection
	FollowerCount int
	IsFollowed    bool
}

// UserWithCollections is a user summary with their public collections.
type UserWithCollections struct {
	ID          string
	Name        string
	Picture     string
	Role        string
	Collections []model.Collection
}

// HomeData is the response for GET /home.
type HomeData struct {
	Own       []model.Collection
	Following []model.Collection
}

// UpdateDraftReq carries the full content of a draft to be persisted.
type UpdateDraftReq struct {
	Title         string
	Description   string
	IsPublic      bool
	Cards         []DraftCardInput
	TestQuestions []DraftTestInput
}

// DraftCardInput carries card content for a draft update. ID identifies an existing
// item; empty means insert as new.
type DraftCardInput struct {
	ID         string
	Term       string
	Definition string
	Image      string
}

// DraftTestInput is accepted for API compatibility but ignored — tests are now quiz
// exercises edited outside the card draft editor.
type DraftTestInput struct {
	ID       string
	Question string
	Options  []model.TestAnswer
	Image    string
}

type collectionRepo interface {
	Create(ctx context.Context, userID, title, description, collType string, isPublic bool) (*model.Collection, error)
	ListByUser(ctx context.Context, userID string) ([]model.Collection, error)
	ListPublic(ctx context.Context) ([]model.Collection, error)
	ListPublicForUsers(ctx context.Context, userIDs []string) (map[string][]model.Collection, error)
	ListAllForUsers(ctx context.Context, userIDs []string) (map[string][]model.Collection, error)
	ListFollowedByUser(ctx context.Context, userID string) ([]model.Collection, error)
	GetByID(ctx context.Context, id, userID string, isAdmin bool) (*model.Collection, error)
	GetPublicByID(ctx context.Context, id string) (*model.Collection, error)
	ExistsForUser(ctx context.Context, id, userID string) (bool, error)
	Update(ctx context.Context, id, userID, title, description string, isPublic bool) (*model.Collection, error)
	Delete(ctx context.Context, id, userID string) error
	ForceDelete(ctx context.Context, id string) error
	ListAllImages(ctx context.Context, collectionID string) ([]string, error)
	ListUserImages(ctx context.Context, userID string) ([]string, error)
	// Share token operations
	GenerateShareToken(ctx context.Context, id, userID string) (string, error)
	RevokeShareToken(ctx context.Context, id, userID string) error
	GetByShareToken(ctx context.Context, token string) (*model.Collection, error)
}

type ImageStore interface {
	DeleteURL(ctx context.Context, url string) error
	DeleteURLs(ctx context.Context, urls []string)
}

type followRepo interface {
	Follow(ctx context.Context, userID, collectionID string) error
	Unfollow(ctx context.Context, userID, collectionID string) error
	FollowedByUser(ctx context.Context, userID string) (map[string]bool, error)
	CountsForCollections(ctx context.Context, collectionIDs []string) (map[string]int, error)
}

type userRepo interface {
	ListAll(ctx context.Context) ([]model.User, error)
	UpdateRole(ctx context.Context, userID, role string) error
	Delete(ctx context.Context, userID string) error
}

type itemRepo interface {
	Create(ctx context.Context, it model.Item) (*model.Item, error)
	Get(ctx context.Context, id, collectionID string) (*model.Item, error)
	ListByCollection(ctx context.Context, collectionID string) ([]model.Item, error)
	ListByParent(ctx context.Context, parentID string) ([]model.Item, error)
	Update(ctx context.Context, id, collectionID string, content map[string]any) (*model.Item, error)
	Delete(ctx context.Context, id, collectionID string) error
	LastRank(ctx context.Context, collectionID string, parentID *string) (string, error)
}

type itemProgressRepo interface {
	Get(ctx context.Context, userID, itemID string) (*model.ItemProgress, error)
	Upsert(ctx context.Context, userID, itemID string, level int, nextReviewAt time.Time) error
	ListByCollection(ctx context.Context, userID, collectionID string) ([]model.ItemProgress, error)
	ResetOne(ctx context.Context, userID, itemID string) error
	ResetCollection(ctx context.Context, userID, collectionID string) error
}

type itemEventRepo interface {
	Append(ctx context.Context, userID, itemID string, correct *bool, payload map[string]any) error
	LatestByCollection(ctx context.Context, userID, collectionID string) (map[string]model.ItemEvent, error)
}

type itemDraftRepo interface {
	Set(ctx context.Context, d model.ItemDraft) error
	ListByCollection(ctx context.Context, collectionID string) ([]model.ItemDraft, error)
	Clear(ctx context.Context, collectionID string) error
	Publish(ctx context.Context, collectionID string) error
}

type CollectionService struct {
	collections  collectionRepo
	follows      followRepo
	users        userRepo
	items        itemRepo
	itemProgress itemProgressRepo
	itemEvents   itemEventRepo
	itemDrafts   itemDraftRepo
	images       ImageStore
}

func NewCollectionService(
	collections *repository.CollectionRepository,
	follows *repository.FollowRepository,
	users *repository.UserRepository,
	items *repository.ItemRepository,
	itemProgress *repository.ItemProgressRepository,
	itemEvents *repository.ItemEventRepository,
	itemDrafts *repository.ItemDraftRepository,
) *CollectionService {
	return &CollectionService{
		collections:  collections,
		follows:      follows,
		users:        users,
		items:        items,
		itemProgress: itemProgress,
		itemEvents:   itemEvents,
		itemDrafts:   itemDrafts,
	}
}

func (s *CollectionService) SetImageStore(store ImageStore) { s.images = store }

// Collections

func (s *CollectionService) CreateCollection(ctx context.Context, userID, title, description, collType string, isPublic bool) (*model.Collection, error) {
	if collType == "" {
		collType = "cards"
	}
	if !validCollectionTypes[collType] {
		return nil, ErrInvalidType
	}
	return s.collections.Create(ctx, userID, title, description, collType, isPublic)
}

func (s *CollectionService) ListCollections(ctx context.Context, userID string) ([]model.Collection, error) {
	return s.collections.ListByUser(ctx, userID)
}

func (s *CollectionService) GetHome(ctx context.Context, userID string) (*HomeData, error) {
	own, err := s.collections.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	following, err := s.collections.ListFollowedByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if own == nil {
		own = []model.Collection{}
	}
	if following == nil {
		following = []model.Collection{}
	}
	return &HomeData{Own: own, Following: following}, nil
}

func (s *CollectionService) ListPublicWithMeta(ctx context.Context, userID string) ([]PublicCollectionMeta, error) {
	cols, err := s.collections.ListPublic(ctx)
	if err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return []PublicCollectionMeta{}, nil
	}
	ids := make([]string, len(cols))
	for i, c := range cols {
		ids[i] = c.ID
	}
	counts, err := s.follows.CountsForCollections(ctx, ids)
	if err != nil {
		return nil, err
	}
	var followed map[string]bool
	if userID != "" {
		followed, err = s.follows.FollowedByUser(ctx, userID)
		if err != nil {
			return nil, err
		}
	}
	result := make([]PublicCollectionMeta, len(cols))
	for i, c := range cols {
		result[i] = PublicCollectionMeta{
			Collection:    c,
			FollowerCount: counts[c.ID],
			IsFollowed:    followed[c.ID],
		}
	}
	return result, nil
}

func (s *CollectionService) GetPublicCollection(ctx context.Context, id string) (*model.Collection, error) {
	col, err := s.collections.GetPublicByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	items, err := s.items.ListByCollection(ctx, id)
	if err != nil {
		return nil, err
	}
	col.Items = items
	col.Cards, col.TestQuestions, col.Exercises = splitItems(items)
	return col, nil
}

func (s *CollectionService) GetCollection(ctx context.Context, id, userID string, isAdmin bool) (*model.Collection, error) {
	col, err := s.collections.GetByID(ctx, id, userID, isAdmin)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	items, err := s.items.ListByCollection(ctx, id)
	if err != nil {
		return nil, err
	}
	col.Items = items
	col.Cards, col.TestQuestions, col.Exercises = splitItems(items)

	// Populate DraftID for the owner when staged changes exist (item_draft rows).
	if col.UserID == userID {
		if drafts, err := s.itemDrafts.ListByCollection(ctx, id); err == nil && len(drafts) > 0 {
			col.DraftID = &col.ID
		}
	}
	return col, nil
}

func (s *CollectionService) UpdateCollection(ctx context.Context, id, userID, title, description string, isPublic bool) (*model.Collection, error) {
	col, err := s.collections.Update(ctx, id, userID, title, description, isPublic)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return col, err
}

func (s *CollectionService) DeleteCollection(ctx context.Context, id, userID string) error {
	if s.images != nil {
		if urls, err := s.collections.ListAllImages(ctx, id); err == nil {
			defer s.images.DeleteURLs(ctx, urls)
		}
	}
	return s.collections.Delete(ctx, id, userID)
}

func (s *CollectionService) ownsCollection(ctx context.Context, collectionID, userID string) error {
	exists, err := s.collections.ExistsForUser(ctx, collectionID, userID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (s *CollectionService) loadContent(ctx context.Context, col *model.Collection) error {
	items, err := s.items.ListByCollection(ctx, col.ID)
	if err != nil {
		return err
	}
	col.Items = items
	col.Cards, col.TestQuestions, col.Exercises = splitItems(items)
	return nil
}

// Drafts

// GetOrCreateDraft returns the existing draft or creates a new one from the active collection.
// GetOrCreateDraft returns the collection with the editor's overlay view
// (live items + staged item_draft). No separate draft collection — the draft is
// the staging rows, so DraftID is just the collection's own id.
func (s *CollectionService) GetOrCreateDraft(ctx context.Context, collectionID, userID string) (*model.Collection, error) {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return nil, err
	}
	col, err := s.collections.GetByID(ctx, collectionID, userID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	items, err := s.items.ListByCollection(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	drafts, err := s.itemDrafts.ListByCollection(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	overlay := overlayDraft(items, drafts)
	col.Items = overlay
	col.Cards, col.TestQuestions, col.Exercises = splitItems(overlay)
	col.DraftID = &col.ID
	return col, nil
}

// UpdateDraft stages the full desired card/test content into item_draft (readers
// still see live items until publish). Rebuilt from scratch each call: upsert every
// desired item, delete-mark live card/test items no longer present. Exercises are
// edited directly (YAML import), so they're left untouched here.
func (s *CollectionService) UpdateDraft(ctx context.Context, collectionID, userID string, req UpdateDraftReq) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	if _, err := s.collections.Update(ctx, collectionID, userID, req.Title, req.Description, req.IsPublic); err != nil {
		return err
	}
	if err := s.itemDrafts.Clear(ctx, collectionID); err != nil {
		return err
	}

	live, err := s.items.ListByCollection(ctx, collectionID)
	if err != nil {
		return err
	}
	liveRank := map[string]string{}
	liveCardTest := map[string]bool{}
	for _, it := range live {
		if it.Type == "card" || it.Type == "test" {
			liveRank[it.ID] = it.Rank
			liveCardTest[it.ID] = true
		}
	}
	last, err := s.items.LastRank(ctx, collectionID, nil)
	if err != nil {
		return err
	}

	desired := map[string]bool{}
	stage := func(id, typ string, content map[string]any) error {
		rk := liveRank[id]
		if id == "" {
			id = uuid.NewString()
			last = rank.After(last)
			rk = last
		}
		desired[id] = true
		t := typ
		return s.itemDrafts.Set(ctx, model.ItemDraft{ItemID: id, CollectionID: collectionID, Op: "upsert", Type: &t, Content: content, Rank: &rk})
	}
	for _, c := range req.Cards {
		if err := stage(c.ID, "card", cardContent(c.Term, c.Definition, c.Image)); err != nil {
			return err
		}
	}
	// Tests are now quiz exercises (created via "+ Test" / import), not part of the
	// card draft editor — req.TestQuestions is ignored.
	for id := range liveCardTest {
		if !desired[id] {
			if err := s.itemDrafts.Set(ctx, model.ItemDraft{ItemID: id, CollectionID: collectionID, Op: "delete"}); err != nil {
				return err
			}
		}
	}
	return nil
}

// DiscardDraft drops all staged changes.
func (s *CollectionService) DiscardDraft(ctx context.Context, collectionID, userID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	return s.itemDrafts.Clear(ctx, collectionID)
}

// PublishDraft applies the staged draft to live items (fires the history trigger).
func (s *CollectionService) PublishDraft(ctx context.Context, collectionID, userID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	return s.itemDrafts.Publish(ctx, collectionID)
}

// Follows

func (s *CollectionService) Follow(ctx context.Context, userID, collectionID string) error {
	col, err := s.collections.GetByID(ctx, collectionID, userID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if col.UserID == userID {
		return ErrForbidden
	}
	return s.follows.Follow(ctx, userID, collectionID)
}

func (s *CollectionService) Unfollow(ctx context.Context, userID, collectionID string) error {
	return s.follows.Unfollow(ctx, userID, collectionID)
}

// Users

func (s *CollectionService) ListUsers(ctx context.Context) ([]UserWithCollections, error) {
	users, err := s.users.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return []UserWithCollections{}, nil
	}
	ids := make([]string, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	colsByUser, err := s.collections.ListAllForUsers(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]UserWithCollections, len(users))
	for i, u := range users {
		cols := colsByUser[u.ID]
		if cols == nil {
			cols = []model.Collection{}
		}
		result[i] = UserWithCollections{
			ID:          u.ID,
			Name:        u.Name,
			Picture:     u.Picture,
			Role:        u.Role,
			Collections: cols,
		}
	}
	return result, nil
}

// Cards

// cardContent builds the JSONB body of a card item; image omitted when empty.
func cardContent(term, definition, image string) map[string]any {
	c := map[string]any{"term": term, "definition": definition}
	if image != "" {
		c["image"] = image
	}
	return c
}

func str(v any) string { s, _ := v.(string); return s }

// cardFromItem maps a card item back to the legacy Card DTO (handler contract).
func cardFromItem(it *model.Item) *model.Card {
	c := &model.Card{
		ID:         it.ID,
		Term:       str(it.Content["term"]),
		Definition: str(it.Content["definition"]),
		Image:      str(it.Content["image"]),
		CreatedAt:  it.CreatedAt,
		UpdatedAt:  it.UpdatedAt,
	}
	if it.CollectionID != nil {
		c.CollectionID = *it.CollectionID
	}
	return c
}

func boolOf(v any) bool { b, _ := v.(bool); return b }

func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, str(e))
	}
	return out
}

func stringMatrix(v any) [][]string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([][]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, stringSlice(e))
	}
	return out
}

// parseOptions reads MCQ options from item content (quiz exercises).
func parseOptions(v any) []model.TestAnswer {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []model.TestAnswer
	for _, o := range arr {
		if m, ok := o.(map[string]any); ok {
			out = append(out, model.TestAnswer{Text: str(m["text"]), IsCorrect: boolOf(m["is_correct"]), Explanation: str(m["explanation"])})
		}
	}
	return out
}

func sentenceFromItem(it model.Item) model.ExerciseSentence {
	return model.ExerciseSentence{ID: it.ID, Text: str(it.Content["text"]), Answer: stringSlice(it.Content["answer"]), Distractors: stringMatrix(it.Content["distractors"])}
}

func exerciseFromItem(it model.Item) model.Exercise {
	ex := model.Exercise{ID: it.ID, Kind: str(it.Content["kind"]), Title: str(it.Content["title"]), Distractors: stringSlice(it.Content["distractors"]), CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt}
	if it.CollectionID != nil {
		ex.CollectionID = *it.CollectionID
	}
	if ex.Kind == "quiz" { // a "test" is a quiz exercise: question + options in content
		ex.Question = str(it.Content["question"])
		ex.Options = parseOptions(it.Content["options"])
	}
	return ex
}

// splitItems maps unified items to the legacy DTO slices (transitional adapter —
// removed once the frontend consumes items[] directly). Sentences nest under their
// parent exercise via ParentID.
func splitItems(items []model.Item) ([]model.Card, []model.TestQuestion, []model.Exercise) {
	var cards []model.Card
	var tests []model.TestQuestion
	var exercises []model.Exercise
	for _, it := range items {
		switch it.Type {
		case "card":
			cards = append(cards, *cardFromItem(&it))
		case "exercise": // includes quiz (former "test")
			exercises = append(exercises, exerciseFromItem(it))
		}
	}
	byID := make(map[string]*model.Exercise, len(exercises))
	for i := range exercises {
		byID[exercises[i].ID] = &exercises[i]
	}
	for _, it := range items {
		if it.Type == "sentence" && it.ParentID != nil {
			if ex := byID[*it.ParentID]; ex != nil {
				ex.Sentences = append(ex.Sentences, sentenceFromItem(it))
			}
		}
	}
	return cards, tests, exercises
}

func derefStr(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}

// overlayDraft applies staged item_draft changes onto the live items — the editor's
// view. Readers still see plain live items; only the editor sees this overlay.
func overlayDraft(items []model.Item, drafts []model.ItemDraft) []model.Item {
	byID := make(map[string]model.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, d := range drafts {
		switch d.Op {
		case "delete":
			delete(byID, d.ItemID)
		case "upsert":
			cid := d.CollectionID
			byID[d.ItemID] = model.Item{ID: d.ItemID, Type: derefStr(d.Type), CollectionID: &cid, ParentID: d.ParentID, Content: d.Content, Rank: derefStr(d.Rank)}
		}
	}
	out := make([]model.Item, 0, len(byID))
	for _, it := range byID {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	return out
}

func testContent(question string, options []model.TestAnswer, image string) map[string]any {
	opts := make([]map[string]any, 0, len(options))
	for _, o := range options {
		m := map[string]any{"text": o.Text, "is_correct": o.IsCorrect}
		if o.Explanation != "" {
			m["explanation"] = o.Explanation
		}
		opts = append(opts, m)
	}
	c := map[string]any{"question": question, "options": opts}
	if image != "" {
		c["image"] = image
	}
	return c
}

func exerciseContent(kind, title string, distractors []string) map[string]any {
	return map[string]any{"kind": kind, "title": title, "distractors": distractors}
}

func sentenceContent(s model.ExerciseSentence) map[string]any {
	m := map[string]any{"text": s.Text, "answer": s.Answer}
	if len(s.Distractors) > 0 {
		m["distractors"] = s.Distractors
	}
	return m
}

func (s *CollectionService) AddCard(ctx context.Context, collectionID, userID, term, definition, image string, position int) (*model.Card, error) {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return nil, err
	}
	last, err := s.items.LastRank(ctx, collectionID, nil)
	if err != nil {
		return nil, err
	}
	it, err := s.items.Create(ctx, model.Item{
		Type:         "card",
		CollectionID: &collectionID,
		Content:      cardContent(term, definition, image),
		Rank:         rank.After(last),
	})
	if err != nil {
		return nil, err
	}
	return cardFromItem(it), nil
}

func (s *CollectionService) UpdateCard(ctx context.Context, cardID, collectionID, userID, term, definition, image string, position int) (*model.Card, error) {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return nil, err
	}
	it, err := s.items.Update(ctx, cardID, collectionID, cardContent(term, definition, image))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return cardFromItem(it), nil
}

func (s *CollectionService) DeleteCard(ctx context.Context, cardID, collectionID, userID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	var imageURL string
	if it, err := s.items.Get(ctx, cardID, collectionID); err == nil && it != nil {
		imageURL = str(it.Content["image"])
	}
	if err := s.items.Delete(ctx, cardID, collectionID); err != nil {
		return err
	}
	if s.images != nil && imageURL != "" {
		_ = s.images.DeleteURL(ctx, imageURL)
	}
	return nil
}

func (s *CollectionService) ImportCards(ctx context.Context, collectionID, userID string, cards []model.Card) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	prev, err := s.items.LastRank(ctx, collectionID, nil)
	if err != nil {
		return err
	}
	for _, c := range cards {
		prev = rank.After(prev)
		if _, err := s.items.Create(ctx, model.Item{
			Type:         "card",
			CollectionID: &collectionID,
			Content:      cardContent(c.Term, c.Definition, c.Image),
			Rank:         prev,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *CollectionService) ImportTests(ctx context.Context, collectionID, userID string, tqs []model.TestQuestion) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	prev, err := s.items.LastRank(ctx, collectionID, nil)
	if err != nil {
		return err
	}
	for _, tq := range tqs {
		prev = rank.After(prev)
		if _, err := s.items.Create(ctx, model.Item{
			Type: "exercise", CollectionID: &collectionID,
			Content: quizContent(tq.Question, tq.Options), Rank: prev,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *CollectionService) ImportExercises(ctx context.Context, collectionID, userID string, exercises []model.Exercise) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	prev, err := s.items.LastRank(ctx, collectionID, nil)
	if err != nil {
		return err
	}
	for _, ex := range exercises {
		prev = rank.After(prev)
		exItem, err := s.items.Create(ctx, model.Item{
			Type: "exercise", CollectionID: &collectionID,
			Content: exerciseContent(ex.Kind, ex.Title, ex.Distractors), Rank: prev,
		})
		if err != nil {
			return err
		}
		sprev := ""
		for _, sent := range ex.Sentences {
			sprev = rank.After(sprev)
			if _, err := s.items.Create(ctx, model.Item{
				Type: "sentence", CollectionID: &collectionID, ParentID: &exItem.ID,
				Content: sentenceContent(sent), Rank: sprev,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteExercise removes the exercise item; its sentence children cascade via
// items.parent_id ON DELETE CASCADE.
func (s *CollectionService) DeleteExercise(ctx context.Context, exID, collectionID, userID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	return s.items.Delete(ctx, exID, collectionID)
}

// Test questions

// quizContent builds a quiz exercise body (a "test" is a quiz-kind exercise).
func quizContent(question string, options []model.TestAnswer) map[string]any {
	c := testContent(question, options, "")
	c["kind"] = "quiz"
	return c
}

func (s *CollectionService) AddTestQuestion(ctx context.Context, collectionID, userID, question string, options []model.TestAnswer, image string, position int) (*model.TestQuestion, error) {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return nil, err
	}
	last, err := s.items.LastRank(ctx, collectionID, nil)
	if err != nil {
		return nil, err
	}
	it, err := s.items.Create(ctx, model.Item{
		Type: "exercise", CollectionID: &collectionID,
		Content: quizContent(question, options), Rank: rank.After(last),
	})
	if err != nil {
		return nil, err
	}
	return &model.TestQuestion{ID: it.ID, CollectionID: collectionID, Question: question, Options: options}, nil
}

func (s *CollectionService) UpdateTestQuestion(ctx context.Context, tqID, collectionID, userID, question string, options []model.TestAnswer, image string, position int) (*model.TestQuestion, error) {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return nil, err
	}
	it, err := s.items.Update(ctx, tqID, collectionID, quizContent(question, options))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &model.TestQuestion{ID: it.ID, CollectionID: collectionID, Question: question, Options: options}, nil
}

func (s *CollectionService) DeleteTestQuestion(ctx context.Context, tqID, collectionID, userID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	var imageURL string
	if it, err := s.items.Get(ctx, tqID, collectionID); err == nil && it != nil {
		imageURL = str(it.Content["image"])
	}
	if err := s.items.Delete(ctx, tqID, collectionID); err != nil {
		return err
	}
	if s.images != nil && imageURL != "" {
		_ = s.images.DeleteURL(ctx, imageURL)
	}
	return nil
}

var validRoles = map[string]bool{"user": true, "pro": true, "admin": true}

func (s *CollectionService) SetUserRole(ctx context.Context, targetUserID, role string) error {
	if !validRoles[role] {
		return fmt.Errorf("invalid role: %s", role)
	}
	return s.users.UpdateRole(ctx, targetUserID, role)
}

func (s *CollectionService) DeleteAccount(ctx context.Context, userID string) error {
	if s.images != nil {
		if urls, err := s.collections.ListUserImages(ctx, userID); err == nil {
			defer s.images.DeleteURLs(ctx, urls)
		}
	}
	return s.users.Delete(ctx, userID)
}

func (s *CollectionService) GenerateShareToken(ctx context.Context, id, userID string) (string, error) {
	return s.collections.GenerateShareToken(ctx, id, userID)
}

func (s *CollectionService) RevokeShareToken(ctx context.Context, id, userID string) error {
	return s.collections.RevokeShareToken(ctx, id, userID)
}

func (s *CollectionService) GetByShareToken(ctx context.Context, token string) (*model.Collection, error) {
	col, err := s.collections.GetByShareToken(ctx, token)
	if err != nil {
		return nil, ErrNotFound
	}
	if err := s.loadContent(ctx, col); err != nil {
		return nil, err
	}
	return col, nil
}

func (s *CollectionService) AdminDeleteCollection(ctx context.Context, collectionID string) error {
	if s.images != nil {
		if urls, err := s.collections.ListAllImages(ctx, collectionID); err == nil {
			defer s.images.DeleteURLs(ctx, urls)
		}
	}
	return s.collections.ForceDelete(ctx, collectionID)
}

// Progress

// GetProgress returns the user's card spaced-rep state (cards only; tests/exercises
// don't track progress). Shaped as legacy ProgressData for the handler contract.
func (s *CollectionService) GetProgress(ctx context.Context, collectionID, userID string) (*repository.ProgressData, error) {
	rows, err := s.itemProgress.ListByCollection(ctx, userID, collectionID)
	if err != nil {
		return nil, err
	}
	data := &repository.ProgressData{
		Cards: make(map[string]repository.ProgressEntry, len(rows)),
		TQs:   map[string]repository.ProgressEntry{},
	}
	for _, p := range rows {
		data.Cards[p.ItemID] = repository.ProgressEntry{Level: p.Level, NextReviewAt: p.NextReviewAt, LastReviewAt: p.LastReviewAt}
	}
	return data, nil
}

// ResetCollectionProgress clears all of the owner's progress for a collection.
func (s *CollectionService) ResetCollectionProgress(ctx context.Context, collectionID, userID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	return s.itemProgress.ResetCollection(ctx, userID, collectionID)
}

// ResetItemProgress clears the owner's progress for a single card (tests have none).
func (s *CollectionService) ResetItemProgress(ctx context.Context, collectionID, userID, itemType, itemID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	if itemType != "card" {
		return nil // only cards carry progress
	}
	return s.itemProgress.ResetOne(ctx, userID, itemID)
}

// ResetExerciseProgress "retakes" one exercise: appends a reset marker event per
// sentence (correct=nil), so the worksheet renders blank while history is kept.
func (s *CollectionService) ResetExerciseProgress(ctx context.Context, collectionID, userID, exerciseID string) error {
	sentences, err := s.items.ListByParent(ctx, exerciseID)
	if err != nil {
		return err
	}
	// quiz records its answer on the exercise id itself; bank/choice on child sentence
	// ids — reset a marker for both so every kind clears.
	ids := []string{exerciseID}
	for _, sent := range sentences {
		ids = append(ids, sent.ID)
	}
	for _, id := range ids {
		if err := s.itemEvents.Append(ctx, userID, id, nil, map[string]any{}); err != nil {
			return err
		}
	}
	return nil
}

// UpdateProgress applies answer result then confidence delta to compute the new level,
// persists it, and returns the resulting level and next review time.
func (s *CollectionService) UpdateProgress(ctx context.Context, userID, collectionID, itemType, itemID string, correct bool, confidenceDelta int, retry bool) (int, time.Time, error) {
	if itemType != "card" {
		return 0, time.Time{}, nil // only cards carry spaced-rep progress
	}
	current, currentNextReview := 1, time.Time{} // default for a card never studied
	if p, err := s.itemProgress.Get(ctx, userID, itemID); err != nil {
		return 0, time.Time{}, err
	} else if p != nil {
		current, currentNextReview = p.Level, p.NextReviewAt
	}

	isDue := currentNextReview.IsZero() || !time.Now().Before(currentNextReview)

	var level int
	if retry {
		// Retry pass (blitz "repeat the mistake"): a correct answer redeems +1
		// bypassing the due-date gate; a wrong answer is not penalised again.
		if correct {
			level = progressRetryBump(current)
		} else {
			level = current
		}
	} else {
		level = progressApplyAnswer(current, correct, currentNextReview)
		level = progressApplyConfidence(level, confidenceDelta)
	}

	// Only update next_review_at when the item was due, reached mastery,
	// was answered incorrectly, confidence was lowered, or this is a retry pass.
	var nextReview time.Time
	if isDue || level == 7 || !correct || confidenceDelta == -1 || retry {
		nextReview = progressNextReview(level)
	} else {
		nextReview = currentNextReview
	}

	err := s.itemProgress.Upsert(ctx, userID, itemID, level, nextReview)
	return level, nextReview, err
}

// SentenceResult is one graded sentence from an exercise worksheet.
type SentenceResult struct {
	SentenceID string
	Correct    bool
	Submitted  []string
}

// RecordSentenceResults saves each sentence's latest answer (words placed + correctness).
// Exercises are one-off worksheets — no spaced-repetition. Per-user, no ownership needed.
func (s *CollectionService) RecordSentenceResults(ctx context.Context, userID string, results []SentenceResult) error {
	for _, r := range results {
		correct := r.Correct
		if err := s.itemEvents.Append(ctx, userID, r.SentenceID, &correct, map[string]any{"submitted": r.Submitted}); err != nil {
			return err
		}
	}
	return nil
}

// GetExerciseResults returns the user's latest answer per sentence (from the event
// log). Reset markers (correct=nil) mean the sentence is blank and are skipped.
func (s *CollectionService) GetExerciseResults(ctx context.Context, collectionID, userID string) (map[string]repository.SentenceResultEntry, error) {
	latest, err := s.itemEvents.LatestByCollection(ctx, userID, collectionID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]repository.SentenceResultEntry)
	for id, e := range latest {
		if e.Correct == nil {
			continue
		}
		out[id] = repository.SentenceResultEntry{Correct: *e.Correct, Submitted: stringSlice(e.Payload["submitted"])}
	}
	return out, nil
}

// BlitzItem is one item in a blitz session.
type BlitzItem struct {
	Type string              `json:"type"` // "card" or "tq"
	Card *model.Card         `json:"card,omitempty"`
	TQ   *model.TestQuestion `json:"tq,omitempty"`
}

// BlitzCardTerm is a lightweight card entry used for distractor generation on the client.
// Both Term and Definition are sent so the client can build options in either direction.
type BlitzCardTerm struct {
	ID         string `json:"ID"`
	Term       string `json:"Term"`
	Definition string `json:"Definition"`
}

// BlitzResult is the response for GET /collections/{id}/blitz.
type BlitzResult struct {
	Items    []BlitzItem     `json:"items"`
	CardPool []BlitzCardTerm `json:"card_pool"`
}

// GetBlitz returns up to 7 non-mastered items prioritised by due date,
// with the due group randomly ordered and the not-yet-due group sorted
// by last_review_at ASC (oldest reviewed first).
func (s *CollectionService) GetBlitz(ctx context.Context, collectionID, userID string) (*BlitzResult, error) {
	allItems, err := s.items.ListByCollection(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	cards, _, _ := splitItems(allItems)

	prog, err := s.GetProgress(ctx, collectionID, userID)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	type candidate struct {
		idx      int
		lastSeen *time.Time
	}

	var due, notDue []candidate
	for i, card := range cards {
		entry, has := prog.Cards[card.ID]
		if has && entry.Level == 7 {
			continue
		}
		isDue := !has || !now.Before(entry.NextReviewAt)
		var ls *time.Time
		if has {
			ls = entry.LastReviewAt
		}
		c := candidate{idx: i, lastSeen: ls}
		if isDue {
			due = append(due, c)
		} else {
			notDue = append(notDue, c)
		}
	}

	rand.Shuffle(len(due), func(i, j int) { due[i], due[j] = due[j], due[i] })
	sort.Slice(notDue, func(i, j int) bool {
		if notDue[i].lastSeen == nil {
			return true
		}
		if notDue[j].lastSeen == nil {
			return false
		}
		return notDue[i].lastSeen.Before(*notDue[j].lastSeen)
	})

	combined := append(due, notDue...)
	if len(combined) > 7 {
		combined = combined[:7]
	}

	blitzItems := make([]BlitzItem, 0, len(combined))
	for _, c := range combined {
		card := cards[c.idx]
		blitzItems = append(blitzItems, BlitzItem{Type: "card", Card: &card})
	}

	pool := make([]BlitzCardTerm, len(cards))
	for i, c := range cards {
		pool[i] = BlitzCardTerm{ID: c.ID, Term: c.Term, Definition: c.Definition}
	}

	return &BlitzResult{Items: blitzItems, CardPool: pool}, nil
}

func progressApplyAnswer(level int, correct bool, nextReviewAt time.Time) int {
	if level == 7 {
		return 7
	}
	if correct {
		if level == 1 {
			return 2 // always allow level 1 → 2
		}
		if time.Now().Before(nextReviewAt) {
			return level // answered before due date — no level change
		}
		return min(level+1, 6)
	}
	return max(1, level/2)
}

// progressRetryBump redeems a single level on a correct retry answer, ignoring the
// due-date gate (the item was just answered wrong, so it is never "due"). Capped at 6
// so a retry cannot reach mastery on its own.
func progressRetryBump(level int) int {
	if level == 7 {
		return 7
	}
	return min(level+1, 6)
}

func progressApplyConfidence(level int, delta int) int {
	switch {
	case delta == 1 && level >= 6:
		return 7
	case delta == 1:
		return level + 1
	case delta == -1:
		return max(1, level/2)
	default:
		return level
	}
}

func progressNextReview(level int) time.Time {
	now := time.Now()
	switch level {
	case 1:
		return now.AddDate(0, 0, 1)
	case 2:
		return now.AddDate(0, 0, 2)
	case 3:
		return now.AddDate(0, 0, 7)
	case 4:
		return now.AddDate(0, 0, 14)
	case 5:
		return now.AddDate(0, 1, 0)
	case 6:
		return now.AddDate(0, 6, 0)
	default:
		return time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)
	}
}
