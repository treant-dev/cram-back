package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

type SetRepository struct {
	pool *pgxpool.Pool
}

func NewSetRepository(pool *pgxpool.Pool) *SetRepository {
	return &SetRepository{pool: pool}
}

func (r *SetRepository) Create(ctx context.Context, userID, title, description string) (*model.StudySet, error) {
	var s model.StudySet
	err := r.pool.QueryRow(ctx,
		`INSERT INTO study_sets (user_id, title, description)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, title, description, created_at, updated_at`,
		userID, title, description,
	).Scan(&s.ID, &s.UserID, &s.Title, &s.Description, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create set: %w", err)
	}
	return &s, nil
}

func (r *SetRepository) ListByUser(ctx context.Context, userID string) ([]model.StudySet, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, title, description, created_at, updated_at
		 FROM study_sets WHERE user_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list sets: %w", err)
	}
	defer rows.Close()

	var sets []model.StudySet
	for rows.Next() {
		var s model.StudySet
		if err := rows.Scan(&s.ID, &s.UserID, &s.Title, &s.Description, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan set: %w", err)
		}
		sets = append(sets, s)
	}
	return sets, nil
}

func (r *SetRepository) GetByID(ctx context.Context, id, userID string) (*model.StudySet, error) {
	var s model.StudySet
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, title, description, created_at, updated_at
		 FROM study_sets WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&s.ID, &s.UserID, &s.Title, &s.Description, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get set: %w", err)
	}
	return &s, nil
}

func (r *SetRepository) Update(ctx context.Context, id, userID, title, description string) (*model.StudySet, error) {
	var s model.StudySet
	err := r.pool.QueryRow(ctx,
		`UPDATE study_sets SET title = $1, description = $2, updated_at = NOW()
		 WHERE id = $3 AND user_id = $4
		 RETURNING id, user_id, title, description, created_at, updated_at`,
		title, description, id, userID,
	).Scan(&s.ID, &s.UserID, &s.Title, &s.Description, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update set: %w", err)
	}
	return &s, nil
}

func (r *SetRepository) Delete(ctx context.Context, id, userID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM study_sets WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("delete set: %w", err)
	}
	return nil
}
