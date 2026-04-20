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
	CardID  string
	TQID    string
	Correct bool
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

// SubmitSession inserts raw events and upserts aggregate stats in a single transaction.
// Only answers whose card_id/tq_id belong to collectionID are recorded.
// Answers are processed in order; the last answer per card/tq determines streak direction.
func (r *StudyRepository) SubmitSession(ctx context.Context, userID, sessionID, collectionID string, answers []StudyAnswer) error {
	if len(answers) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Fetch valid card/tq IDs for this collection to reject cross-collection submissions.
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

	validTQs := map[string]bool{}
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
		validTQs[id] = true
	}
	tqIDRows.Close()

	type aggEntry struct {
		correct     int
		incorrect   int
		lastCorrect bool
	}
	cardAgg := map[string]*aggEntry{}
	tqAgg := map[string]*aggEntry{}

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
			e := cardAgg[a.CardID]
			if e == nil {
				e = &aggEntry{}
				cardAgg[a.CardID] = e
			}
			if a.Correct {
				e.correct++
			} else {
				e.incorrect++
			}
			e.lastCorrect = a.Correct
		} else if a.TQID != "" {
			if !validTQs[a.TQID] {
				continue
			}
			if _, err = tx.Exec(ctx,
				`INSERT INTO user_tq_events (user_id, tq_id, session_id, correct)
				 VALUES ($1, $2, $3, $4)`,
				userID, a.TQID, sessionID, a.Correct,
			); err != nil {
				return fmt.Errorf("insert tq event: %w", err)
			}
			e := tqAgg[a.TQID]
			if e == nil {
				e = &aggEntry{}
				tqAgg[a.TQID] = e
			}
			if a.Correct {
				e.correct++
			} else {
				e.incorrect++
			}
			e.lastCorrect = a.Correct
		}
	}

	for cardID, agg := range cardAgg {
		if _, err = tx.Exec(ctx,
			`INSERT INTO user_card_stats (user_id, card_id, correct, incorrect, streak, last_seen)
			 VALUES ($1, $2, $3, $4, CASE WHEN $5 THEN 1 ELSE 0 END, NOW())
			 ON CONFLICT (user_id, card_id) DO UPDATE SET
			     correct   = user_card_stats.correct   + EXCLUDED.correct,
			     incorrect = user_card_stats.incorrect + EXCLUDED.incorrect,
			     streak    = CASE WHEN $5 THEN user_card_stats.streak + 1 ELSE 0 END,
			     last_seen = NOW()`,
			userID, cardID, agg.correct, agg.incorrect, agg.lastCorrect,
		); err != nil {
			return fmt.Errorf("upsert card stats: %w", err)
		}
	}

	for tqID, agg := range tqAgg {
		if _, err = tx.Exec(ctx,
			`INSERT INTO user_tq_stats (user_id, tq_id, correct, incorrect, streak, last_seen)
			 VALUES ($1, $2, $3, $4, CASE WHEN $5 THEN 1 ELSE 0 END, NOW())
			 ON CONFLICT (user_id, tq_id) DO UPDATE SET
			     correct   = user_tq_stats.correct   + EXCLUDED.correct,
			     incorrect = user_tq_stats.incorrect + EXCLUDED.incorrect,
			     streak    = CASE WHEN $5 THEN user_tq_stats.streak + 1 ELSE 0 END,
			     last_seen = NOW()`,
			userID, tqID, agg.correct, agg.incorrect, agg.lastCorrect,
		); err != nil {
			return fmt.Errorf("upsert tq stats: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (r *StudyRepository) ListCardStats(ctx context.Context, collectionID, userID string) (map[string]model.CardStats, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT s.card_id::text, s.correct, s.incorrect, s.streak, s.last_seen
		 FROM user_card_stats s
		 JOIN cards c ON c.id = s.card_id
		 WHERE c.collection_id = $1 AND s.user_id = $2`,
		collectionID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list card stats: %w", err)
	}
	defer rows.Close()
	result := make(map[string]model.CardStats)
	for rows.Next() {
		var id string
		var s model.CardStats
		if err := rows.Scan(&id, &s.Correct, &s.Incorrect, &s.Streak, &s.LastSeen); err != nil {
			return nil, fmt.Errorf("scan card stats: %w", err)
		}
		result[id] = s
	}
	return result, nil
}

func (r *StudyRepository) ListTQStats(ctx context.Context, collectionID, userID string) (map[string]model.TQStats, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT s.tq_id::text, s.correct, s.incorrect, s.streak, s.last_seen
		 FROM user_tq_stats s
		 JOIN test_questions tq ON tq.id = s.tq_id
		 WHERE tq.collection_id = $1 AND s.user_id = $2`,
		collectionID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tq stats: %w", err)
	}
	defer rows.Close()
	result := make(map[string]model.TQStats)
	for rows.Next() {
		var id string
		var s model.TQStats
		if err := rows.Scan(&id, &s.Correct, &s.Incorrect, &s.Streak, &s.LastSeen); err != nil {
			return nil, fmt.Errorf("scan tq stats: %w", err)
		}
		result[id] = s
	}
	return result, nil
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
		 FROM user_tq_events e
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
