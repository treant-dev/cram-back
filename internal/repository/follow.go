package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type FollowRepository struct {
	pool *pgxpool.Pool
}

func NewFollowRepository(pool *pgxpool.Pool) *FollowRepository {
	return &FollowRepository{pool: pool}
}

func (r *FollowRepository) Follow(ctx context.Context, userID, collectionID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO collection_follows (user_id, collection_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, collectionID,
	)
	if err != nil {
		return fmt.Errorf("follow: %w", err)
	}
	return nil
}

func (r *FollowRepository) Unfollow(ctx context.Context, userID, collectionID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM collection_follows WHERE user_id = $1 AND collection_id = $2`,
		userID, collectionID,
	)
	if err != nil {
		return fmt.Errorf("unfollow: %w", err)
	}
	return nil
}

// FollowedByUser returns the set of collection IDs followed by userID.
func (r *FollowRepository) FollowedByUser(ctx context.Context, userID string) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT collection_id FROM collection_follows WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("followed by user: %w", err)
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, nil
}

// CountsForCollections returns follower count per collection ID for the given IDs.
func (r *FollowRepository) CountsForCollections(ctx context.Context, collectionIDs []string) (map[string]int, error) {
	if len(collectionIDs) == 0 {
		return map[string]int{}, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT collection_id, COUNT(*) FROM collection_follows WHERE collection_id = ANY($1) GROUP BY collection_id`,
		collectionIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("counts for collections: %w", err)
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		result[id] = count
	}
	return result, nil
}
