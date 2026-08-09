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

// seedCard is one card of seed data. Hint is optional — deliberately absent from most
// cards, so a stand shows both shapes: with a hint to reveal and without.
type seedCard struct {
	Term       string
	Definition string
	Hint       string
}

// cardContent mirrors service.cardContent: the hint key exists only when there is a hint.
func cardContent(c seedCard) map[string]any {
	m := map[string]any{"term": c.Term, "definition": c.Definition}
	if c.Hint != "" {
		m["hint"] = c.Hint
	}
	return m
}

// insertCardItems adds card items to a collection, ranked in order.
func insertCardItems(ctx context.Context, pool *pgxpool.Pool, colID string, cards []seedCard) error {
	keys := rank.Sequence(len(cards))
	for i, c := range cards {
		if _, err := pool.Exec(ctx,
			`INSERT INTO items (type, collection_id, content, rank) VALUES ('card', $1, $2, $3)`,
			colID, mustJSON(cardContent(c)), keys[i],
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

	cards := []seedCard{
		{Term: "What is a goroutine?", Definition: "A lightweight thread managed by the Go runtime", Hint: "Starts with a couple of kilobytes of stack, so thousands of them are ordinary."},
		{Term: "What does defer do?", Definition: "Schedules a function to run when the surrounding function returns", Hint: "Think of closing a file on the line right after you opened it."},
		{Term: "What is a channel?", Definition: "A typed conduit for sending and receiving values between goroutines", Hint: "Its type can carry a direction: chan<- sends, <-chan receives."},
		{Term: "Zero value of a pointer?", Definition: "nil", Hint: "The same value an uninitialised map, slice or interface holds."},
		{Term: "Which keyword starts a goroutine?", Definition: "go"},
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
			colID, mustJSON(cardContent(c)), keys[i],
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
	if err = insertCardItems(ctx, pool, privateColID, []seedCard{
		{Term: "Secret key 1", Definition: "Secret answer 1"},
		{Term: "Secret key 2", Definition: "Secret answer 2"},
	}); err != nil {
		return "", err
	}

	if err = seedExtraUsers(ctx, pool, userID); err != nil {
		return "", err
	}

	log.Printf("seed: dev user %s, collection %s", userID, colID)
	return userID, nil
}

// seedExtraUsers fills the other accounts. devUserID is the dev user from Run: collections
// marked followed show up on their home page, which is the only way to see a followed
// collection without clicking through the public list first.

// englishVocabulary is a dictionary-shaped deck: a word and what it means, the way a learner
// would actually write one. Hints are on the words whose trap is spelling or a false friend,
// not on every card — a hint on all of them would say nothing.
var englishVocabulary = []seedCard{
	{Term: "abundant", Definition: "Present in great quantity; more than enough"},
	{Term: "adamant", Definition: "Refusing to be persuaded or to change one's mind", Hint: "Named after a legendary unbreakable stone."},
	{Term: "ambiguous", Definition: "Open to more than one interpretation"},
	{Term: "benevolent", Definition: "Well meaning and kindly", Hint: "Latin bene = well; the opposite starts with mal-."},
	{Term: "candid", Definition: "Truthful and straightforward, even when it is awkward"},
	{Term: "coherent", Definition: "Logical and consistent; holding together"},
	{Term: "concise", Definition: "Saying much in few words"},
	{Term: "diligent", Definition: "Showing steady, careful effort in what one does"},
	{Term: "eloquent", Definition: "Fluent and persuasive in speech or writing"},
	{Term: "eminent", Definition: "Famous and respected within a profession", Hint: "Not imminent, which is about time, not standing."},
	{Term: "feasible", Definition: "Possible to do easily or conveniently"},
	{Term: "frugal", Definition: "Sparing with money or food; not wasteful"},
	{Term: "gregarious", Definition: "Fond of company; sociable"},
	{Term: "hesitant", Definition: "Slow to act or speak through doubt"},
	{Term: "impartial", Definition: "Treating all sides equally; not favouring one"},
	{Term: "inevitable", Definition: "Certain to happen; unavoidable"},
	{Term: "intricate", Definition: "Very complicated or detailed"},
	{Term: "lucid", Definition: "Clear and easy to understand; thinking clearly"},
	{Term: "meticulous", Definition: "Showing great attention to detail; very careful"},
	{Term: "notorious", Definition: "Famous for something bad", Hint: "Fame with a bad smell — never a compliment."},
	{Term: "obsolete", Definition: "No longer produced or used; out of date"},
	{Term: "plausible", Definition: "Seeming reasonable or probable"},
	{Term: "prudent", Definition: "Acting with care and thought for the future"},
	{Term: "reluctant", Definition: "Unwilling and hesitant to do something"},
	{Term: "resilient", Definition: "Able to recover quickly from difficulty"},
	{Term: "scarce", Definition: "Insufficient for the demand; hard to find"},
	{Term: "tedious", Definition: "Too long, slow or dull; tiresome"},
	{Term: "tentative", Definition: "Not certain or fixed; provisional"},
	{Term: "versatile", Definition: "Able to adapt to many different functions"},
	{Term: "vivid", Definition: "Producing powerful, clear images in the mind"},
}

// writingSystems is the deck the typing mini-game is exercised against. Its terms are chosen
// for their *spelling*, not their meaning: each one is a case the on-screen keyboard has to
// get right — letters QWERTY has no key for, a word split by a space, an apostrophe or a
// hyphen the learner should not have to type, a card offering two accepted forms, and scripts
// that have to abandon the QWERTY shape altogether. Deliberately short, so a seven-card round
// covers most of it in one go.
var writingSystems = []seedCard{
	{Term: "der Löffel / Löffel", Definition: "The spoon — German, with the article", Hint: "Either form counts, and the blanks are drawn for the longer one."},
	{Term: "die Straße", Definition: "The street — German", Hint: "The ß is a letter of its own, not a double s."},
	{Term: "l'école", Definition: "The school — French", Hint: "The apostrophe is already written for you."},
	{Term: "après-midi", Definition: "Afternoon — French"},
	{Term: "el niño", Definition: "The boy — Spanish"},
	{Term: "dziękuję", Definition: "Thank you — Polish"},
	{Term: "ışık", Definition: "Light — Turkish", Hint: "Turkish keeps dotted and dotless i apart: ı is not i."},
	{Term: "kuća", Definition: "House — Serbian, latinica"},
	// Several cards per non-Latin script on purpose: the decoy keys are drawn from the other
	// letters the deck uses in that same script, so a lone Cyrillic card would be offered its
	// own letters and nothing else — the keyboard would spell the answer out.
	{Term: "љубав", Definition: "Love — Serbian, ћирилица", Hint: "Written in the other of Serbian's two alphabets."},
	{Term: "хвала", Definition: "Thank you — Serbian, ћирилица"},
	{Term: "добро јутро", Definition: "Good morning — Serbian, ћирилица"},
	{Term: "ευχαριστώ", Definition: "Thank you — Greek"},
	{Term: "καλημέρα", Definition: "Good morning — Greek"},
	{Term: "θάλασσα", Definition: "Sea — Greek"},
}

func seedExtraUsers(ctx context.Context, pool *pgxpool.Pool, devUserID string) error {
	type col struct {
		title    string
		desc     string
		isPublic bool
		followed bool // the dev user follows it
		cards    []seedCard
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
				{title: "French Vocabulary", desc: "Basic French words", isPublic: true, cards: []seedCard{
					{Term: "Bonjour", Definition: "Hello", Hint: "Only until evening — after dark it turns into bonsoir."},
					{Term: "Merci", Definition: "Thank you", Hint: "English borrowed it as 'mercy', from the same Latin root."},
					{Term: "Au revoir", Definition: "Goodbye"},
				}},
				// Followed by the dev user: it is a test fixture, so it should be one click from
				// the home page rather than hunted for in the public list.
				{title: "Writing Systems", desc: "Accents, umlauts, ćirilica and Greek — a workout for the typing keyboard", isPublic: true, followed: true, cards: writingSystems},
				{title: "Alice's Private Deck", desc: "Private study material", cards: []seedCard{
					{Term: "Private card", Definition: "Private answer"},
				}},
			},
		},
		{
			googleID: "seed_user_bob", email: "bob@example.com", name: "Bob Jones", role: "pro",
			cols: []col{
				{title: "Math Formulas", desc: "Essential math formulas", isPublic: true, cards: []seedCard{
					{Term: "Area of a circle", Definition: "π × r²", Hint: "Not the one with 2π — that measures the rim, not the inside."},
					{Term: "Pythagorean theorem", Definition: "a² + b² = c²"},
					{Term: "Quadratic formula", Definition: "(-b ± √(b²-4ac)) / 2a", Hint: "The classic mnemonic sings it to 'Pop Goes the Weasel'."},
				}},
			},
		},
		{
			googleID: "seed_user_carol", email: "carol@example.com", name: "Carol White", role: "user",
			cols: []col{
				// Long on purpose: the only seeded collection past one page, so paging, search
				// and the mini-games have something bigger than a demo deck to work on.
				{title: "English Vocabulary", desc: "A small dictionary: word and meaning", isPublic: true, followed: true, cards: englishVocabulary},
			},
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
			if c.followed {
				if _, err := pool.Exec(ctx,
					`INSERT INTO collection_follows (user_id, collection_id) VALUES ($1, $2)
					 ON CONFLICT DO NOTHING`, devUserID, colID,
				); err != nil {
					return fmt.Errorf("follow %s as dev user: %w", c.title, err)
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
