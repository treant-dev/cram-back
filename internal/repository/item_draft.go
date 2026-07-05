package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

// ItemDraftRepository manages the staging table: pending edits invisible to readers.
// The "draft" of a collection is simply all its rows here. Publish applies them to
// `items` (firing the item_history trigger) and clears the draft.
type ItemDraftRepository struct {
	pool *pgxpool.Pool
}

func NewItemDraftRepository(pool *pgxpool.Pool) *ItemDraftRepository {
	return &ItemDraftRepository{pool: pool}
}

// Set upserts a draft row (one per item_id; last-write-wins).
func (r *ItemDraftRepository) Set(ctx context.Context, d model.ItemDraft) error {
	content, err := json.Marshal(d.Content)
	if err != nil {
		return fmt.Errorf("marshal content: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO item_draft (item_id, collection_id, op, type, parent_id, content, rank)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (item_id) DO UPDATE SET
		   op = EXCLUDED.op, type = EXCLUDED.type, parent_id = EXCLUDED.parent_id,
		   content = EXCLUDED.content, rank = EXCLUDED.rank, updated_at = NOW()`,
		d.ItemID, d.CollectionID, d.Op, d.Type, d.ParentID, content, d.Rank,
	)
	if err != nil {
		return fmt.Errorf("set draft: %w", err)
	}
	return nil
}

func (r *ItemDraftRepository) ListByCollection(ctx context.Context, collectionID string) ([]model.ItemDraft, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT item_id, collection_id, op, type, parent_id, content, rank, updated_at
		 FROM item_draft WHERE collection_id = $1`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list draft: %w", err)
	}
	defer rows.Close()

	var out []model.ItemDraft
	for rows.Next() {
		var d model.ItemDraft
		var raw []byte
		if err := rows.Scan(&d.ItemID, &d.CollectionID, &d.Op, &d.Type, &d.ParentID, &raw, &d.Rank, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan draft: %w", err)
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &d.Content); err != nil {
				return nil, fmt.Errorf("unmarshal draft content: %w", err)
			}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Get returns a single staged row, or nil if the item has no pending change.
func (r *ItemDraftRepository) Get(ctx context.Context, itemID string) (*model.ItemDraft, error) {
	var d model.ItemDraft
	var raw []byte
	err := r.pool.QueryRow(ctx,
		`SELECT item_id, collection_id, op, type, parent_id, content, rank, updated_at
		 FROM item_draft WHERE item_id = $1`, itemID,
	).Scan(&d.ItemID, &d.CollectionID, &d.Op, &d.Type, &d.ParentID, &raw, &d.Rank, &d.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get draft: %w", err)
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &d.Content); err != nil {
			return nil, fmt.Errorf("unmarshal draft content: %w", err)
		}
	}
	return &d, nil
}

// Remove drops a single draft row (per-element revert), scoped to the collection.
func (r *ItemDraftRepository) Remove(ctx context.Context, collectionID, itemID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM item_draft WHERE item_id = $1 AND collection_id = $2`, itemID, collectionID)
	return err
}

// Clear drops the whole draft of a collection (discard).
func (r *ItemDraftRepository) Clear(ctx context.Context, collectionID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM item_draft WHERE collection_id = $1`, collectionID)
	return err
}

// Publish applies the draft to `items` in one transaction (upsert / delete), then
// clears it. Item writes fire the item_history trigger, so published changes are
// logged automatically.
func (r *ItemDraftRepository) Publish(ctx context.Context, collectionID string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	drafts, err := r.listTx(ctx, tx, collectionID)
	if err != nil {
		return err
	}
	// Apply parent upserts before child upserts (a sentence's parent_id → items.id
	// must exist first), then deletes. Without this, publishing a new exercise plus
	// its sentences can violate the parent_id FK.
	sort.SliceStable(drafts, func(i, j int) bool { return publishOrder(drafts[i]) < publishOrder(drafts[j]) })
	for _, d := range drafts {
		switch d.Op {
		case "delete":
			if _, err := tx.Exec(ctx, `DELETE FROM items WHERE id = $1 AND collection_id = $2`, d.ItemID, collectionID); err != nil {
				return fmt.Errorf("publish delete %s: %w", d.ItemID, err)
			}
		case "upsert":
			content, err := json.Marshal(d.Content)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO items (id, type, collection_id, parent_id, content, rank)
				 VALUES ($1, $2, $3, $4, $5, $6)
				 ON CONFLICT (id) DO UPDATE SET
				   type = EXCLUDED.type, parent_id = EXCLUDED.parent_id,
				   content = EXCLUDED.content, rank = EXCLUDED.rank, updated_at = NOW()`,
				d.ItemID, d.Type, collectionID, d.ParentID, content, d.Rank,
			); err != nil {
				return fmt.Errorf("publish upsert %s: %w", d.ItemID, err)
			}
		default:
			return fmt.Errorf("unknown draft op %q", d.Op)
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM item_draft WHERE collection_id = $1`, collectionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// publishOrder ranks draft rows for safe application: parentless upserts (0) before
// child upserts (1) before deletes (2).
func publishOrder(d model.ItemDraft) int {
	if d.Op == "delete" {
		return 2
	}
	if d.ParentID != nil {
		return 1
	}
	return 0
}

func (r *ItemDraftRepository) listTx(ctx context.Context, tx pgx.Tx, collectionID string) ([]model.ItemDraft, error) {
	rows, err := tx.Query(ctx,
		`SELECT item_id, collection_id, op, type, parent_id, content, rank, updated_at
		 FROM item_draft WHERE collection_id = $1`, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ItemDraft
	for rows.Next() {
		var d model.ItemDraft
		var raw []byte
		if err := rows.Scan(&d.ItemID, &d.CollectionID, &d.Op, &d.Type, &d.ParentID, &raw, &d.Rank, &d.UpdatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &d.Content); err != nil {
				return nil, err
			}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
