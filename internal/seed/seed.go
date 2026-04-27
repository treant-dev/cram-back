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

	// Private collection for dev user
	var privateColID string
	if err = pool.QueryRow(ctx, `
		INSERT INTO collections (user_id, title, description, is_public)
		VALUES ($1, 'Private Notes', 'My private study notes', false)
		RETURNING id`, userID,
	).Scan(&privateColID); err != nil {
		return "", fmt.Errorf("create private collection: %w", err)
	}
	privateCards := [][2]string{
		{"Secret key 1", "Secret answer 1"},
		{"Secret key 2", "Secret answer 2"},
	}
	for i, c := range privateCards {
		if _, err = pool.Exec(ctx,
			`INSERT INTO cards (collection_id, question, answer, position) VALUES ($1,$2,$3,$4)`,
			privateColID, c[0], c[1], i,
		); err != nil {
			return "", fmt.Errorf("insert private card: %w", err)
		}
	}

	if err = seedExtraUsers(ctx, pool); err != nil {
		return "", err
	}

	log.Printf("seed: dev user %s, collection %s", userID, colID)
	return userID, nil
}

func seedExtraUsers(ctx context.Context, pool *pgxpool.Pool) error {
	extras := []struct {
		googleID string
		email    string
		name     string
		role     string
		cols     []struct {
			title    string
			desc     string
			isPublic bool
			cards    [][2]string
		}
	}{
		{
			googleID: "seed_user_alice",
			email:    "alice@example.com",
			name:     "Alice Smith",
			role:     "user",
			cols: []struct {
				title    string
				desc     string
				isPublic bool
				cards    [][2]string
			}{
				{"French Vocabulary", "Basic French words", true, [][2]string{
					{"Bonjour", "Hello"},
					{"Merci", "Thank you"},
					{"Au revoir", "Goodbye"},
				}},
				{"Alice's Private Deck", "Private study material", false, [][2]string{
					{"Private card", "Private answer"},
				}},
			},
		},
		{
			googleID: "seed_user_bob",
			email:    "bob@example.com",
			name:     "Bob Jones",
			role:     "premium",
			cols: []struct {
				title    string
				desc     string
				isPublic bool
				cards    [][2]string
			}{
				{"Math Formulas", "Essential math formulas", true, [][2]string{
					{"Area of a circle", "π × r²"},
					{"Pythagorean theorem", "a² + b² = c²"},
					{"Quadratic formula", "(-b ± √(b²-4ac)) / 2a"},
				}},
			},
		},
		{
			googleID: "seed_user_carol",
			email:    "carol@example.com",
			name:     "Carol White",
			role:     "user",
			cols:     nil,
		},
	}

	for _, u := range extras {
		var uid string
		if err := pool.QueryRow(ctx, `
			INSERT INTO users (google_id, email, name, picture, role)
			VALUES ($1, $2, $3, '', $4)
			ON CONFLICT (google_id) DO UPDATE SET name = EXCLUDED.name, role = EXCLUDED.role
			RETURNING id`,
			u.googleID, u.email, u.name, u.role,
		).Scan(&uid); err != nil {
			return fmt.Errorf("upsert extra user %s: %w", u.name, err)
		}

		if _, err := pool.Exec(ctx, `DELETE FROM collections WHERE user_id = $1`, uid); err != nil {
			return fmt.Errorf("clear extra user collections: %w", err)
		}

		for _, col := range u.cols {
			var colID string
			if err := pool.QueryRow(ctx, `
				INSERT INTO collections (user_id, title, description, is_public)
				VALUES ($1, $2, $3, $4) RETURNING id`,
				uid, col.title, col.desc, col.isPublic,
			).Scan(&colID); err != nil {
				return fmt.Errorf("create extra collection: %w", err)
			}
			for i, c := range col.cards {
				if _, err := pool.Exec(ctx,
					`INSERT INTO cards (collection_id, question, answer, position) VALUES ($1,$2,$3,$4)`,
					colID, c[0], c[1], i,
				); err != nil {
					return fmt.Errorf("insert extra card: %w", err)
				}
			}
		}
		log.Printf("seed: extra user %s (%s)", u.name, uid)
	}
	return nil
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
