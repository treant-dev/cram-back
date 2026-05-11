CREATE TABLE test_answers (
    id               UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    test_question_id UUID    NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
    text             TEXT    NOT NULL,
    is_correct       BOOLEAN NOT NULL DEFAULT false,
    explanation      TEXT    NOT NULL DEFAULT '',
    position         INT     NOT NULL DEFAULT 0
);

CREATE INDEX ON test_answers (test_question_id);

INSERT INTO test_answers (test_question_id, text, is_correct, explanation, position)
SELECT
    tq.id,
    elem.value->>'text',
    (elem.value->>'is_correct')::boolean,
    '',
    (elem.ordinality - 1)::int
FROM test_questions tq,
     jsonb_array_elements(tq.options) WITH ORDINALITY AS elem(value, ordinality);

ALTER TABLE test_questions DROP COLUMN options;
