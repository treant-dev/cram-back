ALTER TABLE user_card_progress RENAME TO card_progress;
ALTER TABLE user_test_progress RENAME TO tq_progress;
ALTER TABLE user_test_events RENAME TO user_tq_events;

CREATE TABLE IF NOT EXISTS user_card_stats (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id   UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    correct   INT NOT NULL DEFAULT 0,
    incorrect INT NOT NULL DEFAULT 0,
    streak    INT NOT NULL DEFAULT 0,
    last_seen TIMESTAMPTZ,
    PRIMARY KEY (user_id, card_id)
);

CREATE TABLE IF NOT EXISTS user_tq_stats (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tq_id     UUID NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
    correct   INT NOT NULL DEFAULT 0,
    incorrect INT NOT NULL DEFAULT 0,
    streak    INT NOT NULL DEFAULT 0,
    last_seen TIMESTAMPTZ,
    PRIMARY KEY (user_id, tq_id)
);
