package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/treant-dev/cram-go/internal/model"
)

type StudyRepository struct {
	pool *pgxpool.Pool
}

func NewStudyRepository(pool *pgxpool.Pool) *StudyRepository {
	return &StudyRepository{pool: pool}
}

type StudyAnswer struct {
	CardID              string
	TQID                string
	Correct             bool
	SelectedOptionTexts []string
}

func optionsCorrect(options []model.TestAnswer, selected []string) bool {
	sel := make(map[string]bool, len(selected))
	for _, s := range selected {
		sel[s] = true
	}
	for _, opt := range options {
		if opt.IsCorrect != sel[opt.Text] {
			return false
		}
	}
	return true
}

type DailyBucket struct {
	Date      string `json:"date"`
	Correct   int    `json:"correct"`
	Incorrect int    `json:"incorrect"`
}

type StudyHistoryData struct {
	Cards map[string][]DailyBucket `json:"cards"`
	TQs   map[string][]DailyBucket `json:"test_questions"`
}

// SubmitSession records per-item events for history tracking.
// Only answers whose card_id/tq_id belong to collectionID are recorded.
func (r *StudyRepository) SubmitSession(ctx context.Context, userID, sessionID, collectionID string, answers []StudyAnswer) error {
	if len(answers) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	validCards := map[string]bool{}
	cardIDRows, err := tx.Query(ctx, `SELECT id::text FROM cards WHERE collection_id = $1`, collectionID)
	if err != nil {
		return fmt.Errorf("fetch valid cards: %w", err)
	}
	for cardIDRows.Next() {
		var id string
		if err := cardIDRows.Scan(&id); err != nil {
			cardIDRows.Close()
			return fmt.Errorf("scan card id: %w", err)
		}
		validCards[id] = true
	}
	cardIDRows.Close()

	type tqData struct{ options []model.TestAnswer }
	validTQs := map[string]tqData{}
	tqIDRows, err := tx.Query(ctx, `SELECT id::text FROM test_questions WHERE collection_id = $1`, collectionID)
	if err != nil {
		return fmt.Errorf("fetch valid tqs: %w", err)
	}
	for tqIDRows.Next() {
		var id string
		if err := tqIDRows.Scan(&id); err != nil {
			tqIDRows.Close()
			return fmt.Errorf("scan tq id: %w", err)
		}
		validTQs[id] = tqData{}
	}
	tqIDRows.Close()

	if len(validTQs) > 0 {
		aRows, err := tx.Query(ctx,
			`SELECT ta.test_question_id::text, ta.text, ta.is_correct
			 FROM test_answers ta
			 JOIN test_questions tq ON tq.id = ta.test_question_id
			 WHERE tq.collection_id = $1`,
			collectionID,
		)
		if err != nil {
			return fmt.Errorf("fetch tq answers: %w", err)
		}
		for aRows.Next() {
			var tqID string
			var a model.TestAnswer
			if err := aRows.Scan(&tqID, &a.Text, &a.IsCorrect); err != nil {
				aRows.Close()
				return fmt.Errorf("scan tq answer: %w", err)
			}
			d := validTQs[tqID]
			d.options = append(d.options, a)
			validTQs[tqID] = d
		}
		aRows.Close()
	}

	for _, a := range answers {
		if a.CardID != "" {
			if !validCards[a.CardID] {
				continue
			}
			if _, err = tx.Exec(ctx,
				`INSERT INTO user_card_events (user_id, card_id, session_id, correct)
				 VALUES ($1, $2, $3, $4)`,
				userID, a.CardID, sessionID, a.Correct,
			); err != nil {
				return fmt.Errorf("insert card event: %w", err)
			}
		} else if a.TQID != "" {
			info, ok := validTQs[a.TQID]
			if !ok {
				continue
			}
			correct := optionsCorrect(info.options, a.SelectedOptionTexts)
			if _, err = tx.Exec(ctx,
				`INSERT INTO user_test_events (user_id, tq_id, session_id, correct)
				 VALUES ($1, $2, $3, $4)`,
				userID, a.TQID, sessionID, correct,
			); err != nil {
				return fmt.Errorf("insert tq event: %w", err)
			}
		}
	}

	return tx.Commit(ctx)
}

func (r *StudyRepository) GetHistory(ctx context.Context, collectionID, userID string, days int) (*StudyHistoryData, error) {
	data := &StudyHistoryData{
		Cards: make(map[string][]DailyBucket),
		TQs:   make(map[string][]DailyBucket),
	}

	cardRows, err := r.pool.Query(ctx,
		`SELECT e.card_id::text,
		        TO_CHAR(DATE_TRUNC('day', e.answered_at), 'YYYY-MM-DD'),
		        COUNT(*) FILTER (WHERE e.correct)::int,
		        COUNT(*) FILTER (WHERE NOT e.correct)::int
		 FROM user_card_events e
		 JOIN cards c ON c.id = e.card_id
		 WHERE e.user_id = $1 AND c.collection_id = $2
		   AND e.answered_at > NOW() - ($3 * INTERVAL '1 day')
		 GROUP BY e.card_id, DATE_TRUNC('day', e.answered_at)
		 ORDER BY e.card_id, DATE_TRUNC('day', e.answered_at)`,
		userID, collectionID, days,
	)
	if err != nil {
		return nil, fmt.Errorf("query card history: %w", err)
	}
	defer cardRows.Close()
	for cardRows.Next() {
		var cardID, day string
		var correct, incorrect int
		if err := cardRows.Scan(&cardID, &day, &correct, &incorrect); err != nil {
			return nil, fmt.Errorf("scan card history: %w", err)
		}
		data.Cards[cardID] = append(data.Cards[cardID], DailyBucket{Date: day, Correct: correct, Incorrect: incorrect})
	}

	tqRows, err := r.pool.Query(ctx,
		`SELECT e.tq_id::text,
		        TO_CHAR(DATE_TRUNC('day', e.answered_at), 'YYYY-MM-DD'),
		        COUNT(*) FILTER (WHERE e.correct)::int,
		        COUNT(*) FILTER (WHERE NOT e.correct)::int
		 FROM user_test_events e
		 JOIN test_questions tq ON tq.id = e.tq_id
		 WHERE e.user_id = $1 AND tq.collection_id = $2
		   AND e.answered_at > NOW() - ($3 * INTERVAL '1 day')
		 GROUP BY e.tq_id, DATE_TRUNC('day', e.answered_at)
		 ORDER BY e.tq_id, DATE_TRUNC('day', e.answered_at)`,
		userID, collectionID, days,
	)
	if err != nil {
		return nil, fmt.Errorf("query tq history: %w", err)
	}
	defer tqRows.Close()
	for tqRows.Next() {
		var tqID, day string
		var correct, incorrect int
		if err := tqRows.Scan(&tqID, &day, &correct, &incorrect); err != nil {
			return nil, fmt.Errorf("scan tq history: %w", err)
		}
		data.TQs[tqID] = append(data.TQs[tqID], DailyBucket{Date: day, Correct: correct, Incorrect: incorrect})
	}

	return data, nil
}
