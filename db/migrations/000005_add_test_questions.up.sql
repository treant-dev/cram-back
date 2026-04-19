-- Revert cards to single answer
ALTER TABLE cards ADD COLUMN answer TEXT NOT NULL DEFAULT '';
UPDATE cards SET answer = answers[1] WHERE array_length(answers, 1) > 0;
ALTER TABLE cards DROP COLUMN answers;

-- Test questions with options stored as JSONB
-- options format: [{"text": "...", "is_correct": true}, ...]
CREATE TABLE test_questions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    set_id     UUID NOT NULL REFERENCES study_sets(id) ON DELETE CASCADE,
    question   TEXT NOT NULL,
    options    JSONB NOT NULL DEFAULT '[]',
    position   INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_test_questions_set_id ON test_questions(set_id);
