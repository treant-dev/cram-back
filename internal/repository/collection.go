package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

type CollectionRepository struct {
	pool *pgxpool.Pool
}

func NewCollectionRepository(pool *pgxpool.Pool) *CollectionRepository {
	return &CollectionRepository{pool: pool}
}

const collectionCols = `id, user_id, title, description, is_public, is_draft, draft_of, created_at, updated_at`

func scanCollection(scan func(...any) error) (model.Collection, error) {
	var c model.Collection
	err := scan(&c.ID, &c.UserID, &c.Title, &c.Description, &c.IsPublic, &c.IsDraft, &c.DraftOf, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func (r *CollectionRepository) Create(ctx context.Context, userID, title, description string, isPublic bool) (*model.Collection, error) {
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
		`SELECT `+collectionCols+` FROM collections WHERE user_id = $1 AND draft_of IS NULL ORDER BY created_at DESC`,
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

func (r *CollectionRepository) GetByID(ctx context.Context, id, userID string) (*model.Collection, error) {
	c, err := scanCollection(r.pool.QueryRow(ctx,
		`SELECT `+collectionCols+` FROM collections
		 WHERE id = $1 AND (
		   user_id = $2
		   OR (is_public = true AND is_draft = false)
		   OR (is_draft = false AND EXISTS (
		     SELECT 1 FROM collection_follows WHERE collection_id = $1 AND user_id = $2
		   ))
		 )`,
		id, userID,
	).Scan)
	if err != nil {
		return nil, fmt.Errorf("get collection: %w", err)
	}
	return &c, nil
}

func (r *CollectionRepository) Update(ctx context.Context, id, userID, title, description string, isPublic bool) (*model.Collection, error) {
	c, err := scanCollection(r.pool.QueryRow(ctx,
		`UPDATE collections SET title=$1, description=$2, is_public=$3, updated_at=NOW()
		 WHERE id=$4 AND user_id=$5 AND is_draft=false
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
		`SELECT EXISTS(SELECT 1 FROM collections WHERE id=$1 AND user_id=$2 AND draft_of IS NULL)`,
		id, userID,
	).Scan(&exists)
	return exists, err
}

func (r *CollectionRepository) ListPublic(ctx context.Context) ([]model.Collection, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+collectionCols+` FROM collections WHERE is_public=true AND is_draft=false ORDER BY created_at DESC`,
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
		`SELECT c.id, c.user_id, c.title, c.description, c.is_public, c.is_draft, c.draft_of, c.created_at, c.updated_at
		 FROM collections c
		 JOIN collection_follows f ON f.collection_id = c.id
		 WHERE f.user_id = $1 AND c.is_draft = false
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
	if len(userIDs) == 0 {
		return map[string][]model.Collection{}, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+collectionCols+` FROM collections
		 WHERE user_id = ANY($1) AND is_public = true AND is_draft = false
		 ORDER BY user_id, created_at DESC`,
		userIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("list public for users: %w", err)
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
		`DELETE FROM collections WHERE id=$1 AND user_id=$2 AND is_draft=false`,
		id, userID,
	)
	return err
}

// ForceDelete hard-deletes any collection by ID regardless of owner (admin use only).
func (r *CollectionRepository) ForceDelete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM collections WHERE id=$1`, id)
	return err
}

// GetDraftFor returns the draft collection for a given published collection and owner.
func (r *CollectionRepository) GetDraftFor(ctx context.Context, collectionID, userID string) (*model.Collection, error) {
	c, err := scanCollection(r.pool.QueryRow(ctx,
		`SELECT `+collectionCols+` FROM collections WHERE draft_of=$1 AND user_id=$2`,
		collectionID, userID,
	).Scan)
	if err != nil {
		return nil, fmt.Errorf("get draft: %w", err)
	}
	return &c, nil
}

// CreateDraftFrom creates a draft by copying the published collection and all its cards/tests.
func (r *CollectionRepository) CreateDraftFrom(ctx context.Context, collectionID, userID string) (*model.Collection, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	c, err := scanCollection(tx.QueryRow(ctx,
		`INSERT INTO collections (user_id, title, description, is_public, is_draft, draft_of)
		 SELECT user_id, title, description, is_public, true, id FROM collections WHERE id=$1 AND user_id=$2
		 RETURNING `+collectionCols,
		collectionID, userID,
	).Scan)
	if err != nil {
		return nil, fmt.Errorf("create draft collection: %w", err)
	}

	if _, err = tx.Exec(ctx,
		`INSERT INTO cards (collection_id, question, answer, position, source_card_id, created_at, updated_at)
		 SELECT $1, question, answer, position, id, created_at, updated_at FROM cards WHERE collection_id=$2`,
		c.ID, collectionID,
	); err != nil {
		return nil, fmt.Errorf("copy cards to draft: %w", err)
	}

	if _, err = tx.Exec(ctx,
		`INSERT INTO test_questions (collection_id, question, options, position, source_tq_id, created_at, updated_at)
		 SELECT $1, question, options, position, id, created_at, updated_at FROM test_questions WHERE collection_id=$2`,
		c.ID, collectionID,
	); err != nil {
		return nil, fmt.Errorf("copy test_questions to draft: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &c, nil
}

// UpdateDraftContent saves draft content using a smart diff: existing cards/tests are
// updated in-place (preserving their IDs), new ones are inserted, removed ones are deleted.
func (r *CollectionRepository) UpdateDraftContent(ctx context.Context, draftID, userID, title, description string, isPublic bool, cards []DraftCardInput, tests []DraftTestInput) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE collections SET title=$1, description=$2, is_public=$3, updated_at=NOW()
		 WHERE id=$4 AND user_id=$5 AND is_draft=true`,
		title, description, isPublic, draftID, userID,
	)
	if err != nil {
		return fmt.Errorf("update draft meta: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	// --- Cards ---
	existingCards, err := fetchIDs(ctx, tx, `SELECT id FROM cards WHERE collection_id=$1`, draftID)
	if err != nil {
		return fmt.Errorf("fetch draft card ids: %w", err)
	}
	seenCards := make(map[string]bool)
	for i, c := range cards {
		if c.ID != "" && existingCards[c.ID] {
			if _, err = tx.Exec(ctx,
				`UPDATE cards SET question=$1, answer=$2, position=$3, updated_at=NOW() WHERE id=$4`,
				c.Question, c.Answer, i, c.ID,
			); err != nil {
				return fmt.Errorf("update draft card: %w", err)
			}
			seenCards[c.ID] = true
		} else {
			if _, err = tx.Exec(ctx,
				`INSERT INTO cards (collection_id, question, answer, position) VALUES ($1, $2, $3, $4)`,
				draftID, c.Question, c.Answer, i,
			); err != nil {
				return fmt.Errorf("insert draft card: %w", err)
			}
		}
	}
	for id := range existingCards {
		if !seenCards[id] {
			if _, err = tx.Exec(ctx, `DELETE FROM cards WHERE id=$1`, id); err != nil {
				return fmt.Errorf("delete removed draft card: %w", err)
			}
		}
	}

	// --- Test questions ---
	existingTests, err := fetchIDs(ctx, tx, `SELECT id FROM test_questions WHERE collection_id=$1`, draftID)
	if err != nil {
		return fmt.Errorf("fetch draft test ids: %w", err)
	}
	seenTests := make(map[string]bool)
	for i, tq := range tests {
		optJSON, err := json.Marshal(tq.Options)
		if err != nil {
			return fmt.Errorf("marshal options: %w", err)
		}
		if tq.ID != "" && existingTests[tq.ID] {
			if _, err = tx.Exec(ctx,
				`UPDATE test_questions SET question=$1, options=$2, position=$3, updated_at=NOW() WHERE id=$4`,
				tq.Question, optJSON, i, tq.ID,
			); err != nil {
				return fmt.Errorf("update draft test: %w", err)
			}
			seenTests[tq.ID] = true
		} else {
			if _, err = tx.Exec(ctx,
				`INSERT INTO test_questions (collection_id, question, options, position) VALUES ($1, $2, $3, $4)`,
				draftID, tq.Question, optJSON, i,
			); err != nil {
				return fmt.Errorf("insert draft test: %w", err)
			}
		}
	}
	for id := range existingTests {
		if !seenTests[id] {
			if _, err = tx.Exec(ctx, `DELETE FROM test_questions WHERE id=$1`, id); err != nil {
				return fmt.Errorf("delete removed draft test: %w", err)
			}
		}
	}

	return tx.Commit(ctx)
}

// PublishDraft promotes a draft to the active version atomically.
// Cards with a source_card_id are updated in-place on the original (preserving IDs for stats).
// New cards (no source_card_id) are inserted into the original collection.
// Original cards removed from the draft are deleted.
func (r *CollectionRepository) PublishDraft(ctx context.Context, collectionID, userID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var draftID, title, description string
	var isPublic bool
	if err = tx.QueryRow(ctx,
		`SELECT id, title, description, is_public FROM collections WHERE draft_of=$1 AND user_id=$2 AND is_draft=true`,
		collectionID, userID,
	).Scan(&draftID, &title, &description, &isPublic); err != nil {
		return fmt.Errorf("get draft for publish: %w", err)
	}

	if _, err = tx.Exec(ctx,
		`UPDATE collections SET title=$1, description=$2, is_public=$3, updated_at=NOW() WHERE id=$4`,
		title, description, isPublic, collectionID,
	); err != nil {
		return fmt.Errorf("update original: %w", err)
	}

	// --- Cards ---
	type draftCard struct {
		ID       string
		SourceID *string
		Question string
		Answer   string
		Position int
	}
	var draftCards []draftCard
	cardRows, err := tx.Query(ctx,
		`SELECT id, source_card_id, question, answer, position FROM cards WHERE collection_id=$1`,
		draftID,
	)
	if err != nil {
		return fmt.Errorf("fetch draft cards: %w", err)
	}
	for cardRows.Next() {
		var dc draftCard
		if err := cardRows.Scan(&dc.ID, &dc.SourceID, &dc.Question, &dc.Answer, &dc.Position); err != nil {
			cardRows.Close()
			return fmt.Errorf("scan draft card: %w", err)
		}
		draftCards = append(draftCards, dc)
	}
	cardRows.Close()

	sourceCardIDs := make([]string, 0, len(draftCards))
	for _, dc := range draftCards {
		if dc.SourceID != nil {
			sourceCardIDs = append(sourceCardIDs, *dc.SourceID)
		}
	}
	if _, err = tx.Exec(ctx,
		`DELETE FROM cards WHERE collection_id=$1 AND NOT (id = ANY($2::uuid[]))`,
		collectionID, sourceCardIDs,
	); err != nil {
		return fmt.Errorf("delete removed cards: %w", err)
	}
	for _, dc := range draftCards {
		if dc.SourceID != nil {
			if _, err = tx.Exec(ctx,
				`UPDATE cards SET question=$1, answer=$2, position=$3, updated_at=NOW() WHERE id=$4`,
				dc.Question, dc.Answer, dc.Position, *dc.SourceID,
			); err != nil {
				return fmt.Errorf("update original card: %w", err)
			}
		} else {
			if _, err = tx.Exec(ctx,
				`INSERT INTO cards (collection_id, question, answer, position) VALUES ($1, $2, $3, $4)`,
				collectionID, dc.Question, dc.Answer, dc.Position,
			); err != nil {
				return fmt.Errorf("insert new card: %w", err)
			}
		}
	}

	// --- Test questions ---
	type draftTQ struct {
		ID       string
		SourceID *string
		Question string
		Options  []byte
		Position int
	}
	var draftTQs []draftTQ
	tqRows, err := tx.Query(ctx,
		`SELECT id, source_tq_id, question, options, position FROM test_questions WHERE collection_id=$1`,
		draftID,
	)
	if err != nil {
		return fmt.Errorf("fetch draft tests: %w", err)
	}
	for tqRows.Next() {
		var dtq draftTQ
		if err := tqRows.Scan(&dtq.ID, &dtq.SourceID, &dtq.Question, &dtq.Options, &dtq.Position); err != nil {
			tqRows.Close()
			return fmt.Errorf("scan draft test: %w", err)
		}
		draftTQs = append(draftTQs, dtq)
	}
	tqRows.Close()

	sourceTQIDs := make([]string, 0, len(draftTQs))
	for _, dtq := range draftTQs {
		if dtq.SourceID != nil {
			sourceTQIDs = append(sourceTQIDs, *dtq.SourceID)
		}
	}
	if _, err = tx.Exec(ctx,
		`DELETE FROM test_questions WHERE collection_id=$1 AND NOT (id = ANY($2::uuid[]))`,
		collectionID, sourceTQIDs,
	); err != nil {
		return fmt.Errorf("delete removed tests: %w", err)
	}
	for _, dtq := range draftTQs {
		if dtq.SourceID != nil {
			if _, err = tx.Exec(ctx,
				`UPDATE test_questions SET question=$1, options=$2, position=$3, updated_at=NOW() WHERE id=$4`,
				dtq.Question, dtq.Options, dtq.Position, *dtq.SourceID,
			); err != nil {
				return fmt.Errorf("update original test: %w", err)
			}
		} else {
			if _, err = tx.Exec(ctx,
				`INSERT INTO test_questions (collection_id, question, options, position) VALUES ($1, $2, $3, $4)`,
				collectionID, dtq.Question, dtq.Options, dtq.Position,
			); err != nil {
				return fmt.Errorf("insert new test: %w", err)
			}
		}
	}

	// Deleting the draft collection cascades to draft cards and test questions.
	if _, err = tx.Exec(ctx, `DELETE FROM collections WHERE id=$1`, draftID); err != nil {
		return fmt.Errorf("delete draft: %w", err)
	}

	return tx.Commit(ctx)
}

// DeleteDraft discards the draft for a collection.
func (r *CollectionRepository) DeleteDraft(ctx context.Context, collectionID, userID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM collections WHERE draft_of=$1 AND user_id=$2`,
		collectionID, userID,
	)
	return err
}

// DraftCardInput carries card content for draft updates.
// ID is the existing draft card UUID; empty means insert as new.
type DraftCardInput struct {
	ID       string
	Question string
	Answer   string
}

// DraftTestInput carries test question content for draft updates.
// ID is the existing draft test question UUID; empty means insert as new.
type DraftTestInput struct {
	ID       string
	Question string
	Options  []model.TestOption
}

// fetchIDs runs a query that selects a single UUID column and returns the results as a set.
func fetchIDs(ctx context.Context, tx pgx.Tx, query, arg string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, nil
}
