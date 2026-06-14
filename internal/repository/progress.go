package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ProgressRepository struct {
	pool *pgxpool.Pool
}

func NewProgressRepository(pool *pgxpool.Pool) *ProgressRepository {
	return &ProgressRepository{pool: pool}
}

type ProgressEntry struct {
	Level        int        `json:"level"`
	NextReviewAt time.Time  `json:"next_review_at"`
	LastReviewAt *time.Time `json:"last_review_at,omitempty"`
}

type ProgressData struct {
	Cards map[string]ProgressEntry `json:"cards"`
	TQs   map[string]ProgressEntry `json:"test_questions"`
}

func (r *ProgressRepository) GetForCollection(ctx context.Context, collectionID, userID string) (*ProgressData, error) {
	data := &ProgressData{
		Cards: make(map[string]ProgressEntry),
		TQs:   make(map[string]ProgressEntry),
	}

	cardRows, err := r.pool.Query(ctx,
		`SELECT p.card_id::text, p.level, p.next_review_at, p.last_review_at
		 FROM user_card_progress p
		 JOIN cards c ON c.id = p.card_id
		 WHERE c.collection_id = $1 AND p.user_id = $2`,
		collectionID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get card progress: %w", err)
	}
	defer cardRows.Close()
	for cardRows.Next() {
		var id string
		var e ProgressEntry
		if err := cardRows.Scan(&id, &e.Level, &e.NextReviewAt, &e.LastReviewAt); err != nil {
			return nil, err
		}
		data.Cards[id] = e
	}

	tqRows, err := r.pool.Query(ctx,
		`SELECT p.tq_id::text, p.level, p.next_review_at, p.last_review_at
		 FROM user_test_progress p
		 JOIN test_questions tq ON tq.id = p.tq_id
		 WHERE tq.collection_id = $1 AND p.user_id = $2`,
		collectionID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get tq progress: %w", err)
	}
	defer tqRows.Close()
	for tqRows.Next() {
		var id string
		var e ProgressEntry
		if err := tqRows.Scan(&id, &e.Level, &e.NextReviewAt, &e.LastReviewAt); err != nil {
			return nil, err
		}
		data.TQs[id] = e
	}

	return data, nil
}

func (r *ProgressRepository) GetCardProgress(ctx context.Context, userID, cardID string) (int, time.Time) {
	var level int
	var nextReview time.Time
	if err := r.pool.QueryRow(ctx,
		`SELECT level, next_review_at FROM user_card_progress WHERE user_id = $1 AND card_id = $2`,
		userID, cardID,
	).Scan(&level, &nextReview); err != nil {
		return 1, time.Time{}
	}
	return level, nextReview
}

func (r *ProgressRepository) GetTQProgress(ctx context.Context, userID, tqID string) (int, time.Time) {
	var level int
	var nextReview time.Time
	if err := r.pool.QueryRow(ctx,
		`SELECT level, next_review_at FROM user_test_progress WHERE user_id = $1 AND tq_id = $2`,
		userID, tqID,
	).Scan(&level, &nextReview); err != nil {
		return 1, time.Time{}
	}
	return level, nextReview
}

// ResetCollection deletes all of the user's progress for items in a collection.
func (r *ProgressRepository) ResetCollection(ctx context.Context, collectionID, userID string) error {
	if _, err := r.pool.Exec(ctx,
		`DELETE FROM user_card_progress
		 WHERE user_id = $2 AND card_id IN (SELECT id FROM cards WHERE collection_id = $1)`,
		collectionID, userID,
	); err != nil {
		return fmt.Errorf("reset card progress: %w", err)
	}
	if _, err := r.pool.Exec(ctx,
		`DELETE FROM user_test_progress
		 WHERE user_id = $2 AND tq_id IN (SELECT id FROM test_questions WHERE collection_id = $1)`,
		collectionID, userID,
	); err != nil {
		return fmt.Errorf("reset tq progress: %w", err)
	}
	if _, err := r.pool.Exec(ctx,
		`DELETE FROM user_sentence_progress
		 WHERE user_id = $2 AND sentence_id IN (
		     SELECT s.id FROM exercise_sentences s
		     JOIN exercises e ON e.id = s.exercise_id
		     WHERE e.collection_id = $1)`,
		collectionID, userID,
	); err != nil {
		return fmt.Errorf("reset sentence progress: %w", err)
	}
	return nil
}

