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

type ProgressData struct {
	Cards map[string]int `json:"cards"`
	TQs   map[string]int `json:"test_questions"`
}

func (r *ProgressRepository) GetForCollection(ctx context.Context, collectionID, userID string) (*ProgressData, error) {
	data := &ProgressData{
		Cards: make(map[string]int),
		TQs:   make(map[string]int),
	}

	cardRows, err := r.pool.Query(ctx,
		`SELECT p.card_id::text, p.level
		 FROM card_progress p
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
		var level int
		if err := cardRows.Scan(&id, &level); err != nil {
			return nil, err
		}
		data.Cards[id] = level
	}

	tqRows, err := r.pool.Query(ctx,
		`SELECT p.tq_id::text, p.level
		 FROM tq_progress p
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
		var level int
		if err := tqRows.Scan(&id, &level); err != nil {
			return nil, err
		}
		data.TQs[id] = level
	}

	return data, nil
}

func (r *ProgressRepository) GetCardLevel(ctx context.Context, userID, cardID string) int {
	var level int
	if err := r.pool.QueryRow(ctx,
		`SELECT level FROM card_progress WHERE user_id = $1 AND card_id = $2`,
		userID, cardID,
	).Scan(&level); err != nil {
		return 1
	}
	return level
}

func (r *ProgressRepository) GetTQLevel(ctx context.Context, userID, tqID string) int {
	var level int
	if err := r.pool.QueryRow(ctx,
		`SELECT level FROM tq_progress WHERE user_id = $1 AND tq_id = $2`,
		userID, tqID,
	).Scan(&level); err != nil {
		return 1
	}
	return level
}

func (r *ProgressRepository) UpsertCard(ctx context.Context, userID, cardID string, level int, nextReview time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO card_progress (user_id, card_id, level, next_review_at, last_review_at)
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
		`INSERT INTO tq_progress (user_id, tq_id, level, next_review_at, last_review_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id, tq_id) DO UPDATE SET
		     level          = EXCLUDED.level,
		     next_review_at = EXCLUDED.next_review_at,
		     last_review_at = NOW()`,
		userID, tqID, level, nextReview,
	)
	return err
}
