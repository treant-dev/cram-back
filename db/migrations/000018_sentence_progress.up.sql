-- Per-sentence spaced-repetition progress for exercises, mirroring user_card_progress /
-- user_test_progress. Keyed by the (real, stable) exercise_sentences row so progress
-- cascades away when a sentence is deleted (e.g. on re-import).
CREATE TABLE user_sentence_progress (
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sentence_id    UUID NOT NULL REFERENCES exercise_sentences(id) ON DELETE CASCADE,
    level          INT NOT NULL DEFAULT 1 CHECK (level BETWEEN 1 AND 7),
    next_review_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_review_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, sentence_id)
);
