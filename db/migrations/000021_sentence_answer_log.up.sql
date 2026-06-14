-- Append-only history of every answer submitted to an exercise sentence. Survives
-- "retake" (which only clears the current answer in user_sentence_progress), so the full
-- attempt history is kept. Write-only for now; reading/displaying it is future work.
CREATE TABLE user_sentence_answers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sentence_id UUID NOT NULL REFERENCES exercise_sentences(id) ON DELETE CASCADE,
    correct     BOOLEAN NOT NULL,
    submitted   JSONB NOT NULL DEFAULT '[]'::jsonb,
    answered_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_sentence_answers_user_sentence ON user_sentence_answers(user_id, sentence_id, answered_at);
