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

func (r *CardRepository) Create(ctx context.Context, setID, question, answer string, position int) (*model.Card, error) {
	var c model.Card
	err := r.pool.QueryRow(ctx,
		`INSERT INTO cards (set_id, question, answer, position)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, set_id, question, answer, position, created_at, updated_at`,
		setID, question, answer, position,
	).Scan(&c.ID, &c.SetID, &c.Question, &c.Answer, &c.Position, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create card: %w", err)
	}
	return &c, nil
}

func (r *CardRepository) ListBySet(ctx context.Context, setID string) ([]model.Card, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, set_id, question, answer, position, created_at, updated_at
		 FROM cards WHERE set_id = $1
		 ORDER BY position ASC, created_at ASC`,
		setID,
	)
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	defer rows.Close()

	var cards []model.Card
	for rows.Next() {
		var c model.Card
		if err := rows.Scan(&c.ID, &c.SetID, &c.Question, &c.Answer, &c.Position, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan card: %w", err)
		}
		cards = append(cards, c)
	}
	return cards, nil
}

func (r *CardRepository) Update(ctx context.Context, id, setID, question, answer string, position int) (*model.Card, error) {
	var c model.Card
	err := r.pool.QueryRow(ctx,
		`UPDATE cards SET question = $1, answer = $2, position = $3, updated_at = NOW()
		 WHERE id = $4 AND set_id = $5
		 RETURNING id, set_id, question, answer, position, created_at, updated_at`,
		question, answer, position, id, setID,
	).Scan(&c.ID, &c.SetID, &c.Question, &c.Answer, &c.Position, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update card: %w", err)
	}
	return &c, nil
}

func (r *CardRepository) Delete(ctx context.Context, id, setID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM cards WHERE id = $1 AND set_id = $2`,
		id, setID,
	)
	return err
}

func (r *CardRepository) BulkCreate(ctx context.Context, setID string, cards []model.Card) error {
	for i, c := range cards {
		if _, err := r.Create(ctx, setID, c.Question, c.Answer, i); err != nil {
			return err
		}
	}
	return nil
}
