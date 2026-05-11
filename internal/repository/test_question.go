package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

type TestQuestionRepository struct {
	pool *pgxpool.Pool
}

func NewTestQuestionRepository(pool *pgxpool.Pool) *TestQuestionRepository {
	return &TestQuestionRepository{pool: pool}
}

func (r *TestQuestionRepository) Create(ctx context.Context, collectionID, question string, options []model.TestAnswer, image string, position int) (*model.TestQuestion, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var tq model.TestQuestion
	if err = tx.QueryRow(ctx,
		`INSERT INTO test_questions (collection_id, question, image, position)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id::text, collection_id::text, question, COALESCE(image, ''), position, created_at, updated_at`,
		collectionID, question, image, position,
	).Scan(&tq.ID, &tq.CollectionID, &tq.Question, &tq.Image, &tq.Position, &tq.CreatedAt, &tq.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create test question: %w", err)
	}

	tq.Options = make([]model.TestAnswer, len(options))
	for i, opt := range options {
		tq.Options[i] = opt
		tq.Options[i].Position = i
		if err = tx.QueryRow(ctx,
			`INSERT INTO test_answers (test_question_id, text, is_correct, explanation, position)
			 VALUES ($1, $2, $3, $4, $5)
			 RETURNING id::text`,
			tq.ID, opt.Text, opt.IsCorrect, opt.Explanation, i,
		).Scan(&tq.Options[i].ID); err != nil {
			return nil, fmt.Errorf("insert test answer: %w", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &tq, nil
}

func (r *TestQuestionRepository) ListByCollection(ctx context.Context, collectionID string) ([]model.TestQuestion, error) {
	qRows, err := r.pool.Query(ctx,
		`SELECT id::text, collection_id::text, question, COALESCE(image, ''), position, created_at, updated_at
		 FROM test_questions WHERE collection_id = $1
		 ORDER BY position ASC, created_at ASC`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list test questions: %w", err)
	}
	defer qRows.Close()

	var questions []model.TestQuestion
	idxByID := map[string]int{}
	for qRows.Next() {
		var tq model.TestQuestion
		if err := qRows.Scan(&tq.ID, &tq.CollectionID, &tq.Question, &tq.Image, &tq.Position, &tq.CreatedAt, &tq.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan test question: %w", err)
		}
		idxByID[tq.ID] = len(questions)
		questions = append(questions, tq)
	}
	qRows.Close()

	if len(questions) == 0 {
		return questions, nil
	}

	aRows, err := r.pool.Query(ctx,
		`SELECT id::text, test_question_id::text, text, is_correct, COALESCE(explanation, ''), position
		 FROM test_answers
		 WHERE test_question_id IN (SELECT id FROM test_questions WHERE collection_id = $1)
		 ORDER BY position ASC`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list test answers: %w", err)
	}
	defer aRows.Close()
	for aRows.Next() {
		var a model.TestAnswer
		var tqID string
		if err := aRows.Scan(&a.ID, &tqID, &a.Text, &a.IsCorrect, &a.Explanation, &a.Position); err != nil {
			return nil, fmt.Errorf("scan test answer: %w", err)
		}
		if idx, ok := idxByID[tqID]; ok {
			questions[idx].Options = append(questions[idx].Options, a)
		}
	}

	return questions, nil
}

func (r *TestQuestionRepository) Update(ctx context.Context, id, collectionID, question string, options []model.TestAnswer, image string, position int) (*model.TestQuestion, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var tq model.TestQuestion
	if err = tx.QueryRow(ctx,
		`UPDATE test_questions SET question=$1, image=$2, position=$3, updated_at=NOW()
		 WHERE id=$4 AND collection_id=$5
		 RETURNING id::text, collection_id::text, question, COALESCE(image, ''), position, created_at, updated_at`,
		question, image, position, id, collectionID,
	).Scan(&tq.ID, &tq.CollectionID, &tq.Question, &tq.Image, &tq.Position, &tq.CreatedAt, &tq.UpdatedAt); err != nil {
		return nil, fmt.Errorf("update test question: %w", err)
	}

	if _, err = tx.Exec(ctx, `DELETE FROM test_answers WHERE test_question_id=$1`, id); err != nil {
		return nil, fmt.Errorf("delete old answers: %w", err)
	}

	tq.Options = make([]model.TestAnswer, len(options))
	for i, opt := range options {
		tq.Options[i] = opt
		tq.Options[i].Position = i
		if err = tx.QueryRow(ctx,
			`INSERT INTO test_answers (test_question_id, text, is_correct, explanation, position)
			 VALUES ($1, $2, $3, $4, $5)
			 RETURNING id::text`,
			id, opt.Text, opt.IsCorrect, opt.Explanation, i,
		).Scan(&tq.Options[i].ID); err != nil {
			return nil, fmt.Errorf("insert test answer: %w", err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &tq, nil
}

func (r *TestQuestionRepository) BulkCreate(ctx context.Context, collectionID string, tqs []model.TestQuestion) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for i, tq := range tqs {
		var tqID string
		if err = tx.QueryRow(ctx,
			`INSERT INTO test_questions (collection_id, question, position) VALUES ($1, $2, $3) RETURNING id::text`,
			collectionID, tq.Question, i,
		).Scan(&tqID); err != nil {
			return fmt.Errorf("bulk insert test question: %w", err)
		}
		for j, opt := range tq.Options {
			if _, err = tx.Exec(ctx,
				`INSERT INTO test_answers (test_question_id, text, is_correct, explanation, position)
				 VALUES ($1, $2, $3, $4, $5)`,
				tqID, opt.Text, opt.IsCorrect, opt.Explanation, j,
			); err != nil {
				return fmt.Errorf("bulk insert test answer: %w", err)
			}
		}
	}

	return tx.Commit(ctx)
}

func (r *TestQuestionRepository) Delete(ctx context.Context, id, collectionID string) (string, error) {
	var image string
	err := r.pool.QueryRow(ctx,
		`DELETE FROM test_questions WHERE id=$1 AND collection_id=$2 RETURNING COALESCE(image, '')`,
		id, collectionID,
	).Scan(&image)
	if err != nil {
		return "", err
	}
	return image, nil
}
