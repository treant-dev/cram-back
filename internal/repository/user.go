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

func (r *UserRepository) ListAll(ctx context.Context) ([]model.User, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, google_id, email, name, picture, role, created_at, updated_at FROM users ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.GoogleID, &u.Email, &u.Name, &u.Picture, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, nil
}

func (r *UserRepository) Upsert(ctx context.Context, googleID, email, name, picture string) (*model.User, error) {
	// Role is intentionally excluded from SET — preserve existing role on re-login.
	query := `
		INSERT INTO users (google_id, email, name, picture)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (google_id) DO UPDATE
			SET email      = EXCLUDED.email,
			    name       = EXCLUDED.name,
			    picture    = EXCLUDED.picture,
			    updated_at = NOW()
		RETURNING id, google_id, email, name, picture, role, created_at, updated_at`

	var u model.User
	err := r.pool.QueryRow(ctx, query, googleID, email, name, picture).
		Scan(&u.ID, &u.GoogleID, &u.Email, &u.Name, &u.Picture, &u.Role, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}

	return &u, nil
}

func (r *UserRepository) Delete(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	return err
}

func (r *UserRepository) UpdateRole(ctx context.Context, userID, role string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET role = $1, updated_at = NOW() WHERE id = $2`,
		role, userID,
	)
	if err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}
