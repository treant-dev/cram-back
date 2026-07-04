package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/rank"
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

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// insertCardItems adds card items to a collection, ranked in order.
func insertCardItems(ctx context.Context, pool *pgxpool.Pool, colID string, cards [][2]string) error {
	keys := rank.Sequence(len(cards))
	for i, c := range cards {
		if _, err := pool.Exec(ctx,
			`INSERT INTO items (type, collection_id, content, rank) VALUES ('card', $1, $2, $3)`,
			colID, mustJSON(map[string]any{"term": c[0], "definition": c[1]}), keys[i],
		); err != nil {
			return fmt.Errorf("insert card item: %w", err)
		}
	}
	return nil
}

// insertExercise adds an exercise item + its sentence children (bank/choice kinds).
func insertExercise(ctx context.Context, pool *pgxpool.Pool, colID, rankKey string, content map[string]any, sentences []map[string]any) error {
	var exID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO items (type, collection_id, content, rank) VALUES ('exercise', $1, $2, $3) RETURNING id`,
		colID, mustJSON(content), rankKey,
	).Scan(&exID); err != nil {
		return fmt.Errorf("insert exercise: %w", err)
	}
	skeys := rank.Sequence(len(sentences))
	for i, s := range sentences {
		if _, err := pool.Exec(ctx,
			`INSERT INTO items (type, collection_id, parent_id, content, rank) VALUES ('sentence', $1, $2, $3, $4)`,
			colID, exID, mustJSON(s), skeys[i],
		); err != nil {
			return fmt.Errorf("insert sentence: %w", err)
		}
	}
	return nil
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

	// Main collection is MIXED: cards + tests together (single-type is gone).
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
	questions := []struct {
		q    string
		opts []seedOpt
	}{
		{"Which of the following declares a variable in Go?", []seedOpt{{"var", true}, {":=", true}, {"let", false}, {"def", false}}},
		{"What is the default value of an int in Go?", []seedOpt{{"0", true}, {"nil", false}, {"undefined", false}, {"-1", false}}},
		{"Which keyword starts a goroutine?", []seedOpt{{"go", true}, {"async", false}, {"goroutine", false}, {"spawn", false}}},
	}

	keys := rank.Sequence(len(cards) + len(questions))
	for i, c := range cards {
		if _, err = pool.Exec(ctx,
			`INSERT INTO items (type, collection_id, content, rank) VALUES ('card', $1, $2, $3)`,
			colID, mustJSON(map[string]any{"term": c[0], "definition": c[1]}), keys[i],
		); err != nil {
			return "", fmt.Errorf("insert card item: %w", err)
		}
	}
	for i, tq := range questions {
		opts := make([]map[string]any, len(tq.opts))
		for j, o := range tq.opts {
			opts[j] = map[string]any{"text": o.Text, "is_correct": o.IsCorrect}
		}
		if _, err = pool.Exec(ctx,
			`INSERT INTO items (type, collection_id, content, rank) VALUES ('exercise', $1, $2, $3)`,
			colID, mustJSON(map[string]any{"kind": "quiz", "question": tq.q, "options": opts}), keys[len(cards)+i],
		); err != nil {
			return "", fmt.Errorf("insert quiz item: %w", err)
		}
	}

	// bank + choice exercises (Go-themed) so Go Basics has every exercise kind.
	exRank := rank.After(keys[len(keys)-1])
	if err = insertExercise(ctx, pool, colID, exRank,
		map[string]any{"kind": "bank", "title": "Go keywords", "distractors": []string{"func"}},
		[]map[string]any{
			{"text": "Start a goroutine with the ___ keyword.", "answer": []string{"go"}},
			{"text": "Schedule a cleanup call with ___.", "answer": []string{"defer"}},
		},
	); err != nil {
		return "", err
	}
	exRank = rank.After(exRank)
	if err = insertExercise(ctx, pool, colID, exRank,
		map[string]any{"kind": "choice", "title": "Go basics", "distractors": []string{}},
		[]map[string]any{
			{"text": "The zero value of a pointer is ___.", "answer": []string{"nil"}, "distractors": [][]string{{"0", "undefined"}}},
			{"text": "A slice's length is given by ___(s).", "answer": []string{"len"}, "distractors": [][]string{{"cap", "size"}}},
		},
	); err != nil {
		return "", err
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
	if err = insertCardItems(ctx, pool, privateColID, [][2]string{
		{"Secret key 1", "Secret answer 1"},
		{"Secret key 2", "Secret answer 2"},
	}); err != nil {
		return "", err
	}

	if err = seedExtraUsers(ctx, pool); err != nil {
		return "", err
	}

	log.Printf("seed: dev user %s, collection %s", userID, colID)
	return userID, nil
}

func seedExtraUsers(ctx context.Context, pool *pgxpool.Pool) error {
	type col struct {
		title    string
		desc     string
		isPublic bool
		cards    [][2]string
	}
	extras := []struct {
		googleID string
		email    string
		name     string
		role     string
		cols     []col
	}{
		{
			googleID: "seed_user_alice", email: "alice@example.com", name: "Alice Smith", role: "user",
			cols: []col{
				{"French Vocabulary", "Basic French words", true, [][2]string{
					{"Bonjour", "Hello"}, {"Merci", "Thank you"}, {"Au revoir", "Goodbye"},
				}},
				{"Alice's Private Deck", "Private study material", false, [][2]string{
					{"Private card", "Private answer"},
				}},
			},
		},
		{
			googleID: "seed_user_bob", email: "bob@example.com", name: "Bob Jones", role: "pro",
			cols: []col{
				{"Math Formulas", "Essential math formulas", true, [][2]string{
					{"Area of a circle", "π × r²"},
					{"Pythagorean theorem", "a² + b² = c²"},
					{"Quadratic formula", "(-b ± √(b²-4ac)) / 2a"},
				}},
			},
		},
		{
			googleID: "seed_user_carol", email: "carol@example.com", name: "Carol White", role: "user",
			cols: nil,
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

		for _, c := range u.cols {
			var colID string
			if err := pool.QueryRow(ctx, `
				INSERT INTO collections (user_id, title, description, is_public)
				VALUES ($1, $2, $3, $4) RETURNING id`,
				uid, c.title, c.desc, c.isPublic,
			).Scan(&colID); err != nil {
				return fmt.Errorf("create extra collection: %w", err)
			}
			if err := insertCardItems(ctx, pool, colID, c.cards); err != nil {
				return err
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
