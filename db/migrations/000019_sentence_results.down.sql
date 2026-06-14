ALTER TABLE user_sentence_progress
  DROP COLUMN correct,
  DROP COLUMN answered_at,
  ADD COLUMN level          INT NOT NULL DEFAULT 1 CHECK (level BETWEEN 1 AND 7),
  ADD COLUMN next_review_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN last_review_at TIMESTAMPTZ;
