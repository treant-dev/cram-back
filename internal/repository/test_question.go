package repository

import (
	"context"
	"encoding/json"
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

func (r *TestQuestionRepository) Create(ctx context.Context, collectionID, question string, options []model.TestOption, position int) (*model.TestQuestion, error) {
	optJSON, err := json.Marshal(options)
	if err != nil {
		return nil, fmt.Errorf("marshal options: %w", err)
	}
	var tq model.TestQuestion
	var rawOptions []byte
	err = r.pool.QueryRow(ctx,
		`INSERT INTO test_questions (collection_id, question, options, position)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, collection_id, question, options, position, created_at, updated_at`,
		collectionID, question, optJSON, position,
	).Scan(&tq.ID, &tq.CollectionID, &tq.Question, &rawOptions, &tq.Position, &tq.CreatedAt, &tq.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create test question: %w", err)
	}
	if err := json.Unmarshal(rawOptions, &tq.Options); err != nil {
		return nil, fmt.Errorf("unmarshal options: %w", err)
	}
	return &tq, nil
}

func (r *TestQuestionRepository) ListByCollection(ctx context.Context, collectionID string) ([]model.TestQuestion, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, collection_id, question, options, position, created_at, updated_at
		 FROM test_questions WHERE collection_id = $1
		 ORDER BY position ASC, created_at ASC`,
		collectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list test questions: %w", err)
	}
	defer rows.Close()

	var questions []model.TestQuestion
	for rows.Next() {
		var tq model.TestQuestion
		var rawOptions []byte
		if err := rows.Scan(&tq.ID, &tq.CollectionID, &tq.Question, &rawOptions, &tq.Position, &tq.CreatedAt, &tq.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan test question: %w", err)
		}
		if err := json.Unmarshal(rawOptions, &tq.Options); err != nil {
			return nil, fmt.Errorf("unmarshal options: %w", err)
		}
		questions = append(questions, tq)
	}
	return questions, nil
}

func (r *TestQuestionRepository) Update(ctx context.Context, id, collectionID, question string, options []model.TestOption, position int) (*model.TestQuestion, error) {
	optJSON, err := json.Marshal(options)
	if err != nil {
		return nil, fmt.Errorf("marshal options: %w", err)
	}
	var tq model.TestQuestion
	var rawOptions []byte
	err = r.pool.QueryRow(ctx,
		`UPDATE test_questions SET question = $1, options = $2, position = $3, updated_at = NOW()
		 WHERE id = $4 AND collection_id = $5
		 RETURNING id, collection_id, question, options, position, created_at, updated_at`,
		question, optJSON, position, id, collectionID,
	).Scan(&tq.ID, &tq.CollectionID, &tq.Question, &rawOptions, &tq.Position, &tq.CreatedAt, &tq.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("update test question: %w", err)
	}
	if err := json.Unmarshal(rawOptions, &tq.Options); err != nil {
		return nil, fmt.Errorf("unmarshal options: %w", err)
	}
	return &tq, nil
}

func (r *TestQuestionRepository) Delete(ctx context.Context, id, collectionID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM test_questions WHERE id = $1 AND collection_id = $2`,
		id, collectionID,
	)
	return err
}
