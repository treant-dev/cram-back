package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/treant-dev/cram-go/internal/model"
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
	Cards         []repository.DraftCardInput
	TestQuestions []repository.DraftTestInput
}

type collectionRepo interface {
	Create(ctx context.Context, userID, title, description, collType string, isPublic bool) (*model.Collection, error)
	GetType(ctx context.Context, id string) (string, error)
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
	// Draft operations
	GetDraftFor(ctx context.Context, collectionID, userID string) (*model.Collection, error)
	CreateDraftFrom(ctx context.Context, collectionID, userID string) (*model.Collection, error)
	UpdateDraftContent(ctx context.Context, draftID, userID, title, description string, isPublic bool, cards []repository.DraftCardInput, tests []repository.DraftTestInput) error
	PublishDraft(ctx context.Context, collectionID, userID string) error
	DeleteDraft(ctx context.Context, collectionID, userID string) error
	// Share token operations
	GenerateShareToken(ctx context.Context, id, userID string) (string, error)
	RevokeShareToken(ctx context.Context, id, userID string) error
	GetByShareToken(ctx context.Context, token string) (*model.Collection, error)
}

type cardRepo interface {
	Create(ctx context.Context, collectionID, term, definition, image string, position int) (*model.Card, error)
	ListByCollection(ctx context.Context, collectionID string) ([]model.Card, error)
	Update(ctx context.Context, id, collectionID, term, definition, image string, position int) (*model.Card, error)
	Delete(ctx context.Context, id, collectionID string) (string, error)
	BulkCreate(ctx context.Context, collectionID string, cards []model.Card) error
}

type testQuestionRepo interface {
	Create(ctx context.Context, collectionID, question string, options []model.TestAnswer, image string, position int) (*model.TestQuestion, error)
	ListByCollection(ctx context.Context, collectionID string) ([]model.TestQuestion, error)
	Update(ctx context.Context, id, collectionID, question string, options []model.TestAnswer, image string, position int) (*model.TestQuestion, error)
	Delete(ctx context.Context, id, collectionID string) (string, error)
	BulkCreate(ctx context.Context, collectionID string, tqs []model.TestQuestion) error
}

