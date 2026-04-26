package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

type CardRepository struct {
	pool *pgxpool.Pool
}

func NewCardRepository(pool *pgxpool.Pool) *CardRepository {
	return &CardRepository{pool: pool}
}

func (r *CardRepository) Create(ctx context.Context, collectionID, question, answer, image string, position int) (*model.Card, error) {
	var c model.Card
	err := r.pool.QueryRow(ctx,
		`INSERT INTO cards (collection_id, question, answer, image, position)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, collection_id, question, answer, COALESCE(image, ''), position, created_at, updated_at`,
		collectionID, question, answer, image, position,
	).Scan(&c.ID, &c.CollectionID, &c.Question, &c.Answer, &c.Image, &c.Position, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create card: %w", err)
	}
	return &c, nil
}

func (r *CardRepository) ListByCollection(ctx context.Context, collectionID string) ([]model.Card, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, collection_id, question, answer, COALESCE(image, ''), position, created_at, updated_at
		 FROM cards WHERE collection_id = $1
		 ORDER BY position ASC, created_at ASC`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	defer rows.Close()

	var cards []model.Card
	for rows.Next() {
		var c model.Card
		if err := rows.Scan(&c.ID, &c.CollectionID, &c.Question, &c.Answer, &c.Image, &c.Position, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan card: %w", err)
		}
		cards = append(cards, c)
	}
	return cards, nil
}

func (r *CardRepository) Update(ctx context.Context, id, collectionID, question, answer, image string, position int) (*model.Card, error) {
	var c model.Card
	err := r.pool.QueryRow(ctx,
		`UPDATE cards SET question = $1, answer = $2, image = $3, position = $4, updated_at = NOW()
		 WHERE id = $5 AND collection_id = $6
		 RETURNING id, collection_id, question, answer, COALESCE(image, ''), position, created_at, updated_at`,
		question, answer, image, position, id, collectionID,
	).Scan(&c.ID, &c.CollectionID, &c.Question, &c.Answer, &c.Image, &c.Position, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}
	return &c, nil
}

func (r *CardRepository) Delete(ctx context.Context, id, collectionID string) (string, error) {
	var image string
	err := r.pool.QueryRow(ctx,
		`DELETE FROM cards WHERE id = $1 AND collection_id = $2 RETURNING COALESCE(image, '')`,
		id, collectionID,
	).Scan(&image)
	if err != nil {
		return "", err
	}
	return image, nil
}

func (r *CardRepository) BulkCreate(ctx context.Context, collectionID string, cards []model.Card) error {
	if len(cards) == 0 {
		return nil
	}
	questions := make([]string, len(cards))
	answers := make([]string, len(cards))
	positions := make([]int32, len(cards))
	for i, c := range cards {
		questions[i] = c.Question
		answers[i] = c.Answer
		positions[i] = int32(i)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO cards (collection_id, question, answer, position)
		 SELECT $1, q, a, p
		 FROM unnest($2::text[], $3::text[], $4::int[]) AS t(q, a, p)`,
		collectionID, questions, answers, positions,
	)
	if err != nil {
		return fmt.Errorf("bulk create cards: %w", err)
	}
	return nil
}
