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

// CountLive counts tokens the user can still authenticate with.
func (r *TokenRepository) CountLive(ctx context.Context, userID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM personal_access_tokens
		 WHERE user_id = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW())`,
		userID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count live tokens: %w", err)
	}
	return n, nil
}

// CountCreatedSince counts every token minted in the window, revoked ones included — the daily
// cap is about issuance, not about how many survive.
func (r *TokenRepository) CountCreatedSince(ctx context.Context, userID string, since time.Time) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM personal_access_tokens WHERE user_id = $1 AND created_at >= $2`,
		userID, since,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count tokens created since: %w", err)
	}
	return n, nil
}

// TrimRevoked keeps only the newest `keep` revoked tokens and deletes the rest. A revoked row is
// there to answer "did I revoke it, and when" — after a few of them it is noise that pushes live
// tokens down the page, and nothing else reads it: there is no per-token audit log.
//
// Rows younger than `protect` are never deleted, whatever the count: the daily issuance cap is
// computed from these rows, so trimming them away would let a create-then-revoke loop mint
// tokens without limit.
func (r *TokenRepository) TrimRevoked(ctx context.Context, userID string, keep int, protect time.Duration) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM personal_access_tokens
		 WHERE user_id = $1 AND revoked_at IS NOT NULL
		   AND created_at < NOW() - $3::interval
		   AND id NOT IN (
		     SELECT id FROM personal_access_tokens
		     WHERE user_id = $1 AND revoked_at IS NOT NULL
		     ORDER BY revoked_at DESC LIMIT $2
		   )`,
		userID, keep, protect.String(),
	)
	if err != nil {
		return fmt.Errorf("trim revoked tokens: %w", err)
	}
	return nil
}

// DeleteRevokedBefore drops revoked tokens older than the cutoff, for every user. The count cap
// in TrimRevoked bounds a busy account; this bounds a quiet one, where ten stale rows would sit
// in the list for years.
func (r *TokenRepository) DeleteRevokedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM personal_access_tokens WHERE revoked_at IS NOT NULL AND revoked_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete revoked tokens: %w", err)
	}
	return tag.RowsAffected(), nil
}

// TouchLastUsed is best-effort bookkeeping; callers ignore the error.
func (r *TokenRepository) TouchLastUsed(ctx context.Context, tokenID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE personal_access_tokens SET last_used_at = NOW() WHERE id = $1`,
		tokenID,
	)
	return err
}
