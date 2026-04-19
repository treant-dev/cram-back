package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/treant-dev/cram-go/internal/model"
	"github.com/treant-dev/cram-go/internal/repository"
)

var ErrNotFound = errors.New("not found")

type setRepo interface {
	Create(ctx context.Context, userID, title, description string) (*model.StudySet, error)
	ListByUser(ctx context.Context, userID string) ([]model.StudySet, error)
	GetByID(ctx context.Context, id, userID string) (*model.StudySet, error)
	Update(ctx context.Context, id, userID, title, description string) (*model.StudySet, error)
	Delete(ctx context.Context, id, userID string) error
}

type cardRepo interface {
	Create(ctx context.Context, setID, question, answer string, position int) (*model.Card, error)
	ListBySet(ctx context.Context, setID string) ([]model.Card, error)
	Update(ctx context.Context, id, setID, question, answer string, position int) (*model.Card, error)
	Delete(ctx context.Context, id, setID string) error
	BulkCreate(ctx context.Context, setID string, cards []model.Card) error
}

type testQuestionRepo interface {
	Create(ctx context.Context, setID, question string, options []model.TestOption, position int) (*model.TestQuestion, error)
	ListBySet(ctx context.Context, setID string) ([]model.TestQuestion, error)
	Update(ctx context.Context, id, setID, question string, options []model.TestOption, position int) (*model.TestQuestion, error)
	Delete(ctx context.Context, id, setID string) error
}

type CardService struct {
	sets          setRepo
	cards         cardRepo
	testQuestions testQuestionRepo
}

func NewCardService(sets *repository.SetRepository, cards *repository.CardRepository, tq *repository.TestQuestionRepository) *CardService {
	return &CardService{sets: sets, cards: cards, testQuestions: tq}
}

func (s *CardService) CreateSet(ctx context.Context, userID, title, description string) (*model.StudySet, error) {
	return s.sets.Create(ctx, userID, title, description)
}

func (s *CardService) ListSets(ctx context.Context, userID string) ([]model.StudySet, error) {
	return s.sets.ListByUser(ctx, userID)
}

func (s *CardService) GetSet(ctx context.Context, id, userID string) (*model.StudySet, error) {
	set, err := s.sets.GetByID(ctx, id, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	cards, err := s.cards.ListBySet(ctx, id)
	if err != nil {
		return nil, err
	}
	tqs, err := s.testQuestions.ListBySet(ctx, id)
	if err != nil {
		return nil, err
	}
	set.Cards = cards
	set.TestQuestions = tqs
	return set, nil
}

func (s *CardService) UpdateSet(ctx context.Context, id, userID, title, description string) (*model.StudySet, error) {
	set, err := s.sets.Update(ctx, id, userID, title, description)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return set, err
}

func (s *CardService) DeleteSet(ctx context.Context, id, userID string) error {
	return s.sets.Delete(ctx, id, userID)
}

func (s *CardService) ownsSet(ctx context.Context, setID, userID string) error {
	_, err := s.sets.GetByID(ctx, setID, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Cards

func (s *CardService) AddCard(ctx context.Context, setID, userID, question, answer string, position int) (*model.Card, error) {
	if err := s.ownsSet(ctx, setID, userID); err != nil {
		return nil, err
	}
	return s.cards.Create(ctx, setID, question, answer, position)
}

func (s *CardService) UpdateCard(ctx context.Context, cardID, setID, userID, question, answer string, position int) (*model.Card, error) {
	if err := s.ownsSet(ctx, setID, userID); err != nil {
		return nil, err
	}
	card, err := s.cards.Update(ctx, cardID, setID, question, answer, position)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return card, err
}

func (s *CardService) DeleteCard(ctx context.Context, cardID, setID, userID string) error {
	if err := s.ownsSet(ctx, setID, userID); err != nil {
		return err
	}
	return s.cards.Delete(ctx, cardID, setID)
}

func (s *CardService) ImportCards(ctx context.Context, setID, userID string, cards []model.Card) error {
	if err := s.ownsSet(ctx, setID, userID); err != nil {
		return err
	}
	return s.cards.BulkCreate(ctx, setID, cards)
}

// Test questions

func (s *CardService) AddTestQuestion(ctx context.Context, setID, userID, question string, options []model.TestOption, position int) (*model.TestQuestion, error) {
	if err := s.ownsSet(ctx, setID, userID); err != nil {
		return nil, err
	}
	return s.testQuestions.Create(ctx, setID, question, options, position)
}

func (s *CardService) UpdateTestQuestion(ctx context.Context, tqID, setID, userID, question string, options []model.TestOption, position int) (*model.TestQuestion, error) {
	if err := s.ownsSet(ctx, setID, userID); err != nil {
		return nil, err
	}
	tq, err := s.testQuestions.Update(ctx, tqID, setID, question, options, position)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return tq, err
}

func (s *CardService) DeleteTestQuestion(ctx context.Context, tqID, setID, userID string) error {
	if err := s.ownsSet(ctx, setID, userID); err != nil {
		return err
	}
	return s.testQuestions.Delete(ctx, tqID, setID)
}
