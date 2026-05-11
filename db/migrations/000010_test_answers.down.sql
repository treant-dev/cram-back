ALTER TABLE test_questions ADD COLUMN options JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE test_questions tq
SET options = COALESCE(
    (SELECT jsonb_agg(jsonb_build_object('text', a.text, 'is_correct', a.is_correct) ORDER BY a.position)
     FROM test_answers a WHERE a.test_question_id = tq.id),
    '[]'::jsonb
);

DROP TABLE test_answers;
