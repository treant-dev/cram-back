package service

import (
	"context"
	"errors"
	"fmt"

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
	Cards         []DraftCard
	TestQuestions []DraftQuestion
}

// DraftCard is a card payload for draft updates.
// ID is the existing draft card UUID; empty means insert as new.
type DraftCard struct {
	ID       string
	Question string
	Answer   string
	Image    string
}

// DraftQuestion is a test question payload for draft updates.
// ID is the existing draft test question UUID; empty means insert as new.
type DraftQuestion struct {
	ID       string
	Question string
	Options  []model.TestOption
	Image    string
}

type collectionRepo interface {
	Create(ctx context.Context, userID, title, description string, isPublic bool) (*model.Collection, error)
	ListByUser(ctx context.Context, userID string) ([]model.Collection, error)
	ListPublic(ctx context.Context) ([]model.Collection, error)
	ListPublicForUsers(ctx context.Context, userIDs []string) (map[string][]model.Collection, error)
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
	Create(ctx context.Context, collectionID, question, answer, image string, position int) (*model.Card, error)
	ListByCollection(ctx context.Context, collectionID string) ([]model.Card, error)
	Update(ctx context.Context, id, collectionID, question, answer, image string, position int) (*model.Card, error)
	Delete(ctx context.Context, id, collectionID string) (string, error)
	BulkCreate(ctx context.Context, collectionID string, cards []model.Card) error
}

type testQuestionRepo interface {
	Create(ctx context.Context, collectionID, question string, options []model.TestOption, image string, position int) (*model.TestQuestion, error)
	ListByCollection(ctx context.Context, collectionID string) ([]model.TestQuestion, error)
	Update(ctx context.Context, id, collectionID, question string, options []model.TestOption, image string, position int) (*model.TestQuestion, error)
	Delete(ctx context.Context, id, collectionID string) (string, error)
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
	ListCardStats(ctx context.Context, collectionID, userID string) (map[string]model.CardStats, error)
	ListTQStats(ctx context.Context, collectionID, userID string) (map[string]model.TQStats, error)
	GetHistory(ctx context.Context, collectionID, userID string, days int) (*repository.StudyHistoryData, error)
}

type CollectionService struct {
	collections   collectionRepo
	cards         cardRepo
	testQuestions testQuestionRepo
	follows       followRepo
	users         userRepo
	study         studyRepo
	images        ImageStore
}

func NewCollectionService(
	collections *repository.CollectionRepository,
	cards *repository.CardRepository,
	tq *repository.TestQuestionRepository,
	follows *repository.FollowRepository,
	users *repository.UserRepository,
	study *repository.StudyRepository,
) *CollectionService {
	return &CollectionService{
		collections:   collections,
		cards:         cards,
		testQuestions: tq,
		follows:       follows,
		users:         users,
		study:         study,
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

	// Attach per-user stats (errors are non-fatal; stats are best-effort).
	if cardStats, err := s.study.ListCardStats(ctx, id, userID); err == nil {
		for i := range col.Cards {
			if st, ok := cardStats[col.Cards[i].ID]; ok {
				col.Cards[i].Stats = &st
			}
		}
	}
	if tqStats, err := s.study.ListTQStats(ctx, id, userID); err == nil {
		for i := range col.TestQuestions {
			if st, ok := tqStats[col.TestQuestions[i].ID]; ok {
				col.TestQuestions[i].Stats = &st
			}
		}
	}

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

// Drafts

// GetOrCreateDraft returns the existing draft or creates a new one from the active collection.
func (s *CollectionService) GetOrCreateDraft(ctx context.Context, collectionID, userID string) (*model.Collection, error) {
	draft, err := s.collections.GetDraftFor(ctx, collectionID, userID)
	if err == nil {
		// Draft already exists — load its content.
		cards, err := s.cards.ListByCollection(ctx, draft.ID)
		if err != nil {
			return nil, err
		}
		tests, err := s.testQuestions.ListByCollection(ctx, draft.ID)
		if err != nil {
			return nil, err
		}
		draft.Cards = cards
		draft.TestQuestions = tests
		return draft, nil
	}

	// Verify ownership before creating.
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return nil, err
	}
	draft, err = s.collections.CreateDraftFrom(ctx, collectionID, userID)
	if err != nil {
		return nil, err
	}
	cards, err := s.cards.ListByCollection(ctx, draft.ID)
	if err != nil {
		return nil, err
	}
	tests, err := s.testQuestions.ListByCollection(ctx, draft.ID)
	if err != nil {
		return nil, err
	}
	draft.Cards = cards
	draft.TestQuestions = tests
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
	cards := make([]repository.DraftCardInput, len(req.Cards))
	for i, c := range req.Cards {
		cards[i] = repository.DraftCardInput{ID: c.ID, Question: c.Question, Answer: c.Answer, Image: c.Image}
	}
	tests := make([]repository.DraftTestInput, len(req.TestQuestions))
	for i, t := range req.TestQuestions {
		tests[i] = repository.DraftTestInput{ID: t.ID, Question: t.Question, Options: t.Options, Image: t.Image}
	}
	return s.collections.UpdateDraftContent(ctx, draft.ID, userID, req.Title, req.Description, req.IsPublic, cards, tests)
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
	colsByUser, err := s.collections.ListPublicForUsers(ctx, ids)
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

func (s *CollectionService) AddCard(ctx context.Context, collectionID, userID, question, answer, image string, position int) (*model.Card, error) {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return nil, err
	}
	return s.cards.Create(ctx, collectionID, question, answer, image, position)
}

func (s *CollectionService) UpdateCard(ctx context.Context, cardID, collectionID, userID, question, answer, image string, position int) (*model.Card, error) {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return nil, err
	}
	card, err := s.cards.Update(ctx, cardID, collectionID, question, answer, image, position)
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

// Test questions

func (s *CollectionService) AddTestQuestion(ctx context.Context, collectionID, userID, question string, options []model.TestOption, image string, position int) (*model.TestQuestion, error) {
	if err := s.ownsCollection(ctx, collectionID, userID); err != nil {
		return nil, err
	}
	return s.testQuestions.Create(ctx, collectionID, question, options, image, position)
}

func (s *CollectionService) UpdateTestQuestion(ctx context.Context, tqID, collectionID, userID, question string, options []model.TestOption, image string, position int) (*model.TestQuestion, error) {
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
	cards, err := s.cards.ListByCollection(ctx, col.ID)
	if err != nil {
		return nil, err
	}
	col.Cards = cards
	tests, err := s.testQuestions.ListByCollection(ctx, col.ID)
	if err != nil {
		return nil, err
	}
	col.TestQuestions = tests
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
