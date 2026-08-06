package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

// ErrOAuthNotFound covers every "no such row" in this repository: unknown client, unknown or
// expired code, unknown token. Callers deliberately cannot tell them apart.
var ErrOAuthNotFound = errors.New("oauth record not found")

type OAuthRepository struct {
	pool *pgxpool.Pool
}

func NewOAuthRepository(pool *pgxpool.Pool) *OAuthRepository {
	return &OAuthRepository{pool: pool}
}

// ---------- clients ----------

func (r *OAuthRepository) CreateClient(ctx context.Context, id, name string, redirectURIs []string, scope string) (*model.OAuthClient, error) {
	uris, err := json.Marshal(redirectURIs)
	if err != nil {
		return nil, fmt.Errorf("encode redirect uris: %w", err)
	}
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO oauth_clients (id, name, redirect_uris, scope) VALUES ($1, $2, $3, $4)`,
		id, name, uris, scope,
	); err != nil {
		return nil, fmt.Errorf("create oauth client: %w", err)
	}
	return &model.OAuthClient{ID: id, Name: name, RedirectURIs: redirectURIs, Scope: scope, CreatedAt: time.Now()}, nil
}

func (r *OAuthRepository) GetClient(ctx context.Context, id string) (*model.OAuthClient, error) {
	var c model.OAuthClient
	var uris []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, redirect_uris, scope, created_at, last_used_at FROM oauth_clients WHERE id = $1`, id,
	).Scan(&c.ID, &c.Name, &uris, &c.Scope, &c.CreatedAt, &c.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOAuthNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get oauth client: %w", err)
	}
	if err := json.Unmarshal(uris, &c.RedirectURIs); err != nil {
		return nil, fmt.Errorf("decode redirect uris: %w", err)
	}
	return &c, nil
}

// ---------- codes ----------

func (r *OAuthRepository) CreateAuthCode(ctx context.Context, hash string, c model.OAuthAuthCode) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO oauth_auth_codes (code_hash, client_id, user_id, redirect_uri, code_challenge, scope, resource, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		hash, c.ClientID, c.UserID, c.RedirectURI, c.CodeChallenge, c.Scope, c.Resource, c.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("create auth code: %w", err)
	}
	return nil
}

// ConsumeAuthCode marks a code used and returns it. A code that was already used is still
// returned, with UsedAt set, so the caller can treat replay as the credential leak it is.
func (r *OAuthRepository) ConsumeAuthCode(ctx context.Context, hash string) (*model.OAuthAuthCode, error) {
	var c model.OAuthAuthCode
	err := r.pool.QueryRow(ctx,
		`UPDATE oauth_auth_codes SET used_at = COALESCE(used_at, NOW())
		 WHERE code_hash = $1
		 RETURNING client_id, user_id, redirect_uri, code_challenge, scope, resource, expires_at,
		           (SELECT used_at FROM oauth_auth_codes WHERE code_hash = $1)`,
		hash,
	).Scan(&c.ClientID, &c.UserID, &c.RedirectURI, &c.CodeChallenge, &c.Scope, &c.Resource, &c.ExpiresAt, &c.UsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOAuthNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("consume auth code: %w", err)
	}
	return &c, nil
}

// ---------- grants ----------