// ResetExercise deletes the user's progress for all sentences of one exercise.
func (r *ProgressRepository) ResetExercise(ctx context.Context, userID, exerciseID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM user_sentence_progress
		 WHERE user_id = $1 AND sentence_id IN (SELECT id FROM exercise_sentences WHERE exercise_id = $2)`,
		userID, exerciseID,
	)
	return err
}

func (r *ProgressRepository) ResetCard(ctx context.Context, userID, cardID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_card_progress WHERE user_id = $1 AND card_id = $2`, userID, cardID)
	return err
}

func (r *ProgressRepository) ResetTQ(ctx context.Context, userID, tqID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM user_test_progress WHERE user_id = $1 AND tq_id = $2`, userID, tqID)
	return err
}

// SentenceResultEntry is the user's last answer to one sentence.
type SentenceResultEntry struct {
	Correct   bool     `json:"correct"`
	Submitted []string `json:"submitted"`
}

// RecordSentence stores the user's latest answer (the words placed, in order) and whether
// it was correct. No levels — exercises are one-off worksheets.
func (r *ProgressRepository) RecordSentence(ctx context.Context, userID, sentenceID string, correct bool, submitted []string) error {
	raw, err := json.Marshal(submitted)
	if err != nil {
		return fmt.Errorf("marshal submitted: %w", err)
	}
	if _, err = r.pool.Exec(ctx,
		`INSERT INTO user_sentence_progress (user_id, sentence_id, correct, submitted, answered_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id, sentence_id) DO UPDATE SET
		     correct     = EXCLUDED.correct,
		     submitted   = EXCLUDED.submitted,
		     answered_at = NOW()`,
		userID, sentenceID, correct, raw,
	); err != nil {
		return err
	}
	// Append to the permanent history log (kept across retakes).
	_, err = r.pool.Exec(ctx,
		`INSERT INTO user_sentence_answers (user_id, sentence_id, correct, submitted)
		 VALUES ($1, $2, $3, $4)`,
		userID, sentenceID, correct, raw,
	)
	return err
}

// GetResultsForCollection returns the user's saved answers for every answered sentence in
// a collection, keyed by sentence id.
func (r *ProgressRepository) GetResultsForCollection(ctx context.Context, collectionID, userID string) (map[string]SentenceResultEntry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT p.sentence_id::text, p.correct, p.submitted
		 FROM user_sentence_progress p
		 JOIN exercise_sentences s ON s.id = p.sentence_id
		 JOIN exercises e ON e.id = s.exercise_id
		 WHERE e.collection_id = $1 AND p.user_id = $2`,
		collectionID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get sentence results: %w", err)
	}
	defer rows.Close()
	out := make(map[string]SentenceResultEntry)
	for rows.Next() {
		var id string
		var e SentenceResultEntry
		var submitted []byte
		if err := rows.Scan(&id, &e.Correct, &submitted); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(submitted, &e.Submitted); err != nil {
			return nil, fmt.Errorf("unmarshal submitted: %w", err)
		}
		out[id] = e
	}
	return out, nil
}

func (r *ProgressRepository) UpsertCard(ctx context.Context, userID, cardID string, level int, nextReview time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_card_progress (user_id, card_id, level, next_review_at, last_review_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id, card_id) DO UPDATE SET
		     level          = EXCLUDED.level,
		     next_review_at = EXCLUDED.next_review_at,
		     last_review_at = NOW()`,
		userID, cardID, level, nextReview,
	)
	return err
}

func (r *ProgressRepository) UpsertTQ(ctx context.Context, userID, tqID string, level int, nextReview time.Time) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_test_progress (user_id, tq_id, level, next_review_at, last_review_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id, tq_id) DO UPDATE SET
		     level          = EXCLUDED.level,
		     next_review_at = EXCLUDED.next_review_at,
		     last_review_at = NOW()`,
		userID, tqID, level, nextReview,
	)
	return err
}
