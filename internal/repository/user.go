package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) Upsert(ctx context.Context, googleID, email, name, picture string) (*model.User, error) {
	query := `
		INSERT INTO users (google_id, email, name, picture)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (google_id) DO UPDATE
			SET email      = EXCLUDED.email,
			    name       = EXCLUDED.name,
			    picture    = EXCLUDED.picture,
			    updated_at = NOW()
		RETURNING id, google_id, email, name, picture, created_at, updated_at`

	var u model.User
	err := r.pool.QueryRow(ctx, query, googleID, email, name, picture).
		Scan(&u.ID, &u.GoogleID, &u.Email, &u.Name, &u.Picture, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}

	return &u, nil
}
