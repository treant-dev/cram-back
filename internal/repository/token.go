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

// ErrTokenNotFound is returned when no live token matches a hash.
var ErrTokenNotFound = errors.New("token not found")

const tokenCols = `id, user_id, name, scope, created_at, last_used_at, expires_at, revoked_at`

type TokenRepository struct {
	pool *pgxpool.Pool
}

func NewTokenRepository(pool *pgxpool.Pool) *TokenRepository {
	return &TokenRepository{pool: pool}
}

func scanToken(scan func(...any) error) (model.PersonalAccessToken, error) {
	var t model.PersonalAccessToken
	err := scan(&t.ID, &t.UserID, &t.Name, &t.Scope, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt, &t.RevokedAt)
	return t, err
}

func (r *TokenRepository) Create(ctx context.Context, userID, name, tokenHash, scope string, expiresAt *time.Time) (*model.PersonalAccessToken, error) {
	t, err := scanToken(r.pool.QueryRow(ctx,
		`INSERT INTO personal_access_tokens (user_id, name, token_hash, scope, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+tokenCols,
		userID, name, tokenHash, scope, expiresAt,
	).Scan)
	if err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}
	return &t, nil
}

// ListByUser returns the user's tokens, newest first, including revoked ones so the UI can
// show history. Never returns the hash.
func (r *TokenRepository) ListByUser(ctx context.Context, userID string) ([]model.PersonalAccessToken, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+tokenCols+` FROM personal_access_tokens WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()
	var tokens []model.PersonalAccessToken
	for rows.Next() {
		t, err := scanToken(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

// GetByHash returns the token and its owner's claim fields. Revoked and expired tokens are
// filtered out in SQL, so a match is always usable.
func (r *TokenRepository) GetByHash(ctx context.Context, tokenHash string) (*model.TokenOwner, error) {
	var owner model.TokenOwner
	err := r.pool.QueryRow(ctx,
		`SELECT t.id, t.user_id, t.name, t.scope, t.created_at, t.last_used_at, t.expires_at, t.revoked_at,
		        u.email, u.role
		 FROM personal_access_tokens t
		 JOIN users u ON u.id = t.user_id
		 WHERE t.token_hash = $1
		   AND t.revoked_at IS NULL
		   AND (t.expires_at IS NULL OR t.expires_at > NOW())`,
		tokenHash,
	).Scan(
		&owner.Token.ID, &owner.Token.UserID, &owner.Token.Name, &owner.Token.Scope,
		&owner.Token.CreatedAt, &owner.Token.LastUsedAt, &owner.Token.ExpiresAt, &owner.Token.RevokedAt,
		&owner.Email, &owner.Role,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get token by hash: %w", err)
	}
	return &owner, nil
}

// Revoke marks a token revoked. Idempotent: revoking an already-revoked token keeps the
// original timestamp.
func (r *TokenRepository) Revoke(ctx context.Context, tokenID, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE personal_access_tokens SET revoked_at = COALESCE(revoked_at, NOW())
		 WHERE id = $1 AND user_id = $2`,
		tokenID, userID,
	)
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrTokenNotFound
	}
	return nil
}

// TouchLastUsed is best-effort bookkeeping; callers ignore the error.
func (r *TokenRepository) TouchLastUsed(ctx context.Context, tokenID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE personal_access_tokens SET last_used_at = NOW() WHERE id = $1`,
		tokenID,
	)
	return err
}
