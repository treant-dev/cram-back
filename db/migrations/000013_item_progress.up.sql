CREATE TABLE card_progress (
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id        UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    level          INT NOT NULL DEFAULT 1 CHECK (level BETWEEN 1 AND 7),
    next_review_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_review_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, card_id)
);

CREATE TABLE tq_progress (
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tq_id          UUID NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
    level          INT NOT NULL DEFAULT 1 CHECK (level BETWEEN 1 AND 7),
    next_review_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_review_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, tq_id)
);
