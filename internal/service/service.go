package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/repository"
)

var ErrNotFound = errors.New("not found")

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
	Create(ctx context.Context, userID, title, description string, isPublic bool) (*model.Collection, error)
	ListByUser(ctx context.Context, userID string) ([]model.Collection, error)
	ListPublic(ctx context.Context) ([]model.Collection, error)
	ListPublicForUsers(ctx context.Context, userIDs []string) (map[string][]model.Collection, error)
	ListAllForUsers(ctx context.Context, userIDs []string) (map[string][]model.Collection, error)
	ListFollowedByUser(ctx context.Context, userID string) ([]model.Collection, error)
	GetByID(ctx context.Context, id, userID string, isAdmin bool) (*model.Collection, error)
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
}

type CollectionService struct {
	collections   collectionRepo
	cards         cardRepo
	testQuestions testQuestionRepo
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
	follows *repository.FollowRepository,
	users *repository.UserRepository,
	study *repository.StudyRepository,
	progress *repository.ProgressRepository,
) *CollectionService {
	return &CollectionService{
		collections:   collections,
		cards:         cards,
		testQuestions: tq,
		follows:       follows,
		users:         users,
		study:         study,
		progress:      progress,
	}
}

func (s *CollectionService) SetImageStore(store ImageStore) { s.images = store }

// Collections

func (s *CollectionService) CreateCollection(ctx context.Context, userID, title, description string, isPublic bool) (*model.Collection, error) {
	return s.collections.Create(ctx, userID, title, description, isPublic)
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
	col.Cards = cards
	col.TestQuestions = tqs

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

func (s *CollectionService) loadContent(ctx context.Context, col *model.Collection) error {
	cards, err := s.cards.ListByCollection(ctx, col.ID)
	if err != nil {
		return err
	}
	tests, err := s.testQuestions.ListByCollection(ctx, col.ID)
	if err != nil {
		return err
	}
	col.Cards = cards
	col.TestQuestions = tests
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
	_, err := s.collections.GetByID(ctx, collectionID, userID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
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
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
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
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	return s.cards.BulkCreate(ctx, collectionID, cards)
}

func (s *CollectionService) ImportTests(ctx context.Context, collectionID, userID string, tqs []model.TestQuestion) error {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return err
	}
	return s.testQuestions.BulkCreate(ctx, collectionID, tqs)
}

// Test questions

func (s *CollectionService) AddTestQuestion(ctx context.Context, collectionID, userID, question string, options []model.TestAnswer, image string, position int) (*model.TestQuestion, error) {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
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

var validRoles = map[string]bool{"user": true, "premium": true, "admin": true}

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

// UpdateProgress applies answer result then confidence delta to compute the new level,
// persists it, and returns the resulting level and next review time.
func (s *CollectionService) UpdateProgress(ctx context.Context, userID, collectionID, itemType, itemID string, correct bool, confidenceDelta int) (int, time.Time, error) {
	var current int
	var currentNextReview time.Time
	if itemType == "card" {
		current, currentNextReview = s.progress.GetCardProgress(ctx, userID, itemID)
	} else {
		current, currentNextReview = s.progress.GetTQProgress(ctx, userID, itemID)
	}

	level := progressApplyAnswer(current, correct, currentNextReview)
	level = progressApplyConfidence(level, confidenceDelta)
	nextReview := progressNextReview(level)

	var err error
	if itemType == "card" {
		err = s.progress.UpsertCard(ctx, userID, itemID, level, nextReview)
	} else {
		err = s.progress.UpsertTQ(ctx, userID, itemID, level, nextReview)
	}
	return level, nextReview, err
}

func progressApplyAnswer(level int, correct bool, nextReviewAt time.Time) int {
	if level == 7 {
		return 7
	}
	if correct {
		if time.Now().Before(nextReviewAt) {
			return level // answered before due date — no level change
		}
		return min(level+1, 6)
	}
	return max(1, level/2)
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
