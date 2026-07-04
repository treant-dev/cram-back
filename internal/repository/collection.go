package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

type CollectionRepository struct {
	pool *pgxpool.Pool
}

func NewCollectionRepository(pool *pgxpool.Pool) *CollectionRepository {
	return &CollectionRepository{pool: pool}
}

const collectionCols = `id, user_id, title, description, is_public, share_token, created_at, updated_at`

func scanCollection(scan func(...any) error) (model.Collection, error) {
	var c model.Collection
	err := scan(&c.ID, &c.UserID, &c.Title, &c.Description, &c.IsPublic, &c.ShareToken, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// Create makes a new collection. collType is accepted for API compatibility but no
// longer persisted — the unified item model makes collections type-agnostic.
func (r *CollectionRepository) Create(ctx context.Context, userID, title, description, collType string, isPublic bool) (*model.Collection, error) {
	c, err := scanCollection(r.pool.QueryRow(ctx,
		`INSERT INTO collections (user_id, title, description, is_public)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+collectionCols,
		userID, title, description, isPublic,
	).Scan)
	if err != nil {
		return nil, fmt.Errorf("create collection: %w", err)
	}
	return &c, nil
}

func (r *CollectionRepository) ListByUser(ctx context.Context, userID string) ([]model.Collection, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+collectionCols+` FROM collections WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	defer rows.Close()
	var collections []model.Collection
	for rows.Next() {
		c, err := scanCollection(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan collection: %w", err)
		}
		collections = append(collections, c)
	}
	return collections, nil
}

func (r *CollectionRepository) GetPublicByID(ctx context.Context, id string) (*model.Collection, error) {
	c, err := scanCollection(r.pool.QueryRow(ctx,
		`SELECT `+collectionCols+` FROM collections WHERE id = $1 AND is_public = true`,
		id,
	).Scan)
	if err != nil {
		return nil, fmt.Errorf("get public collection: %w", err)
	}
	return &c, nil
}

func (r *CollectionRepository) GetByID(ctx context.Context, id, userID string, isAdmin bool) (*model.Collection, error) {
	var c model.Collection
	var err error
	if isAdmin {
		c, err = scanCollection(r.pool.QueryRow(ctx,
			`SELECT `+collectionCols+` FROM collections WHERE id = $1`,
			id,
		).Scan)
	} else {
		c, err = scanCollection(r.pool.QueryRow(ctx,
			`SELECT `+collectionCols+` FROM collections
			 WHERE id = $1 AND (
			   user_id = $2
			   OR is_public = true
			   OR EXISTS (
			     SELECT 1 FROM collection_follows WHERE collection_id = $1 AND user_id = $2
			   )
			 )`,
			id, userID,
		).Scan)
	}
	if err != nil {
		return nil, fmt.Errorf("get collection: %w", err)
	}
	return &c, nil
}

func (r *CollectionRepository) Update(ctx context.Context, id, userID, title, description string, isPublic bool) (*model.Collection, error) {
	c, err := scanCollection(r.pool.QueryRow(ctx,
		`UPDATE collections SET title=$1, description=$2, is_public=$3, updated_at=NOW()
		 WHERE id=$4 AND user_id=$5
		 RETURNING `+collectionCols,
		title, description, isPublic, id, userID,
	).Scan)
	if err != nil {
		return nil, fmt.Errorf("update collection: %w", err)
	}
	return &c, nil
}

func (r *CollectionRepository) ExistsForUser(ctx context.Context, id, userID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM collections WHERE id=$1 AND user_id=$2)`,
		id, userID,
	).Scan(&exists)
	return exists, err
}

func (r *CollectionRepository) ListPublic(ctx context.Context) ([]model.Collection, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+collectionCols+` FROM collections WHERE is_public=true ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list public collections: %w", err)
	}
	defer rows.Close()
	var collections []model.Collection
	for rows.Next() {
		c, err := scanCollection(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan collection: %w", err)
		}
		collections = append(collections, c)
	}
	return collections, nil
}

func (r *CollectionRepository) ListFollowedByUser(ctx context.Context, userID string) ([]model.Collection, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT c.id, c.user_id, c.title, c.description, c.is_public, c.share_token, c.created_at, c.updated_at
		 FROM collections c
		 JOIN collection_follows f ON f.collection_id = c.id
		 WHERE f.user_id = $1
		 ORDER BY f.followed_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list followed by user: %w", err)
	}
	defer rows.Close()
	var collections []model.Collection
	for rows.Next() {
		c, err := scanCollection(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan collection: %w", err)
		}
		collections = append(collections, c)
	}
	return collections, nil
}

func (r *CollectionRepository) ListPublicForUsers(ctx context.Context, userIDs []string) (map[string][]model.Collection, error) {
	return r.listForUsers(ctx, userIDs, true)
}

func (r *CollectionRepository) ListAllForUsers(ctx context.Context, userIDs []string) (map[string][]model.Collection, error) {
	return r.listForUsers(ctx, userIDs, false)
}

func (r *CollectionRepository) listForUsers(ctx context.Context, userIDs []string, publicOnly bool) (map[string][]model.Collection, error) {
	if len(userIDs) == 0 {
		return map[string][]model.Collection{}, nil
	}
	query := `SELECT ` + collectionCols + ` FROM collections WHERE user_id = ANY($1)`
	if publicOnly {
		query += ` AND is_public = true`
	}
	query += ` ORDER BY user_id, created_at DESC`
	rows, err := r.pool.Query(ctx, query, userIDs)
	if err != nil {
		return nil, fmt.Errorf("list for users: %w", err)
	}
	defer rows.Close()
	result := make(map[string][]model.Collection)
	for rows.Next() {
		c, err := scanCollection(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan collection: %w", err)
		}
		result[c.UserID] = append(result[c.UserID], c)
	}
	return result, nil
}

func (r *CollectionRepository) Delete(ctx context.Context, id, userID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM collections WHERE id=$1 AND user_id=$2`,
		id, userID,
	)
	return err
}

// ForceDelete hard-deletes any collection by ID regardless of owner (admin use only).
func (r *CollectionRepository) ForceDelete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM collections WHERE id=$1`, id)
	return err
}

