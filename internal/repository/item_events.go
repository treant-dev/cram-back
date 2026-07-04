package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

// ItemEventRepository is the append-only attempt log. Exercises use it as their only
// trace: the "current worksheet state" is the latest event per sentence.
type ItemEventRepository struct {
	pool *pgxpool.Pool
}

func NewItemEventRepository(pool *pgxpool.Pool) *ItemEventRepository {
	return &ItemEventRepository{pool: pool}
}

// SentenceResultEntry is the DTO for a saved worksheet answer (handler/frontend).
type SentenceResultEntry struct {
	Correct   bool     `json:"correct"`
	Submitted []string `json:"submitted"`
}

// Append records one attempt. correct == nil marks a retake reset.
func (r *ItemEventRepository) Append(ctx context.Context, userID, itemID string, correct *bool, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO item_events (user_id, item_id, correct, payload) VALUES ($1, $2, $3, $4)`,
		userID, itemID, correct, raw,
	)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

// LatestByCollection returns, per item, the most recent event for the user across a
// collection's items — used to restore worksheet state on reload.
func (r *ItemEventRepository) LatestByCollection(ctx context.Context, userID, collectionID string) (map[string]model.ItemEvent, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT ON (e.item_id) e.id, e.user_id, e.item_id, e.correct, e.payload, e.created_at
		 FROM item_events e JOIN items i ON i.id = e.item_id
		 WHERE e.user_id = $1 AND i.collection_id = $2
		 ORDER BY e.item_id, e.created_at DESC`,
		userID, collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("latest events: %w", err)
	}
	defer rows.Close()

	out := make(map[string]model.ItemEvent)
	for rows.Next() {
		var e model.ItemEvent
		var raw []byte
		if err := rows.Scan(&e.ID, &e.UserID, &e.ItemID, &e.Correct, &raw, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &e.Payload); err != nil {
				return nil, fmt.Errorf("unmarshal payload: %w", err)
			}
		}
		out[e.ItemID] = e
	}
	return out, rows.Err()
}
