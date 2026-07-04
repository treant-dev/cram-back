package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

// ItemProgressRepository holds spaced-repetition state — ONLY for card items.
type ItemProgressRepository struct {
	pool *pgxpool.Pool
}

func NewItemProgressRepository(pool *pgxpool.Pool) *ItemProgressRepository {
	return &ItemProgressRepository{pool: pool}
}

// ProgressEntry / ProgressData are the DTO shapes returned to the handler/frontend.
// (Cards-only now; TQs kept for the JSON contract but always empty.)
type ProgressEntry struct {
	Level        int        `json:"level"`
	NextReviewAt time.Time  `json:"next_review_at"`
	LastReviewAt *time.Time `json:"last_review_at,omitempty"`
}

type ProgressData struct {
	Cards map[string]ProgressEntry `json:"cards"`
	TQs   map[string]ProgressEntry `json:"test_questions"`
}

// Get returns the progress row, or nil if the user hasn't studied this item yet.
func (r *ItemProgressRepository) Get(ctx context.Context, userID, itemID string) (*model.ItemProgress, error) {
	var p model.ItemProgress
	err := r.pool.QueryRow(ctx,
		`SELECT user_id, item_id, level, next_review_at, last_review_at
		 FROM item_progress WHERE user_id = $1 AND item_id = $2`,
		userID, itemID,
	).Scan(&p.UserID, &p.ItemID, &p.Level, &p.NextReviewAt, &p.LastReviewAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get progress: %w", err)
	}
	return &p, nil
}

// Upsert writes the new spaced-rep state after an answer.
func (r *ItemProgressRepository) Upsert(ctx context.Context, userID, itemID string, level int, nextReviewAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO item_progress (user_id, item_id, level, next_review_at, last_review_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id, item_id) DO UPDATE SET
		   level = EXCLUDED.level, next_review_at = EXCLUDED.next_review_at, last_review_at = NOW()`,
		userID, itemID, level, nextReviewAt,
	)
	if err != nil {
		return fmt.Errorf("upsert progress: %w", err)
	}
	return nil
}

// ListByCollection returns the user's progress for every card in a collection.
func (r *ItemProgressRepository) ListByCollection(ctx context.Context, userID, collectionID string) ([]model.ItemProgress, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT p.user_id, p.item_id, p.level, p.next_review_at, p.last_review_at
		 FROM item_progress p JOIN items i ON i.id = p.item_id
		 WHERE p.user_id = $1 AND i.collection_id = $2`,
		userID, collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list progress: %w", err)
	}
	defer rows.Close()
	var out []model.ItemProgress
	for rows.Next() {
		var p model.ItemProgress
		if err := rows.Scan(&p.UserID, &p.ItemID, &p.Level, &p.NextReviewAt, &p.LastReviewAt); err != nil {
			return nil, fmt.Errorf("scan progress: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ResetOne clears the user's progress for a single item.
func (r *ItemProgressRepository) ResetOne(ctx context.Context, userID, itemID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM item_progress WHERE user_id = $1 AND item_id = $2`, userID, itemID)
	return err
}

// ResetCollection clears the user's progress for all items of a collection.
func (r *ItemProgressRepository) ResetCollection(ctx context.Context, userID, collectionID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM item_progress
		 WHERE user_id = $1 AND item_id IN (SELECT id FROM items WHERE collection_id = $2)`,
		userID, collectionID,
	)
	return err
}
