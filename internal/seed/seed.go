package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DevGoogleID   = "dev_seed_user"
	DevEmail      = "dev@example.com"
	DevName       = "Dev User"
	AdminGoogleID = "dev_seed_admin"
	AdminEmail    = "admin@example.com"
	AdminName     = "Admin User"
)

type seedOpt struct {
	Text      string `json:"text"`
	IsCorrect bool   `json:"is_correct"`
}

func Run(ctx context.Context, pool *pgxpool.Pool) (userID string, err error) {
	if err = pool.QueryRow(ctx, `
		INSERT INTO users (google_id, email, name, picture)
		VALUES ($1, $2, $3, '')
		ON CONFLICT (google_id) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`,
		DevGoogleID, DevEmail, DevName,
	).Scan(&userID); err != nil {
		return "", fmt.Errorf("upsert seed user: %w", err)
	}

	if _, err = pool.Exec(ctx, `DELETE FROM collections WHERE user_id = $1`, userID); err != nil {
		return "", fmt.Errorf("clear seed data: %w", err)
	}

	var colID string
	if err = pool.QueryRow(ctx, `
		INSERT INTO collections (user_id, title, description, is_public)
		VALUES ($1, 'Go Basics', 'Core Go language concepts', true)
		RETURNING id`, userID,
	).Scan(&colID); err != nil {
		return "", fmt.Errorf("create collection: %w", err)
	}

	cards := [][2]string{
		{"What is a goroutine?", "A lightweight thread managed by the Go runtime"},
		{"What does defer do?", "Schedules a function to run when the surrounding function returns"},
		{"What is a channel?", "A typed conduit for sending and receiving values between goroutines"},
		{"Zero value of a pointer?", "nil"},
		{"Which keyword starts a goroutine?", "go"},
	}
	for i, c := range cards {
		if _, err = pool.Exec(ctx,
			`INSERT INTO cards (collection_id, question, answer, position) VALUES ($1,$2,$3,$4)`,
			colID, c[0], c[1], i,
		); err != nil {
			return "", fmt.Errorf("insert card: %w", err)
		}
	}

	questions := []struct {
		q    string
		opts []seedOpt
	}{
		{"Which of the following declares a variable in Go?", []seedOpt{{"var", true}, {":=", true}, {"let", false}, {"def", false}}},
		{"What is the default value of an int in Go?", []seedOpt{{"0", true}, {"nil", false}, {"undefined", false}, {"-1", false}}},
		{"Which keyword starts a goroutine?", []seedOpt{{"go", true}, {"async", false}, {"goroutine", false}, {"spawn", false}}},
	}
	for i, tq := range questions {
		opts, _ := json.Marshal(tq.opts)
		if _, err = pool.Exec(ctx,
			`INSERT INTO test_questions (collection_id, question, options, position) VALUES ($1,$2,$3,$4)`,
			colID, tq.q, opts, i,
		); err != nil {
			return "", fmt.Errorf("insert test question: %w", err)
		}
	}

	log.Printf("seed: dev user %s, collection %s", userID, colID)
	return userID, nil
}

func RunAdmin(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var adminID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (google_id, email, name, picture, role)
		VALUES ($1, $2, $3, '', 'admin')
		ON CONFLICT (google_id) DO UPDATE SET name = EXCLUDED.name, role = 'admin'
		RETURNING id`,
		AdminGoogleID, AdminEmail, AdminName,
	).Scan(&adminID); err != nil {
		return "", fmt.Errorf("upsert admin user: %w", err)
	}
	log.Printf("seed: admin user %s", adminID)
	return adminID, nil
}
