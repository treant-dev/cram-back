-- Fill-in-the-blank exercises. An exercise is a group of sentences of one kind
-- ('bank' = shared word pool / matching, 'choice' = options per blank). Sentences are
-- real rows (not JSONB) so they have stable IDs for per-sentence progress (FK cascade)
-- and fit the draft/publish diff machinery, mirroring test_questions + test_answers.
CREATE TABLE exercises (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    collection_id      UUID NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    kind               TEXT NOT NULL,                          -- 'bank' | 'choice'
    title              TEXT NOT NULL DEFAULT '',
    distractors        JSONB NOT NULL DEFAULT '[]'::jsonb,     -- bank only: extra words for the shared pool ([]string)
    position           INT NOT NULL DEFAULT 0,
    source_exercise_id UUID,                                   -- set on draft copies; links back to the published exercise
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_exercises_collection ON exercises(collection_id);

CREATE TABLE exercise_sentences (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    exercise_id UUID NOT NULL REFERENCES exercises(id) ON DELETE CASCADE,
    text        TEXT NOT NULL,                          -- contains one or more "___" blanks
    answer      JSONB NOT NULL DEFAULT '[]'::jsonb,     -- correct word per blank, in order ([]string)
    distractors JSONB NOT NULL DEFAULT '[]'::jsonb,     -- choice only: alternative whole combinations ([][]string)
    hint        TEXT NOT NULL DEFAULT '',
    position    INT NOT NULL DEFAULT 0
);
CREATE INDEX idx_exercise_sentences_exercise ON exercise_sentences(exercise_id);
