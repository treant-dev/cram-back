package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

type ExerciseRepository struct {
	pool *pgxpool.Pool
}

func NewExerciseRepository(pool *pgxpool.Pool) *ExerciseRepository {
	return &ExerciseRepository{pool: pool}
}

// ListByCollection returns all exercises of a collection with their sentences,
// ordered by position. Mirrors TestQuestionRepository.ListByCollection.
func (r *ExerciseRepository) ListByCollection(ctx context.Context, collectionID string) ([]model.Exercise, error) {
	exRows, err := r.pool.Query(ctx,
		`SELECT id::text, collection_id::text, kind, title, distractors, position, created_at, updated_at
		 FROM exercises WHERE collection_id = $1
		 ORDER BY position ASC, created_at ASC`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list exercises: %w", err)
	}
	defer exRows.Close()

	var exercises []model.Exercise
	idxByID := map[string]int{}
	for exRows.Next() {
		var ex model.Exercise
		var distractors []byte
		if err := exRows.Scan(&ex.ID, &ex.CollectionID, &ex.Kind, &ex.Title, &distractors, &ex.Position, &ex.CreatedAt, &ex.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan exercise: %w", err)
		}
		if err := json.Unmarshal(distractors, &ex.Distractors); err != nil {
			return nil, fmt.Errorf("unmarshal exercise distractors: %w", err)
		}
		idxByID[ex.ID] = len(exercises)
		exercises = append(exercises, ex)
	}
	exRows.Close()

	if len(exercises) == 0 {
		return exercises, nil
	}

	sRows, err := r.pool.Query(ctx,
		`SELECT id::text, exercise_id::text, text, answer, distractors, position
		 FROM exercise_sentences
		 WHERE exercise_id IN (SELECT id FROM exercises WHERE collection_id = $1)
		 ORDER BY position ASC`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list exercise sentences: %w", err)
	}
	defer sRows.Close()
	for sRows.Next() {
		var s model.ExerciseSentence
		var exID string
		var answer, distractors []byte
		if err := sRows.Scan(&s.ID, &exID, &s.Text, &answer, &distractors, &s.Position); err != nil {
			return nil, fmt.Errorf("scan exercise sentence: %w", err)
		}
		if err := json.Unmarshal(answer, &s.Answer); err != nil {
			return nil, fmt.Errorf("unmarshal sentence answer: %w", err)
		}
		if err := json.Unmarshal(distractors, &s.Distractors); err != nil {
			return nil, fmt.Errorf("unmarshal sentence distractors: %w", err)
		}
		if idx, ok := idxByID[exID]; ok {
			exercises[idx].Sentences = append(exercises[idx].Sentences, s)
		}
	}

	return exercises, nil
}

// BulkCreate inserts exercises and their sentences in one transaction. Sentence and
// exercise IDs are assigned by the DB (gen_random_uuid). Position is taken from order.
func (r *ExerciseRepository) BulkCreate(ctx context.Context, collectionID string, exercises []model.Exercise) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for i, ex := range exercises {
		distractors, err := json.Marshal(ex.Distractors)
		if err != nil {
			return fmt.Errorf("marshal exercise distractors: %w", err)
		}
		var exID string
		if err = tx.QueryRow(ctx,
			`INSERT INTO exercises (collection_id, kind, title, distractors, position)
			 VALUES ($1, $2, $3, $4, $5) RETURNING id::text`,
			collectionID, ex.Kind, ex.Title, distractors, i,
		).Scan(&exID); err != nil {
			return fmt.Errorf("bulk insert exercise: %w", err)
		}
		for j, s := range ex.Sentences {
			answer, err := json.Marshal(s.Answer)
			if err != nil {
				return fmt.Errorf("marshal sentence answer: %w", err)
			}
			sDistractors, err := json.Marshal(s.Distractors)
			if err != nil {
				return fmt.Errorf("marshal sentence distractors: %w", err)
			}
			if _, err = tx.Exec(ctx,
				`INSERT INTO exercise_sentences (exercise_id, text, answer, distractors, position)
				 VALUES ($1, $2, $3, $4, $5)`,
				exID, s.Text, answer, sDistractors, j,
			); err != nil {
				return fmt.Errorf("bulk insert exercise sentence: %w", err)
			}
		}
	}

	return tx.Commit(ctx)
}

func (r *ExerciseRepository) Delete(ctx context.Context, id, collectionID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM exercises WHERE id=$1 AND collection_id=$2`,
		id, collectionID,
	)
	return err
}
