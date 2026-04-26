CREATE TABLE user_card_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id     UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    session_id  UUID NOT NULL,
    correct     BOOLEAN NOT NULL,
    answered_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON user_card_events (user_id, card_id, answered_at DESC);
CREATE INDEX ON user_card_events (user_id, session_id);

CREATE TABLE user_tq_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tq_id       UUID NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
    session_id  UUID NOT NULL,
    correct     BOOLEAN NOT NULL,
    answered_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON user_tq_events (user_id, tq_id, answered_at DESC);
CREATE INDEX ON user_tq_events (user_id, session_id);

CREATE TABLE user_card_stats (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id   UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    correct   INT NOT NULL DEFAULT 0,
    incorrect INT NOT NULL DEFAULT 0,
    streak    INT NOT NULL DEFAULT 0,
    last_seen TIMESTAMPTZ,
    PRIMARY KEY (user_id, card_id)
);

CREATE TABLE user_tq_stats (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tq_id     UUID NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
    correct   INT NOT NULL DEFAULT 0,
    incorrect INT NOT NULL DEFAULT 0,
    streak    INT NOT NULL DEFAULT 0,
    last_seen TIMESTAMPTZ,
    PRIMARY KEY (user_id, tq_id)
);
