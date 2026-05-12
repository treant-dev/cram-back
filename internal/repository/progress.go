package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ProgressRepository struct {
	pool *pgxpool.Pool
}

func NewProgressRepository(pool *pgxpool.Pool) *ProgressRepository {
	return &ProgressRepository{pool: pool}
}

type ProgressEntry struct {
	Level        int       `json:"level"`
	NextReviewAt time.Time `json:"next_review_at"`
}

type ProgressData struct {
	Cards map[string]ProgressEntry `json:"cards"`
	TQs   map[string]ProgressEntry `json:"test_questions"`
}

func (r *ProgressRepository) GetForCollection(ctx context.Context, collectionID, userID string) (*ProgressData, error) {
	data := &ProgressData{
		Cards: make(map[string]ProgressEntry),
		TQs:   make(map[string]ProgressEntry),
	}

	cardRows, err := r.pool.Query(ctx,
		`SELECT p.card_id::text, p.level, p.next_review_at
		 FROM user_card_progress p
		 JOIN cards c ON c.id = p.card_id
		 WHERE c.collection_id = $1 AND p.user_id = $2`,
		collectionID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get card progress: %w", err)
	}
	defer cardRows.Close()
	for cardRows.Next() {
		var id string
		var e ProgressEntry
		if err := cardRows.Scan(&id, &e.Level, &e.NextReviewAt); err != nil {
			return nil, err
		}
		data.Cards[id] = e
	}

	tqRows, err := r.pool.Query(ctx,
		`SELECT p.tq_id::text, p.level, p.next_review_at
		 FROM user_test_progress p
		 JOIN test_questions tq ON tq.id = p.tq_id
		 WHERE tq.collection_id = $1 AND p.user_id = $2`,
		collectionID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get tq progress: %w", err)
	}
	defer tqRows.Close()
	for tqRows.Next() {
		var id string
		var e ProgressEntry
		if err := tqRows.Scan(&id, &e.Level, &e.NextReviewAt); err != nil {
			return nil, err
		}
		data.TQs[id] = e
	}

	return data, nil
}

func (r *ProgressRepository) GetCardLevel(ctx context.Context, userID, cardID string) int {
	var level int
	if err := r.pool.QueryRow(ctx,
		`SELECT level FROM user_card_progress WHERE user_id = $1 AND card_id = $2`,
		userID, cardID,
	).Scan(&level); err != nil {
		return 1
	}
	return level
}

func (r *ProgressRepository) GetTQLevel(ctx context.Context, userID, tqID string) int {
	var level int
	if err := r.pool.QueryRow(ctx,
		`SELECT level FROM user_test_progress WHERE user_id = $1 AND tq_id = $2`,
		userID, tqID,
	).Scan(&level); err != nil {
		return 1
	}
	return level
}

func (r *ProgressRepository) UpsertCard(ctx context.Context, userID, cardID string, level int, nextReview time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_card_progress (user_id, card_id, level, next_review_at, last_review_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id, card_id) DO UPDATE SET
		     level          = EXCLUDED.level,
		     next_review_at = EXCLUDED.next_review_at,
		     last_review_at = NOW()`,
		userID, cardID, level, nextReview,
	)
	return err
}

func (r *ProgressRepository) UpsertTQ(ctx context.Context, userID, tqID string, level int, nextReview time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_test_progress (user_id, tq_id, level, next_review_at, last_review_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id, tq_id) DO UPDATE SET
		     level          = EXCLUDED.level,
		     next_review_at = EXCLUDED.next_review_at,
		     last_review_at = NOW()`,
		userID, tqID, level, nextReview,
	)
	return err
}
