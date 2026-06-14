-- Store what the user actually put in each blank (one word per blank, in order) so the
-- worksheet can restore the answered state on reload, not just whether it was correct.
ALTER TABLE user_sentence_progress
  ADD COLUMN submitted JSONB NOT NULL DEFAULT '[]'::jsonb;
