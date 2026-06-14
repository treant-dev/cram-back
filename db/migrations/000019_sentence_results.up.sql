-- Exercises are one-off worksheets: we don't run spaced-repetition on them. We only
-- record whether each sentence was last answered correctly. Reshape the per-sentence
-- table from levels to a simple boolean result.
ALTER TABLE user_sentence_progress
  DROP COLUMN level,
  DROP COLUMN next_review_at,
  DROP COLUMN last_review_at,
  ADD COLUMN correct      BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN answered_at  TIMESTAMPTZ NOT NULL DEFAULT NOW();