// ListUserImages returns all non-empty image URLs stored on items owned by userID.
func (r *CollectionRepository) ListUserImages(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT i.content->>'image' FROM items i
		 JOIN collections col ON i.collection_id = col.id
		 WHERE col.user_id = $1 AND COALESCE(i.content->>'image', '') <> ''`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	return urls, nil
}

// ListAllImages returns all non-empty image URLs stored on items belonging to
// collectionID. Used to clean up object storage before a cascade delete.
func (r *CollectionRepository) ListAllImages(ctx context.Context, collectionID string) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT content->>'image' FROM items
		 WHERE collection_id=$1 AND COALESCE(content->>'image', '') <> ''`,
		collectionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	return urls, nil
}

func (r *CollectionRepository) GenerateShareToken(ctx context.Context, id, userID string) (string, error) {
	var token string
	err := r.pool.QueryRow(ctx,
		`UPDATE collections SET share_token=gen_random_uuid()::text, updated_at=NOW()
		 WHERE id=$1 AND user_id=$2
		 RETURNING share_token`,
		id, userID,
	).Scan(&token)
	if err != nil {
		return "", fmt.Errorf("generate share token: %w", err)
	}
	return token, nil
}

func (r *CollectionRepository) RevokeShareToken(ctx context.Context, id, userID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE collections SET share_token=NULL, updated_at=NOW() WHERE id=$1 AND user_id=$2`,
		id, userID,
	)
	return err
}

func (r *CollectionRepository) GetByShareToken(ctx context.Context, token string) (*model.Collection, error) {
	c, err := scanCollection(r.pool.QueryRow(ctx,
		`SELECT `+collectionCols+` FROM collections WHERE share_token=$1`,
		token,
	).Scan)
	if err != nil {
		return nil, fmt.Errorf("get by share token: %w", err)
	}
	return &c, nil
}