// UpsertGrant returns the live grant for (user, client), creating it or widening its scope.
// Re-authorizing must not leave the user with a list of near-identical grants to revoke.
func (r *OAuthRepository) UpsertGrant(ctx context.Context, userID, clientID, scope string) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO oauth_grants (user_id, client_id, scope) VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, client_id) WHERE revoked_at IS NULL
		 DO UPDATE SET scope = EXCLUDED.scope
		 RETURNING id`,
		userID, clientID, scope,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("upsert grant: %w", err)
	}
	return id, nil
}

func (r *OAuthRepository) ListGrants(ctx context.Context, userID string) ([]model.OAuthGrant, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT g.id, g.user_id, g.client_id, c.name, g.scope, g.created_at, g.last_used_at, g.revoked_at
		 FROM oauth_grants g JOIN oauth_clients c ON c.id = g.client_id
		 WHERE g.user_id = $1 AND g.revoked_at IS NULL
		 ORDER BY g.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	defer rows.Close()
	var out []model.OAuthGrant
	for rows.Next() {
		var g model.OAuthGrant
		if err := rows.Scan(&g.ID, &g.UserID, &g.ClientID, &g.ClientName, &g.Scope, &g.CreatedAt, &g.LastUsedAt, &g.RevokedAt); err != nil {
			return nil, fmt.Errorf("scan grant: %w", err)
		}
		out = append(out, g)
	}
	return out, nil
}

// RevokeGrant revokes the grant and every token hanging off it, in one transaction: a user
// clicking "disconnect" expects the client to stop working now, not when tokens expire.
func (r *OAuthRepository) RevokeGrant(ctx context.Context, grantID, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE oauth_grants SET revoked_at = COALESCE(revoked_at, NOW()) WHERE id = $1 AND user_id = $2`,
		grantID, userID)
	if err != nil {
		return fmt.Errorf("revoke grant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOAuthNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM oauth_access_tokens WHERE grant_id = $1`, grantID); err != nil {
		return fmt.Errorf("drop access tokens: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM oauth_refresh_tokens WHERE grant_id = $1`, grantID); err != nil {
		return fmt.Errorf("drop refresh tokens: %w", err)
	}
	return tx.Commit(ctx)
}

// RevokeGrantByID revokes a grant without knowing the user — used when a rotated refresh token
// is replayed, which means the value leaked.
func (r *OAuthRepository) RevokeGrantByID(ctx context.Context, grantID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE oauth_grants SET revoked_at = COALESCE(revoked_at, NOW()) WHERE id = $1`, grantID); err != nil {
		return fmt.Errorf("revoke grant: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM oauth_access_tokens WHERE grant_id = $1`, grantID); err != nil {
		return fmt.Errorf("drop access tokens: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM oauth_refresh_tokens WHERE grant_id = $1`, grantID); err != nil {
		return fmt.Errorf("drop refresh tokens: %w", err)
	}
	return tx.Commit(ctx)
}

// ---------- tokens ----------

func (r *OAuthRepository) CreateAccessToken(ctx context.Context, hash, grantID, scope, resource string, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO oauth_access_tokens (token_hash, grant_id, scope, resource, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		hash, grantID, scope, resource, expiresAt)
	if err != nil {
		return fmt.Errorf("create access token: %w", err)
	}
	return nil
}

func (r *OAuthRepository) CreateRefreshToken(ctx context.Context, hash, grantID string, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO oauth_refresh_tokens (token_hash, grant_id, expires_at) VALUES ($1, $2, $3)`,
		hash, grantID, expiresAt)
	if err != nil {
		return fmt.Errorf("create refresh token: %w", err)
	}
	return nil
}

// GetAccessToken resolves a live access token to its grant. Revoked grants and expired tokens
// are filtered in SQL, so a match is always usable.
func (r *OAuthRepository) GetAccessToken(ctx context.Context, hash string) (*model.OAuthToken, error) {
	var t model.OAuthToken
	err := r.pool.QueryRow(ctx,
		`SELECT t.grant_id, g.user_id, g.client_id, t.scope, t.resource, t.expires_at
		 FROM oauth_access_tokens t JOIN oauth_grants g ON g.id = t.grant_id
		 WHERE t.token_hash = $1 AND t.expires_at > NOW() AND g.revoked_at IS NULL`,
		hash,
	).Scan(&t.GrantID, &t.UserID, &t.ClientID, &t.Scope, &t.Resource, &t.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOAuthNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}
	return &t, nil
}

// ConsumeRefreshToken marks a refresh token used and returns it, including one that was already
// used — replay of a rotated token is a leak, and the caller reacts by killing the grant.
func (r *OAuthRepository) ConsumeRefreshToken(ctx context.Context, hash string) (*model.OAuthToken, error) {
	var t model.OAuthToken
	err := r.pool.QueryRow(ctx,
		`UPDATE oauth_refresh_tokens SET used_at = COALESCE(used_at, NOW())
		 WHERE token_hash = $1
		 RETURNING grant_id, expires_at,
		           (SELECT used_at FROM oauth_refresh_tokens WHERE token_hash = $1),
		           (SELECT user_id FROM oauth_grants WHERE id = grant_id),
		           (SELECT client_id FROM oauth_grants WHERE id = grant_id),
		           (SELECT scope FROM oauth_grants WHERE id = grant_id)`,
		hash,
	).Scan(&t.GrantID, &t.ExpiresAt, &t.UsedAt, &t.UserID, &t.ClientID, &t.Scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOAuthNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("consume refresh token: %w", err)
	}
	return &t, nil
}

// TouchGrant records use, for the "last used" column in settings. Best effort.
func (r *OAuthRepository) TouchGrant(ctx context.Context, grantID string) error {
	_, err := r.pool.Exec(ctx, `UPDATE oauth_grants SET last_used_at = NOW() WHERE id = $1`, grantID)
	return err
}

// DeleteExpired clears rows that can no longer be redeemed. Called periodically; codes are
// short-lived and tokens expire, so without this the tables only grow.
func (r *OAuthRepository) DeleteExpired(ctx context.Context) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM oauth_auth_codes WHERE expires_at < NOW() - INTERVAL '1 day'`); err != nil {
		return err
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM oauth_access_tokens WHERE expires_at < NOW() - INTERVAL '1 day'`); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM oauth_refresh_tokens WHERE expires_at < NOW() - INTERVAL '1 day'`)
	return err
}
