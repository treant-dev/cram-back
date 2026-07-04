package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

type ItemRepository struct {
	pool *pgxpool.Pool
}

func NewItemRepository(pool *pgxpool.Pool) *ItemRepository {
	return &ItemRepository{pool: pool}
}

const itemCols = `id, type, collection_id, parent_id, content, rank, created_at, updated_at`

func scanItem(row interface {
	Scan(dest ...any) error
}) (model.Item, error) {
	var it model.Item
	var raw []byte
	if err := row.Scan(&it.ID, &it.Type, &it.CollectionID, &it.ParentID, &raw, &it.Rank, &it.CreatedAt, &it.UpdatedAt); err != nil {
		return it, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &it.Content); err != nil {
			return it, fmt.Errorf("unmarshal content: %w", err)
		}
	}
	return it, nil
}

func (r *ItemRepository) Create(ctx context.Context, it model.Item) (*model.Item, error) {
	content, err := json.Marshal(it.Content)
	if err != nil {
		return nil, fmt.Errorf("marshal content: %w", err)
	}
	out, err := scanItem(r.pool.QueryRow(ctx,
		`INSERT INTO items (type, collection_id, parent_id, content, rank)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+itemCols,
		it.Type, it.CollectionID, it.ParentID, content, it.Rank,
	))
	if err != nil {
		return nil, fmt.Errorf("create item: %w", err)
	}
	return &out, nil
}

// ListByCollection returns every item of a collection (all types, including nested
// sentences) ordered by rank. Callers group by ParentID / filter by Type.
func (r *ItemRepository) ListByCollection(ctx context.Context, collectionID string) ([]model.Item, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+itemCols+` FROM items WHERE collection_id = $1 ORDER BY rank ASC`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()

	var items []model.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// Get returns one item, or nil if not found in the collection.
func (r *ItemRepository) Get(ctx context.Context, id, collectionID string) (*model.Item, error) {
	out, err := scanItem(r.pool.QueryRow(ctx,
		`SELECT `+itemCols+` FROM items WHERE id = $1 AND collection_id = $2`, id, collectionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	return &out, nil
}

// Update replaces content only; rank is unchanged (reordering is a separate op).
// ListByParent returns child items (e.g. an exercise's sentences) ordered by rank.
func (r *ItemRepository) ListByParent(ctx context.Context, parentID string) ([]model.Item, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+itemCols+` FROM items WHERE parent_id = $1 ORDER BY rank ASC`, parentID)
	if err != nil {
		return nil, fmt.Errorf("list by parent: %w", err)
	}
	defer rows.Close()
	var items []model.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (r *ItemRepository) Update(ctx context.Context, id, collectionID string, content map[string]any) (*model.Item, error) {
	raw, err := json.Marshal(content)
	if err != nil {
		return nil, fmt.Errorf("marshal content: %w", err)
	}
	out, err := scanItem(r.pool.QueryRow(ctx,
		`UPDATE items SET content = $1, updated_at = NOW()
		 WHERE id = $2 AND collection_id = $3
		 RETURNING `+itemCols,
		raw, id, collectionID,
	))
	if err != nil {
		return nil, fmt.Errorf("update item: %w", err)
	}
	return &out, nil
}

func (r *ItemRepository) Delete(ctx context.Context, id, collectionID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM items WHERE id = $1 AND collection_id = $2`, id, collectionID)
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	return nil
}

// LastRank returns the highest rank among siblings (same collection + parent), or ""
// if none — used to append a new item at the end via rank.After(LastRank(...)).
func (r *ItemRepository) LastRank(ctx context.Context, collectionID string, parentID *string) (string, error) {
	var rank string
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(rank), '') FROM items
		 WHERE collection_id = $1 AND parent_id IS NOT DISTINCT FROM $2`,
		collectionID, parentID,
	).Scan(&rank)
	if err != nil {
		return "", fmt.Errorf("last rank: %w", err)
	}
	return rank, nil
}