type exerciseRepo interface {
	ListByCollection(ctx context.Context, collectionID string) ([]model.Exercise, error)
	BulkCreate(ctx context.Context, collectionID string, exercises []model.Exercise) error
	Delete(ctx context.Context, id, collectionID string) error
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

type studyRepo interface {
	SubmitSession(ctx context.Context, userID, sessionID, collectionID string, answers []repository.StudyAnswer) error
	GetHistory(ctx context.Context, collectionID, userID string, days int) (*repository.StudyHistoryData, error)
}

type progressRepo interface {
	GetForCollection(ctx context.Context, collectionID, userID string) (*repository.ProgressData, error)
	GetCardProgress(ctx context.Context, userID, cardID string) (int, time.Time)
	GetTQProgress(ctx context.Context, userID, tqID string) (int, time.Time)
	UpsertCard(ctx context.Context, userID, cardID string, level int, nextReview time.Time) error
	UpsertTQ(ctx context.Context, userID, tqID string, level int, nextReview time.Time) error
	RecordSentence(ctx context.Context, userID, sentenceID string, correct bool, submitted []string) error
	GetResultsForCollection(ctx context.Context, collectionID, userID string) (map[string]repository.SentenceResultEntry, error)
	ResetCollection(ctx context.Context, collectionID, userID string) error
	ResetExercise(ctx context.Context, userID, exerciseID string) error
	ResetCard(ctx context.Context, userID, cardID string) error
	ResetTQ(ctx context.Context, userID, tqID string) error
}

type CollectionService struct {
	collections   collectionRepo
	cards         cardRepo
	testQuestions testQuestionRepo
	exercises     exerciseRepo
	follows       followRepo
	users         userRepo
	study         studyRepo
	progress      progressRepo
	images        ImageStore
}

func NewCollectionService(
	collections *repository.CollectionRepository,
	cards *repository.CardRepository,
	tq *repository.TestQuestionRepository,
	exercises *repository.ExerciseRepository,
	follows *repository.FollowRepository,
	users *repository.UserRepository,
	study *repository.StudyRepository,
	progress *repository.ProgressRepository,
) *CollectionService {
	return &CollectionService{
		collections:   collections,
		cards:         cards,
		testQuestions: tq,
		exercises:     exercises,
		follows:       follows,
		users:         users,
		study:         study,
		progress:      progress,
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
	cards, err := s.cards.ListByCollection(ctx, id)
	if err != nil {
		return nil, err
	}
	tqs, err := s.testQuestions.ListByCollection(ctx, id)
	if err != nil {
		return nil, err
	}
	exs, err := s.exercises.ListByCollection(ctx, id)
	if err != nil {
		return nil, err
	}
	col.Cards = cards
	col.TestQuestions = tqs
	col.Exercises = exs
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
	cards, err := s.cards.ListByCollection(ctx, id)
	if err != nil {
		return nil, err
	}
	tqs, err := s.testQuestions.ListByCollection(ctx, id)
	if err != nil {
		return nil, err
	}
	exs, err := s.exercises.ListByCollection(ctx, id)
	if err != nil {
		return nil, err
	}
	col.Cards = cards
	col.TestQuestions = tqs
	col.Exercises = exs

	// Populate DraftID for the owner so the frontend knows a draft exists.
	if col.UserID == userID {
		if draft, err := s.collections.GetDraftFor(ctx, id, userID); err == nil {
			col.DraftID = &draft.ID
		}
	}
	return col, nil
}

func (s *CollectionService) SubmitStudySession(ctx context.Context, userID, sessionID, collectionID string, answers []repository.StudyAnswer) error {
	return s.study.SubmitSession(ctx, userID, sessionID, collectionID, answers)
}

func (s *CollectionService) GetStudyHistory(ctx context.Context, collectionID, userID string, days int) (*repository.StudyHistoryData, error) {
	return s.study.GetHistory(ctx, collectionID, userID, days)
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

// ownsCollectionOfType checks ownership and that the collection is of the expected
// type, so cards can't be added to a tests collection and vice versa.
func (s *CollectionService) ownsCollectionOfType(ctx context.Context, collectionID, userID, want string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	t, err := s.collections.GetType(ctx, collectionID)
	if err != nil {
		return err
	}
	if t != want {
		return ErrInvalidType
	}
	return nil
}

func (s *CollectionService) loadContent(ctx context.Context, col *model.Collection) error {
	cards, err := s.cards.ListByCollection(ctx, col.ID)
	if err != nil {
		return err
	}
	tests, err := s.testQuestions.ListByCollection(ctx, col.ID)
	if err != nil {
		return err
	}
	exs, err := s.exercises.ListByCollection(ctx, col.ID)
	if err != nil {
		return err
	}
	col.Cards = cards
	col.TestQuestions = tests
	col.Exercises = exs
	return nil
}

// Drafts

// GetOrCreateDraft returns the existing draft or creates a new one from the active collection.
func (s *CollectionService) GetOrCreateDraft(ctx context.Context, collectionID, userID string) (*model.Collection, error) {
	draft, err := s.collections.GetDraftFor(ctx, collectionID, userID)
	if err != nil {
		// Verify ownership before creating.
		if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
			return nil, err
		}
		draft, err = s.collections.CreateDraftFrom(ctx, collectionID, userID)
		if err != nil {
			return nil, err
		}
	}
	if err := s.loadContent(ctx, draft); err != nil {
		return nil, err
	}
	return draft, nil
}

// UpdateDraft saves draft content without publishing.
func (s *CollectionService) UpdateDraft(ctx context.Context, collectionID, userID string, req UpdateDraftReq) error {
	draft, err := s.collections.GetDraftFor(ctx, collectionID, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.collections.UpdateDraftContent(ctx, draft.ID, userID, req.Title, req.Description, req.IsPublic, req.Cards, req.TestQuestions)
}

// DiscardDraft deletes the draft.
func (s *CollectionService) DiscardDraft(ctx context.Context, collectionID, userID string) error {
	return s.collections.DeleteDraft(ctx, collectionID, userID)
}

// PublishDraft promotes the draft to the active version.
func (s *CollectionService) PublishDraft(ctx context.Context, collectionID, userID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	if err := s.collections.PublishDraft(ctx, collectionID, userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
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

func (s *CollectionService) AddCard(ctx context.Context, collectionID, userID, term, definition, image string, position int) (*model.Card, error) {
	if err := s.ownsCollectionOfType(ctx, collectionID, userID, "cards"); err != nil {
		return nil, err
	}
	return s.cards.Create(ctx, collectionID, term, definition, image, position)
}

func (s *CollectionService) UpdateCard(ctx context.Context, cardID, collectionID, userID, term, definition, image string, position int) (*model.Card, error) {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return nil, err
	}
	card, err := s.cards.Update(ctx, cardID, collectionID, term, definition, image, position)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return card, err
}

func (s *CollectionService) DeleteCard(ctx context.Context, cardID, collectionID, userID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	imageURL, err := s.cards.Delete(ctx, cardID, collectionID)
	if err != nil {
		return err
	}
	if s.images != nil {
		_ = s.images.DeleteURL(ctx, imageURL)
	}
	return nil
}

func (s *CollectionService) ImportCards(ctx context.Context, collectionID, userID string, cards []model.Card) error {
	if err := s.ownsCollectionOfType(ctx, collectionID, userID, "cards"); err != nil {
		return err
	}
	return s.cards.BulkCreate(ctx, collectionID, cards)
}

func (s *CollectionService) ImportTests(ctx context.Context, collectionID, userID string, tqs []model.TestQuestion) error {
	if err := s.ownsCollectionOfType(ctx, collectionID, userID, "tests"); err != nil {
		return err
	}
	return s.testQuestions.BulkCreate(ctx, collectionID, tqs)
}

func (s *CollectionService) ImportExercises(ctx context.Context, collectionID, userID string, exercises []model.Exercise) error {
	if err := s.ownsCollectionOfType(ctx, collectionID, userID, "exercises"); err != nil {
		return err
	}
	return s.exercises.BulkCreate(ctx, collectionID, exercises)
}

func (s *CollectionService) DeleteExercise(ctx context.Context, exID, collectionID, userID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	return s.exercises.Delete(ctx, exID, collectionID)
}

// Test questions

func (s *CollectionService) AddTestQuestion(ctx context.Context, collectionID, userID, question string, options []model.TestAnswer, image string, position int) (*model.TestQuestion, error) {
	if err := s.ownsCollectionOfType(ctx, collectionID, userID, "tests"); err != nil {
		return nil, err
	}
	return s.testQuestions.Create(ctx, collectionID, question, options, image, position)
}

func (s *CollectionService) UpdateTestQuestion(ctx context.Context, tqID, collectionID, userID, question string, options []model.TestAnswer, image string, position int) (*model.TestQuestion, error) {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return nil, err
	}
	tq, err := s.testQuestions.Update(ctx, tqID, collectionID, question, options, image, position)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return tq, err
}

func (s *CollectionService) DeleteTestQuestion(ctx context.Context, tqID, collectionID, userID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	imageURL, err := s.testQuestions.Delete(ctx, tqID, collectionID)
	if err != nil {
		return err
	}
	if s.images != nil {
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

func (s *CollectionService) GetProgress(ctx context.Context, collectionID, userID string) (*repository.ProgressData, error) {
	return s.progress.GetForCollection(ctx, collectionID, userID)
}

// ResetCollectionProgress clears all of the owner's progress for a collection.
func (s *CollectionService) ResetCollectionProgress(ctx context.Context, collectionID, userID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	return s.progress.ResetCollection(ctx, collectionID, userID)
}

// ResetItemProgress clears the owner's progress for a single card or test question.
func (s *CollectionService) ResetItemProgress(ctx context.Context, collectionID, userID, itemType, itemID string) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	if itemType == "card" {
		return s.progress.ResetCard(ctx, userID, itemID)
	}
	return s.progress.ResetTQ(ctx, userID, itemID)
}

// ResetExerciseProgress clears the user's own saved answers for one exercise (retake).
// Per-user data, so no ownership check — anyone studying the exercise can retake it.
func (s *CollectionService) ResetExerciseProgress(ctx context.Context, collectionID, userID, exerciseID string) error {
	return s.progress.ResetExercise(ctx, userID, exerciseID)
}

// UpdateProgress applies answer result then confidence delta to compute the new level,
// persists it, and returns the resulting level and next review time.
func (s *CollectionService) UpdateProgress(ctx context.Context, userID, collectionID, itemType, itemID string, correct bool, confidenceDelta int, retry bool) (int, time.Time, error) {
	var current int
	var currentNextReview time.Time
	if itemType == "card" {
		current, currentNextReview = s.progress.GetCardProgress(ctx, userID, itemID)
	} else {
		current, currentNextReview = s.progress.GetTQProgress(ctx, userID, itemID)
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

	var err error
	if itemType == "card" {
		err = s.progress.UpsertCard(ctx, userID, itemID, level, nextReview)
	} else {
		err = s.progress.UpsertTQ(ctx, userID, itemID, level, nextReview)
	}
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
		if err := s.progress.RecordSentence(ctx, userID, r.SentenceID, r.Correct, r.Submitted); err != nil {
			return err
		}
	}
	return nil
}

// GetExerciseResults returns the user's saved answers for a collection's sentences.
func (s *CollectionService) GetExerciseResults(ctx context.Context, collectionID, userID string) (map[string]repository.SentenceResultEntry, error) {
	return s.progress.GetResultsForCollection(ctx, collectionID, userID)
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
	cards, err := s.cards.ListByCollection(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	tqs, err := s.testQuestions.ListByCollection(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	progress, err := s.progress.GetForCollection(ctx, collectionID, userID)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	type candidate struct {
		cardIdx  int
		tqIdx    int
		lastSeen *time.Time
	}

	var due, notDue []candidate

	for i, card := range cards {
		entry, has := progress.Cards[card.ID]
		if has && entry.Level == 7 {
			continue
		}
		isDue := !has || !now.Before(entry.NextReviewAt)
		var ls *time.Time
		if has {
			ls = entry.LastReviewAt
		}
		c := candidate{cardIdx: i, tqIdx: -1, lastSeen: ls}
		if isDue {
			due = append(due, c)
		} else {
			notDue = append(notDue, c)
		}
	}

	for i, tq := range tqs {
		entry, has := progress.TQs[tq.ID]
		if has && entry.Level == 7 {
			continue
		}
		isDue := !has || !now.Before(entry.NextReviewAt)
		var ls *time.Time
		if has {
			ls = entry.LastReviewAt
		}
		c := candidate{cardIdx: -1, tqIdx: i, lastSeen: ls}
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

	items := make([]BlitzItem, 0, len(combined))
	for _, c := range combined {
		if c.cardIdx >= 0 {
			card := cards[c.cardIdx]
			items = append(items, BlitzItem{Type: "card", Card: &card})
		} else {
			tq := tqs[c.tqIdx]
			items = append(items, BlitzItem{Type: "tq", TQ: &tq})
		}
	}

	pool := make([]BlitzCardTerm, len(cards))
	for i, c := range cards {
		pool[i] = BlitzCardTerm{ID: c.ID, Term: c.Term, Definition: c.Definition}
	}

	return &BlitzResult{Items: items, CardPool: pool}, nil
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
